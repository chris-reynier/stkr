package ai

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var fakebinPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ai-fakebin")
	if err != nil {
		panic(err)
	}
	name := "fakebin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fakebinPath = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", fakebinPath, "github.com/chris-reynier/stkr/internal/testutil/fakebin")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		panic(string(b))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func fakebin(testing.TB) string { return fakebinPath }

func newTestLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}
