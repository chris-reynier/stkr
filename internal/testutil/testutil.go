// Package testutil holds helpers shared by tests.
package testutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
)

// TempHome returns a fresh STKR data directory (already initialised) and sets
// STKR_HOME to it for the duration of the test. It never touches ~/.stkr.
func TempHome(t testing.TB) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "stkr-home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STKR_HOME", home)
	return home
}

// BuildFakebin compiles internal/testutil/fakebin into a temp dir and returns
// the executable path. See the fakebin package for the env vars it reads.
func BuildFakebin(t testing.TB) string {
	t.Helper()
	name := "fakebin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "github.com/chris-reynier/stkr/internal/testutil/fakebin")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakebin: %v\n%s", err, b)
	}
	return out
}
