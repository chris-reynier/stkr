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

var binPath string

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
