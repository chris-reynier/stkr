package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTempHome(t *testing.T) {
	h := TempHome(t)
	if os.Getenv("STKR_HOME") != h {
		t.Fatal("STKR_HOME not set")
	}
	if _, err := os.Stat(filepath.Join(h, "logs")); err != nil {
		t.Fatal(err)
	}
}

func TestFakebin(t *testing.T) {
	bin := BuildFakebin(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	cmd := exec.Command(bin, "a", "b")
	cmd.Env = append(os.Environ(), "FAKEBIN_STDOUT=out", "FAKEBIN_EXIT=3", "FAKEBIN_ARGS_FILE="+argsFile, "FAKEBIN_ECHO_STDIN=1")
	cmd.Stdin = strings.NewReader("in-")
	out, err := cmd.Output()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 3 {
		t.Fatalf("err = %v", err)
	}
	if string(out) != "in-out" {
		t.Fatalf("stdout = %q", out)
	}
	if b, _ := os.ReadFile(argsFile); string(b) != "a\nb" {
		t.Fatalf("args = %q", b)
	}
}
