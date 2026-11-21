// Package e2e drives the compiled stkr binary against a temporary data dir.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	binPath string
	// toolDir holds the binaries built for the run (removed at the end).
	toolDir string
)

func TestMain(m *testing.M) {
	os.Exit(realMain(m))
}

func realMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "stkr-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	toolDir = dir
	binPath = filepath.Join(dir, "stkr")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	build := exec.Command("go", "build", "-o", binPath, "github.com/chris-reynier/stkr/cmd/stkr")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// run executes the binary with an isolated home and returns stdout, stderr
// and the exit code.
func run(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "STKR_HOME="+home)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func TestSmokeP00(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	out, _, code := run(t, home, "version")
	if code != 0 || !strings.HasPrefix(out, "stkr dev") {
		t.Fatalf("version: code=%d out=%q", code, out)
	}

	out, _, code = run(t, home, "--json", "version")
	var v map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v["version"] != "dev" || v["os"] != runtime.GOOS {
		t.Fatalf("version --json: code=%d out=%q", code, out)
	}

	out, _, code = run(t, home, "config", "show")
	if code != 0 || !strings.Contains(out, "port: 7878") {
		t.Fatalf("config show: code=%d out=%q", code, out)
	}
	for _, d := range []string{"audio", "prompts", "backups", "logs"} {
		if _, err := os.Stat(filepath.Join(home, d)); err != nil {
			t.Errorf("data dir %s not created", d)
		}
	}

	out, _, code = run(t, home, "config", "path")
	if code != 0 || strings.TrimSpace(out) != filepath.Join(home, "config.yaml") {
		t.Fatalf("config path: code=%d out=%q", code, out)
	}

	if _, _, code = run(t, home, "config", "set", "port", "8123"); code != 0 {
		t.Fatalf("config set: code=%d", code)
	}
	out, _, _ = run(t, home, "config", "show")
	if !strings.Contains(out, "port: 8123") {
		t.Fatalf("set not persisted: %q", out)
	}

	_, errOut, code := run(t, home, "config", "set", "port", "99999")
	if code != 2 || !strings.Contains(errOut, "port") {
		t.Fatalf("invalid set: code=%d stderr=%q", code, errOut)
	}
	out, _, _ = run(t, home, "config", "show")
	if !strings.Contains(out, "port: 8123") {
		t.Fatalf("invalid set modified config: %q", out)
	}

	if _, _, code = run(t, home, "nonsense"); code != 2 {
		t.Fatalf("unknown command: code=%d", code)
	}
	if _, _, code = run(t, home, "--bogus"); code != 2 {
		t.Fatalf("unknown flag: code=%d", code)
	}
}

func TestSmokeTimeTracking(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	if out, _, code := run(t, home, "ctx", "add", "acme", "--label", "ACME"); code != 0 {
		t.Fatalf("ctx add: %d %q", code, out)
	}
	if _, _, code := run(t, home, "switch", "acme", "-n", "start"); code != 0 {
		t.Fatalf("switch acme: %d", code)
	}
	// A second switch to the same context within 2 s is a no-op.
	out, _, code := run(t, home, "--json", "switch", "acme")
	var sw struct {
		Created bool `json:"created"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &sw) != nil || sw.Created {
		t.Fatalf("double switch: %d %q", code, out)
	}
	if _, _, code := run(t, home, "switch", "meeting"); code != 0 {
		t.Fatalf("switch meeting: %d", code)
	}
	out, _, code = run(t, home, "--json", "now")
	var now struct {
		Tracking bool `json:"tracking"`
		Context  struct {
			ID string `json:"id"`
		} `json:"context"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &now) != nil || !now.Tracking || now.Context.ID != "meeting" {
		t.Fatalf("now: %d %q", code, out)
	}
	if _, _, code := run(t, home, "stop"); code != 0 {
		t.Fatalf("stop: %d", code)
	}

	out, _, code = run(t, home, "--json", "day")
	var day struct {
		Events []struct {
			ContextID string `json:"context_id"`
		} `json:"events"`
		Blocks []struct {
			ContextID string `json:"context_id"`
			Counted   bool   `json:"counted"`
		} `json:"blocks"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &day) != nil {
		t.Fatalf("day: %d %q", code, out)
	}
	var evs, blocks []string
	for _, e := range day.Events {
		evs = append(evs, e.ContextID)
	}
	for _, b := range day.Blocks {
		blocks = append(blocks, b.ContextID)
	}
	// Blocks shorter than a millisecond may be dropped; events are exact.
	if strings.Join(evs, ",") != "acme,meeting,off" {
		t.Fatalf("events: %v", evs)
	}
	for _, b := range blocks {
		if b != "acme" && b != "meeting" {
			t.Fatalf("unexpected block %q", b)
		}
	}

	out, _, code = run(t, home, "now")
	if code != 0 || !strings.HasPrefix(out, "not tracking") {
		t.Fatalf("now after stop: %d %q", code, out)
	}

	_, errOut, code := run(t, home, "switch", "nope")
	if code != 1 || !strings.Contains(errOut, "stkr ctx add nope") {
		t.Fatalf("unknown context: %d %q", code, errOut)
	}

	out, _, code = run(t, home, "backup")
	if code != 0 {
		t.Fatalf("backup: %d", code)
	}
	if _, err := os.Stat(strings.TrimSpace(out)); err != nil {
		t.Fatalf("backup file: %v", err)
	}
}
