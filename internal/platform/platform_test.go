package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/testutil"
)

func TestLauncherArgv(t *testing.T) {
	bin := testutil.BuildFakebin(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"placeholder", []string{"--reuse-window", "{path}"}, "--reuse-window\n/w/my proj"},
		{"appended", nil, "/w/my proj"},
		{"embedded", []string{"--folder-uri={path}"}, "--folder-uri=/w/my proj"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "args")
			t.Setenv("FAKEBIN_ARGS_FILE", out)
			if err := NewLauncher(bin, c.args).OpenWorkspace(context.Background(), "/w/my proj"); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(out)
			if string(got) != c.want {
				t.Errorf("argv = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLauncherErrors(t *testing.T) {
	bin := testutil.BuildFakebin(t)
	ctx := context.Background()
	if err := NewLauncher("", nil).OpenWorkspace(ctx, "/w"); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("empty command: %v", err)
	}
	for _, p := range []string{"", "-evil", "a\x00b"} {
		if err := NewLauncher(bin, nil).OpenWorkspace(ctx, p); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("path %q: %v", p, err)
		}
	}
	t.Setenv("FAKEBIN_EXIT", "3")
	if err := NewLauncher(bin, nil).OpenWorkspace(ctx, "/w"); err == nil {
		t.Error("non-zero exit must fail")
	}
	if err := NewLauncher("stkr-no-such-binary", nil).OpenWorkspace(ctx, "/w"); err == nil {
		t.Error("missing binary must fail")
	}
}

func TestLauncherTimeout(t *testing.T) {
	bin := testutil.BuildFakebin(t)
	t.Setenv("FAKEBIN_SLEEP_MS", "3000")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := NewLauncher(bin, nil).OpenWorkspace(ctx, "/w")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("did not stop at the deadline")
	}
}
