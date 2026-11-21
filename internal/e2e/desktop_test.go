package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

// TestSmokeP11DesktopBinary builds stkrw. On Windows it is only built (as a
// GUI program); elsewhere it must print the hint and exit 2.
func TestSmokeP11DesktopBinary(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "stkrw")
	args := []string{"build", "-o", exe}
	if runtime.GOOS == "windows" {
		exe += ".exe"
		args = []string{"build", "-ldflags", "-H windowsgui", "-o", exe}
	}
	build := exec.Command("go", append(args, "github.com/chris-reynier/stkr/cmd/stkrw")...)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stkrw: %v\n%s", err, out)
	}
	if runtime.GOOS == "windows" {
		t.Skip("running the tray needs a desktop session (manual check M10)")
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
