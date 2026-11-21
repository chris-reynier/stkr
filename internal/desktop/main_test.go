package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/server"
)

// signalQuit records the quit argument of the last signalInstance call.
var signalQuit bool

// hooks replaces the platform hooks for one test.
func hooks(t *testing.T, tray Tray, already bool, signalled *bool) {
	t.Helper()
	oldS, oldT, oldA, oldSig, oldO, oldW := supported, newTray, acquireInstance, signalInstance, openTarget, newWindows
	oldC, oldE, oldR, oldU := confirmBox, executablePath, relaunchProgram, newUpdater
	t.Cleanup(func() {
		supported, newTray, acquireInstance, signalInstance, openTarget, newWindows = oldS, oldT, oldA, oldSig, oldO, oldW
		confirmBox, executablePath, relaunchProgram, newUpdater = oldC, oldE, oldR, oldU
	})
	// Tests never open a window.
	newWindows = func(string, bool, *slog.Logger) WindowsFactory { return nil }
	supported = true
	newTray = func(string) (Tray, error) {
		if tray == nil {
			return nil, errors.New("no tray here")
		}
		return tray, nil
	}
	acquireInstance = func(string) (func(), bool, error) { return func() {}, already, nil }
	signalInstance = func(_ string, quit bool) error {
		signalQuit = quit
		if signalled != nil {
			*signalled = true
		}
		return nil
	}
	openTarget = func(string) error { return nil }
	// Never look at (or clean up beside) the test binary, never start programs.
	exe := filepath.Join(t.TempDir(), "stkrw.exe")
	executablePath = func() (string, error) { return exe, nil }
	relaunchProgram = func(context.Context, string, []string) error { return nil }
	confirmBox = func(string, string) bool { return false }
}

