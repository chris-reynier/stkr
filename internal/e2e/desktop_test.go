package e2e

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // test-only: integrity_check on the database file
)

// TestSmokeP11Status checks GET /api/status on `stkr serve` and its guard.
func TestSmokeP11Status(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	base, stop := startServe(t, home)
	defer func() { _ = stop() }()

	if code, _ := call(t, "POST", base+"/api/switch/meeting"); code != 200 {
		t.Fatalf("switch %d", code)
	}
	code, body := call(t, "GET", base+"/api/status")
	var st struct {
		Now *struct {
			ContextID string `json:"context_id"`
			Kind      string `json:"kind"`
			ElapsedS  *int64 `json:"elapsed_s"`
		} `json:"now"`
		Contexts []struct {
			ID string `json:"id"`
		} `json:"contexts"`
		CloseDue      *bool  `json:"close_due"`
		AgentsWaiting *int   `json:"agents_waiting"`
		IdeasNew      *int   `json:"ideas_new"`
		Version       string `json:"version"`
		HostedBy      string `json:"hosted_by"`
	}
	if code != 200 || json.Unmarshal(body, &st) != nil {
		t.Fatalf("status %d %s", code, body)
	}
	if st.Now == nil || st.Now.ContextID != "meeting" || st.Now.Kind != "mode" || st.Now.ElapsedS == nil {
		t.Fatalf("now = %s", body)
	}
	if len(st.Contexts) == 0 || st.Contexts[0].ID != "meeting" || st.CloseDue == nil || st.AgentsWaiting == nil || st.IdeasNew == nil {
		t.Fatalf("status = %s", body)
	}
	if st.HostedBy != "serve" || st.Version != "dev" {
		t.Fatalf("hosted_by %q version %q", st.HostedBy, st.Version)
	}
	if code, _ := call(t, "GET", base+"/api/status", "Host", "evil.example"); code != 421 {
		t.Fatalf("foreign host = %d", code)
	}
}

// ---- stkrw ----

var (
	stkrwOnce sync.Once
	stkrwPath string
	stkrwErr  error
)

// stkrwBinary builds stkrw once per test run (as a GUI program on Windows,
// like the release).
func stkrwBinary(t *testing.T) string {
	t.Helper()
	stkrwOnce.Do(func() {
		stkrwPath = filepath.Join(toolDir, "stkrw")
		args := []string{"build", "-o", stkrwPath}
		if runtime.GOOS == "windows" {
			stkrwPath += ".exe"
			args = []string{"build", "-ldflags", "-H windowsgui", "-o", stkrwPath}
		}
		build := exec.Command("go", append(args, "github.com/chris-reynier/stkr/cmd/stkrw")...)
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			stkrwErr = fmt.Errorf("build stkrw: %w\n%s", err, out)
		}
	})
	if stkrwErr != nil {
		t.Fatal(stkrwErr)
	}
	return stkrwPath
}

