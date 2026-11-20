package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// The update scenario needs no network: a development build refuses to
// install, and `version` reads only the cache.
func TestSmokeUpdate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	if _, se, code := run(t, home, "update", "--yes"); code != 2 || !strings.Contains(se, "development build") {
		t.Fatalf("update on dev build: code=%d stderr=%q", code, se)
	}
	if _, _, code := run(t, home, "config", "set", "update.check", "false"); code != 0 {
		t.Fatalf("config set: %d", code)
	}
	out, _, code := run(t, home, "version")
	if code != 0 || strings.Contains(out, "update available") {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	if out, _, _ := run(t, home, "update", "--help"); !strings.Contains(out, "--check") || !strings.Contains(out, "--yes") {
		t.Fatalf("help: %q", out)
	}
}