func TestMainUnsupported(t *testing.T) {
	old := supported
	t.Cleanup(func() { supported = old })
	supported = false
	var stderr bytes.Buffer
	if code := Main(nil, &stderr, "0.2.0"); code != 2 || !strings.Contains(stderr.String(), "Windows only, use `stkr serve`") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestMainBadFlag(t *testing.T) {
	hooks(t, nil, false, nil)
	var stderr bytes.Buffer
	if code := Main([]string{"--nope"}, &stderr, "0.2.0"); code != 2 {
		t.Fatalf("code %d", code)
	}
}

func TestMainSecondInstanceSignals(t *testing.T) {
	var signalled bool
	hooks(t, nil, true, &signalled)
	home := filepath.Join(t.TempDir(), "home")
	if code := Main([]string{"-home", home}, &bytes.Buffer{}, "0.2.0"); code != 0 || !signalled {
		t.Fatalf("code %d signalled %v", code, signalled)
	}
}

func TestMainTrayError(t *testing.T) {
	hooks(t, nil, false, nil)
	home := filepath.Join(t.TempDir(), "home")
	var stderr bytes.Buffer
	if code := Main([]string{"-home", home}, &stderr, "0.2.0"); code != 1 || !strings.Contains(stderr.String(), "no tray here") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestMainBadConfig(t *testing.T) {
	hooks(t, newFakeTray(), false, nil)
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Path(home), []byte("desktop:\n  poll_seconds: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := Main([]string{"-home", home}, &stderr, "0.2.0"); code != 1 || !strings.Contains(stderr.String(), "poll_seconds") {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

// TestMainHostsAndQuits runs the whole program with a fake tray: it hosts
// the server on the configured port, answers /api/status as "desktop", and
// stops the server on Quit.
func TestMainHostsAndQuits(t *testing.T) {
	tray := newFakeTray()
	hooks(t, tray, false, nil)
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cfg := config.Default()
	cfg.Port = port
	cfg.Update.Check = false
	cfg.Desktop.PollSeconds = 2
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() { done <- Main([]string{"-home", home}, &bytes.Buffer{}, "0.2.0") }()
	<-tray.ready
	c := NewClient(port)
	var st Status
	waitFor(t, "hosted server", func() bool {
		var err error
		st, err = c.Status(context.Background())
		return err == nil
	})
	if st.HostedBy != server.HostedByDesktop || st.Version != "0.2.0" {
		t.Fatalf("status %+v", st)
	}
	tray.Quit()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(QuitTimeout + 5*time.Second):
		t.Fatal("Main did not return after Quit")
	}
	if _, err := http.Get(c.URL("/healthz")); err == nil { //nolint:noctx,bodyclose // expect failure
		t.Fatal("server still running after Quit")
	}
	for _, f := range []string{"desktop.log", "stkr.log"} {
		if _, err := os.Stat(filepath.Join(home, "logs", f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestSavePanelOnTopKeepsTheOtherSettings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Port = 7999
	cfg.Desktop.Hotkey = "Ctrl+Shift+K"
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true, false} {
		if err := savePanelOnTop(home, on); err != nil {
			t.Fatal(err)
		}
		got, _, err := config.Load(config.Path(home))
		if err != nil {
			t.Fatal(err)
		}
		if got.Desktop.PanelOnTop != on || got.Port != 7999 || got.Desktop.Hotkey != "Ctrl+Shift+K" {
			t.Fatalf("after %v: %+v port %d", on, got.Desktop, got.Port)
		}
	}
	// A damaged file is reported, not overwritten.
	if err := os.WriteFile(config.Path(home), []byte("port: [not a number"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := savePanelOnTop(home, true); err == nil {
		t.Fatal("a broken config file was overwritten")
	}
}

func TestMainPassesDesktopSettings(t *testing.T) {
	// The windows factory gets the saved pin state and the data folder.
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cfg := config.Default()
	cfg.Desktop.PanelOnTop = false
	cfg.Port = port
	cfg.Desktop.Hotkey = ""
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	tray := newFakeTray()
	hooks(t, tray, false, nil)
	var gotHome string
	var gotOnTop = true
	newWindows = func(h string, onTop bool, _ *slog.Logger) WindowsFactory {
		gotHome, gotOnTop = h, onTop
		return nil
	}
	done := make(chan int, 1)
	go func() { done <- Main([]string{"-home", home}, &bytes.Buffer{}, "0.2.0") }()
	<-tray.ready
	tray.Quit()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Main did not return")
	}
	if gotHome != home || gotOnTop {
		t.Errorf("factory got home %q onTop %v", gotHome, gotOnTop)
	}
	tray.mu.Lock()
	defer tray.mu.Unlock()
	if len(tray.hotkeys) != 0 {
		t.Errorf("hotkeys registered although it is empty: %v", tray.hotkeys)
	}
}

func TestMainQuitSignalsWithoutStarting(t *testing.T) {
	var signalled bool
	hooks(t, nil, false, &signalled)
	signalQuit = false
	home := filepath.Join(t.TempDir(), "home")
	if code := Main([]string{"-home", home, "--quit"}, &bytes.Buffer{}, "0.2.0"); code != 0 || !signalled || !signalQuit {
		t.Fatalf("code %d signalled %v quit %v", code, signalled, signalQuit)
	}
}

func TestMainAcceptsAutostartMarker(t *testing.T) {
	hooks(t, nil, true, nil)
	home := filepath.Join(t.TempDir(), "home")
	if code := Main([]string{"-home", home, "--autostart"}, &bytes.Buffer{}, "0.2.0"); code != 0 {
		t.Fatalf("code %d", code)
	}
}

// TestMainHeadlessMode: --headless-test swaps in the headless tray, builds no
// windows, never opens the browser or asks for a hotkey, and still hosts the
// server.
func TestMainHeadlessMode(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	cfg := config.Default()
	cfg.Port = port
	cfg.Update.Check = false
	cfg.Desktop.Hotkey = "Ctrl+Alt+K"
	if err := config.Save(config.Path(home), cfg); err != nil {
		t.Fatal(err)
	}
	normal := newFakeTray()
	headless := newFakeTray()
	hooks(t, normal, false, nil)
	var opened []string
	openTarget = func(s string) error { opened = append(opened, s); return nil }
	newHeadlessTray = func(string) (Tray, error) { return headless, nil }
	t.Cleanup(func() { newHeadlessTray = newPlatformHeadlessTray })
	factoryCalled := false
	newWindows = func(string, bool, *slog.Logger) WindowsFactory { factoryCalled = true; return nil }

	done := make(chan int, 1)
	go func() { done <- Main([]string{"-home", home, "--headless-test"}, &bytes.Buffer{}, "0.2.0") }()
	<-headless.ready
	c := NewClient(port)
	waitFor(t, "hosted server", func() bool { _, err := c.Status(context.Background()); return err == nil })
	headless.Quit()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(QuitTimeout + 5*time.Second):
		t.Fatal("Main did not return")
	}
	if factoryCalled || len(opened) != 0 {
		t.Errorf("windows built %v, opened %v", factoryCalled, opened)
	}
	headless.mu.Lock()
	defer headless.mu.Unlock()
	if len(headless.hotkeys) != 0 {
		t.Errorf("hotkey requested in headless mode: %v", headless.hotkeys)
	}
}

func TestHeadlessOpenLogsOnlyOwnPages(t *testing.T) {
	var buf bytes.Buffer
	open := headlessOpen(slog.New(slog.NewJSONHandler(&buf, nil)))
	_ = open("http://127.0.0.1:7878/ui/today")
	_ = open("https://example.com/secret-bookmark")
	_ = open(`C:\Users\someone\.stkr`)
	out := buf.String()
	if !strings.Contains(out, "http://127.0.0.1:7878/ui/today") || strings.Contains(out, "secret-bookmark") || strings.Contains(out, "someone") {
		t.Fatalf("log: %s", out)
	}
}

func TestWindowsForNil(t *testing.T) {
	if windowsFor(nil, "h", true, slog.Default()) != nil {
		t.Fatal("a nil factory function must give no windows")
	}
}
