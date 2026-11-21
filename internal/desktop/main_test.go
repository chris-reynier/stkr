package desktop

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/server"
)

// hooks replaces the platform hooks for one test.
func hooks(t *testing.T, tray Tray, already bool, signalled *bool) {
	t.Helper()
	oldS, oldT, oldA, oldSig, oldO := supported, newTray, acquireInstance, signalInstance, openTarget
	t.Cleanup(func() {
		supported, newTray, acquireInstance, signalInstance, openTarget = oldS, oldT, oldA, oldSig, oldO
	})
	supported = true
	newTray = func(string) (Tray, error) {
		if tray == nil {
			return nil, errors.New("no tray here")
		}
		return tray, nil
	}
	acquireInstance = func(string) (func(), bool, error) { return func() {}, already, nil }
	signalInstance = func(string) error {
		if signalled != nil {
			*signalled = true
		}
		return nil
	}
	openTarget = func(string) error { return nil }
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
