package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestStatusUpdateField(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		check   bool
		version string
		want    *StatusUpdate
	}{
		{"update known", true, "v0.1.0", &StatusUpdate{Available: true, Version: "v0.2.0"}},
		{"up to date", true, "v0.2.0", &StatusUpdate{Available: false, Version: "v0.2.0"}},
		{"check off", false, "v0.1.0", nil},
		{"dev build", true, "dev", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := updateFixture(t, true, tt.version, releases("v0.2.0", "v0.1.0"))
			if _, err := svc.UpdateCheck(ctx); err != nil && tt.version != "dev" {
				t.Fatal(err)
			}
			svc.cfg.Update.Check = tt.check
			st, err := svc.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(st.Update) != fmt.Sprint(tt.want) || st.Version != tt.version {
				t.Fatalf("update = %+v version %q, want %+v", st.Update, st.Version, tt.want)
			}
		})
	}
}

func TestStatusContextsRecentFirstAndLimited(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		f.addCtx(t, fmt.Sprintf("p%02d", i), domain.KindProject)
	}
	if err := f.svc.ParkContext(ctx, "p00"); err != nil {
		t.Fatal(err)
	}
	f.switchAt(t, "2026-11-16 09:00", "p19")
	f.switchAt(t, "2026-11-16 09:30", "p18")
	f.switchAt(t, "2026-11-16 10:00", "off")
	st, err := f.svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Now != nil {
		t.Fatalf("now after off = %+v", st.Now)
	}
	if len(st.Contexts) != StatusContextLimit {
		t.Fatalf("len = %d", len(st.Contexts))
	}
	if st.Contexts[0].ID != "p18" || st.Contexts[1].ID != "p19" {
		t.Fatalf("order = %v %v", st.Contexts[0], st.Contexts[1])
	}
	for _, c := range st.Contexts {
		if c.ID == "off" || c.ID == "p00" {
			t.Fatalf("unexpected context %q", c.ID)
		}
	}
}