// TestSmokeP11DesktopBinary builds stkrw. On Windows it is only built (as a
// GUI program; TestDesktopHeadless* run it); elsewhere it must print the hint
// and exit 2.
func TestSmokeP11DesktopBinary(t *testing.T) {
	exe := stkrwBinary(t)
	if runtime.GOOS == "windows" {
		t.Skip("running the tray needs a desktop session (manual check M10); see TestDesktopHeadless*")
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "STKR_HOME="+filepath.Join(t.TempDir(), "home"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 2 || !strings.Contains(stderr.String(), "Windows only, use `stkr serve`") {
		t.Fatalf("err %v stderr %q", err, stderr.String())
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// desktopProc is a running `stkrw --headless-test`.
type desktopProc struct {
	cmd  *exec.Cmd
	done chan error
	home string
	port int
}

// startDesktop starts stkrw in test mode (no icon, no window, no hotkey,
// nothing opened) on the data folder and, with wantServer, waits until the
// server answers. Windows only: the single-instance handling and the message
// window are Win32.
func startDesktop(t *testing.T, home string, port int, wantServer bool) *desktopProc {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("stkrw runs on Windows only")
	}
	exe := stkrwBinary(t)
	cmd := exec.Command(exe, "--headless-test", "-home", home)
	cmd.Env = append(os.Environ(), "STKR_HOME="+home)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &desktopProc{cmd: cmd, done: make(chan error, 1), home: home, port: port}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	if wantServer {
		p.waitHealthy(t)
	}
	return p
}

func (p *desktopProc) base() string { return "http://127.0.0.1:" + strconv.Itoa(p.port) }

func (p *desktopProc) waitHealthy(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-p.done:
			t.Fatalf("stkrw ended early: %v\nlog:\n%s", err, desktopLog(p.home))
		default:
		}
		if resp, err := http.Get(p.base() + "/healthz"); err == nil { //nolint:noctx // short local probe
			_ = resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stkrw did not answer on %s\nlog:\n%s", p.base(), desktopLog(p.home))
}

// waitExit waits for the process to end and returns its exit code.
func (p *desktopProc) waitExit(t *testing.T, within time.Duration) int {
	t.Helper()
	select {
	case err := <-p.done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		return 0
	case <-time.After(within):
		_ = p.cmd.Process.Kill()
		t.Fatalf("stkrw did not exit within %v\nlog:\n%s", within, desktopLog(p.home))
		return -1
	}
}

func desktopLog(home string) string {
	b, _ := os.ReadFile(filepath.Join(home, "logs", "desktop.log"))
	return string(b)
}

// waitLog waits until desktop.log contains every fragment.
func waitLog(t *testing.T, home string, want ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		log := desktopLog(home)
		missing := ""
		for _, w := range want {
			if !strings.Contains(log, w) {
				missing = w
				break
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("desktop.log has no %q:\n%s", missing, log)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// desktopHome makes a data folder whose configuration uses a free port, no
// update check (so the test makes no outbound request) and no hotkey.
func desktopHome(t *testing.T) (string, int) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	port := freeTCPPort(t)
	for _, kv := range [][2]string{{"port", strconv.Itoa(port)}, {"update.check", "false"}, {"desktop.hotkey", ""}} {
		if _, errOut, code := run(t, home, "config", "set", kv[0], kv[1]); code != 0 {
			t.Fatalf("config set %s: %d %s", kv[0], code, errOut)
		}
	}
	return home, port
}

func integrityCheck(t *testing.T, home string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, "stkr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var res string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&res); err != nil || res != "ok" {
		t.Fatalf("integrity_check: %q %v", res, err)
	}
}

type statusJSON struct {
	HostedBy string `json:"hosted_by"`
	Now      *struct {
		ContextID string `json:"context_id"`
	} `json:"now"`
	IdeasNew int `json:"ideas_new"`
}

func getStatus(t *testing.T, base string) statusJSON {
	t.Helper()
	code, body := call(t, "GET", base+"/api/status")
	var st statusJSON
	if code != 200 || json.Unmarshal(body, &st) != nil {
		t.Fatalf("status %d %s", code, body)
	}
	return st
}

// quitDesktop runs `stkrw --quit` for the data folder.
func quitDesktop(t *testing.T, home string) {
	t.Helper()
	quit := exec.Command(stkrwBinary(t), "--quit", "-home", home)
	quit.Env = append(os.Environ(), "STKR_HOME="+home)
	if out, err := quit.CombinedOutput(); err != nil {
		t.Fatalf("stkrw --quit: %v %s", err, out)
	}
}

// TestDesktopHeadlessLifecycle runs the real stkrw.exe in test mode: it hosts
// the server, a CLI switch shows in /api/status, the panel round trip works, a
// second launch signals the first and exits 0, and --quit stops the first
// gracefully with the database intact.
func TestDesktopHeadlessLifecycle(t *testing.T) {
	home, port := desktopHome(t)
	if _, _, code := run(t, home, "ctx", "add", "acme", "--label", "ACME portal", "--kind", "project"); code != 0 {
		t.Fatal("ctx add failed")
	}
	first := startDesktop(t, home, port, true)
	base := first.base()
	waitLog(t, home, "hosting server")

	st := getStatus(t, base)
	if st.HostedBy != "desktop" || st.Now != nil {
		t.Fatalf("status = %+v", st)
	}

	// The CLI works on the same database while the desktop app hosts the server.
	if _, _, code := run(t, home, "switch", "acme"); code != 0 {
		t.Fatal("switch failed")
	}
	if st = getStatus(t, base); st.Now == nil || st.Now.ContextID != "acme" {
		t.Fatalf("status after a CLI switch = %+v", st)
	}

	// Panel round trip: switch, idea, stop.
	if resp := postForm(t, base, "/ui/switch", url.Values{"next": {"/ui/panel"}, "ctx": {"meeting"}}); resp.StatusCode != 303 {
		t.Fatalf("panel switch: %d", resp.StatusCode)
	}
	if st = getStatus(t, base); st.Now == nil || st.Now.ContextID != "meeting" {
		t.Fatalf("status after the panel switch = %+v", st)
	}
	if _, b := call(t, "GET", base+"/ui/panel"); !strings.Contains(string(b), "Stop</button>") {
		t.Errorf("panel does not offer Stop while tracking")
	}
	if resp := postForm(t, base, "/ui/ideas/add", url.Values{"next": {"/ui/panel"}, "text": {"idea from the desktop test"}}); resp.StatusCode != 303 {
		t.Fatalf("panel idea: %d", resp.StatusCode)
	}
	if st = getStatus(t, base); st.IdeasNew != 1 {
		t.Errorf("ideas_new = %d", st.IdeasNew)
	}
	if resp := postForm(t, base, "/ui/stop", url.Values{"next": {"/ui/panel"}}); resp.StatusCode != 303 {
		t.Fatalf("panel stop: %d", resp.StatusCode)
	}
	if st = getStatus(t, base); st.Now != nil {
		t.Fatalf("still tracking after stop: %+v", st)
	}

	// A second launch signals the first and exits 0 at once.
	second := startDesktop(t, home, port, false)
	if code := second.waitExit(t, 15*time.Second); code != 0 {
		t.Fatalf("second instance exited %d\n%s", code, desktopLog(home))
	}
	waitLog(t, home, "already running, asked it to show", `"action":"show-panel"`, "headless: open")
	if _, _, code := run(t, home, "now"); code != 0 {
		t.Fatal("cli now failed after the second launch")
	}
	getStatus(t, base) // the first instance is still serving

	// Desktop-only actions exist here (and only here): an update cannot be
	// installed without a pending one, and the data folder "opens" (headless:
	// logged, nothing is launched).
	if f := postFlash(t, base, "/ui/update/apply"); !strings.HasPrefix(f, "e") || !strings.Contains(f, "No update is waiting") {
		t.Errorf("update apply under the desktop host: %q", f)
	}
	if f := postFlash(t, base, "/ui/settings/open-data"); strings.HasPrefix(f, "e") || !strings.Contains(f, "Opened the data folder") {
		t.Errorf("open data folder: %q", f)
	}

	// --quit: a graceful stop with exit 0, the server is gone, the data is intact.
	quitDesktop(t, home)
	if code := first.waitExit(t, 15*time.Second); code != 0 {
		t.Fatalf("first instance exited %d\n%s", code, desktopLog(home))
	}
	waitLog(t, home, "quit requested", "desktop stopped")
	if resp, err := http.Get(base + "/healthz"); err == nil { //nolint:noctx // expect failure
		_ = resp.Body.Close()
		t.Fatal("server still answers after --quit")
	}
	integrityCheck(t, home)
	if out, _, code := run(t, home, "ideas"); code != 0 || !strings.Contains(out, "idea from the desktop test") {
		t.Errorf("idea lost: %d %q", code, out)
	}
	if out, _, code := run(t, home, "day"); code != 0 || !strings.Contains(out, "acme") {
		t.Errorf("day lost the CLI switch: %d %q", code, out)
	}
	// Nothing that is user content reaches the desktop log.
	if log := desktopLog(home); strings.Contains(log, "idea from the desktop test") || strings.Contains(log, "ACME portal") {
		t.Errorf("desktop.log has user content:\n%s", log)
	}
}

// TestDesktopHeadlessAttach starts `stkr serve` first: stkrw attaches to it
// (does not host), and quitting stkrw leaves the server running.
func TestDesktopHeadlessAttach(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("stkrw runs on Windows only")
	}
	home, port := desktopHome(t)
	base, stopServe := startServe(t, home, "--port", strconv.Itoa(port))
	if !strings.HasSuffix(base, ":"+strconv.Itoa(port)) {
		t.Fatalf("serve is on %s, want port %d", base, port)
	}
	d := startDesktop(t, home, port, false)
	waitLog(t, home, "attached to running server")
	if st := getStatus(t, base); st.HostedBy != "serve" {
		t.Fatalf("hosted_by = %q", st.HostedBy)
	}
	if strings.Contains(desktopLog(home), "hosting server") {
		t.Error("stkrw hosted a second server")
	}
	if code, _ := call(t, "POST", base+"/api/switch/meeting"); code != 200 {
		t.Fatal("switch failed")
	}
	quitDesktop(t, home)
	if code := d.waitExit(t, 15*time.Second); code != 0 {
		t.Fatalf("stkrw exited %d\n%s", code, desktopLog(home))
	}
	// The server of `stkr serve` is not touched by the quit.
	if st := getStatus(t, base); st.Now == nil || st.Now.ContextID != "meeting" {
		t.Fatalf("status after quitting stkrw: %+v", st)
	}
	_ = stopServe()
	integrityCheck(t, home)
}

// postFlash posts a UI form and returns the flash message ("o..." for ok,
// "e..." for an error) the server sets in its cookie.
func postFlash(t *testing.T, base, path string) string {
	t.Helper()
	resp := postForm(t, base, path, url.Values{"next": {"/ui/settings"}})
	if resp.StatusCode != 303 {
		t.Fatalf("%s: %d", path, resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "stkr_flash" {
			v, _ := url.QueryUnescape(c.Value)
			return v
		}
	}
	return ""
}
