package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func exec(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	var so, se bytes.Buffer
	root, o := newRoot()
	code := run(root, o, append([]string{"--home", home}, args...), &so, &se)
	return so.String(), se.String(), code
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{errors.New("boom"), ExitError},
		{fmt.Errorf("x: %w", domain.ErrInvalid), ExitUsage},
		{fmt.Errorf("x: %w", domain.ErrUnavailable), ExitUsage},
		{fmt.Errorf("x: %w", domain.ErrNotFound), ExitError},
		{usageError{errors.New("flag")}, ExitUsage},
	}
	for _, tt := range tests {
		if got := exitCode(tt.err); got != tt.want {
			t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestConfigCommands(t *testing.T) {
	home := t.TempDir()

	out, _, code := exec(t, home, "config", "show", "--json")
	var m map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &m) != nil || m["port"] != float64(7878) {
		t.Fatalf("show --json: %d %q", code, out)
	}

	out, _, code = exec(t, home, "config", "set", "ai.provider", "ollama", "--json")
	if code != 0 || !strings.Contains(out, `"key":"ai.provider"`) {
		t.Fatalf("set: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "config", "show")
	if !strings.Contains(out, "provider: ollama") {
		t.Fatalf("show: %q", out)
	}

	for _, args := range [][]string{
		{"config", "set", "port", "0"},
		{"config", "set", "nope", "1"},
		{"config", "set", "port"},
	} {
		if _, _, code := exec(t, home, args...); code != ExitUsage {
			t.Errorf("%v: code = %d, want %d", args, code, ExitUsage)
		}
	}

	out, _, code = exec(t, home, "config", "path", "--json")
	if code != 0 || !strings.Contains(out, "config.yaml") {
		t.Fatalf("path: %d %q", code, out)
	}
}

func TestUnknownKeyWarning(t *testing.T) {
	home := t.TempDir()
	if _, _, code := exec(t, home, "config", "set", "port", "8000"); code != 0 {
		t.Fatal("set failed")
	}
	// Append an unknown key to the written file.
	if err := appendFile(home+"/config.yaml", "bogus: 1\n"); err != nil {
		t.Fatal(err)
	}
	_, errOut, code := exec(t, home, "config", "show")
	if code != 0 || !strings.Contains(errOut, "warning: unknown config key") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestVersion(t *testing.T) {
	out, _, code := exec(t, t.TempDir(), "version")
	if code != 0 || !strings.Contains(out, "stkr dev") {
		t.Fatalf("%d %q", code, out)
	}
}
