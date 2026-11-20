package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

func TestUpdateConfig(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// Without a config path saving is unavailable but reading works.
	if err := f.svc.UpdateConfig(ctx, map[string]string{"day_end": "18:00"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if c, err := f.svc.ConfigFile(); err != nil || c.DayEnd != "17:00" {
		t.Fatalf("%v %+v", err, c)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	f.svc.configPath = path
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatal(err)
	}
	// Cross-field change valid only as a pair.
	err := f.svc.UpdateConfig(ctx, map[string]string{"day_start": "07:00", "day_end": "08:00", "ollama.model": "m1"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.svc.ConfigFile()
	if err != nil || c.DayStart != "07:00" || c.DayEnd != "08:00" || c.Ollama.Model != "m1" {
		t.Fatalf("%v %+v", err, c)
	}
	if f.svc.Config().DayEnd != "17:00" {
		t.Error("running config must not change")
	}
	// Invalid input leaves the file untouched.
	if err := f.svc.UpdateConfig(ctx, map[string]string{"day_end": "06:00"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if err := f.svc.UpdateConfig(ctx, map[string]string{"nope": "x"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if c, _ := f.svc.ConfigFile(); c.DayEnd != "08:00" {
		t.Errorf("file changed: %+v", c)
	}
}
