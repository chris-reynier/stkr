package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

func TestOpenMigratesAndReopens(t *testing.T) {
	ctx := context.Background()
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	svc, closeFn, err := Open(ctx, home, config.Default(), Options{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Switch(ctx, "other", domain.SourceCLI, ""); err != nil {
		t.Fatal(err)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, DBName)); err != nil {
		t.Fatal(err)
	}
	// Second open: nothing to migrate, data kept, no pre-migration backup.
	svc, closeFn, err = Open(ctx, home, config.Default(), Options{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeFn() }()
	if st, _ := svc.Now(ctx); !st.Tracking {
		t.Fatal("data lost on reopen")
	}
	if entries, _ := os.ReadDir(filepath.Join(home, "backups")); len(entries) != 0 {
		t.Fatalf("unexpected backups: %v", entries)
	}
}

func TestOpenErrors(t *testing.T) {
	ctx := context.Background()
	if _, _, err := Open(ctx, filepath.Join(t.TempDir(), "missing"), config.Default(), Options{}); err == nil {
		t.Fatal("want error for missing home")
	}
	home := t.TempDir()
	cfg := config.Default()
	cfg.Timezone = "Nowhere/Land"
	if _, _, err := Open(ctx, home, cfg, Options{}); err == nil {
		t.Fatal("want error for bad timezone")
	}
}
