package service

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

var update = flag.Bool("update", false, "update golden files")

func (f *fixture) switchNote(t *testing.T, ts, ctxID, note string) {
	t.Helper()
	f.clk.Set(at(ts))
	if _, err := f.svc.Switch(context.Background(), ctxID, domain.SourceDeck, note); err != nil {
		t.Fatal(err)
	}
}

func tempoFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	for _, c := range []domain.Context{
		{ID: "acme", Label: "ACME", Kind: domain.KindProject, TempoTicket: "ACME-1"},
		{ID: "beta", Label: "Beta", Kind: domain.KindProject, TempoTicket: "ACME-1"},
		{ID: "gamma", Label: "Gamma", Kind: domain.KindProject},
	} {
		if _, err := f.svc.AddContext(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	f.switchNote(t, "2026-11-16 09:00", "acme", `fix "auth", retry`)
	f.switchNote(t, "2026-11-16 09:20", "beta", "")
	f.switchNote(t, "2026-11-16 09:40", "break", "")
	f.switchNote(t, "2026-11-16 10:00", "gamma", "")
	f.switchNote(t, "2026-11-16 10:30", "planning", "")
	f.switchNote(t, "2026-11-16 11:00", "off", "")
	f.switchNote(t, "2026-11-17 09:00", "acme", "")
	f.switchNote(t, "2026-11-17 09:07", "off", "")
	f.switchNote(t, "2026-11-18 09:00", "acme", "")
	f.switchNote(t, "2026-11-18 10:30", "off", "")
	f.clk.Set(at("2026-11-22 18:00"))
	return f
}

func TestTempoGolden(t *testing.T) {
	f := tempoFixture(t)
	rep, err := f.svc.Tempo(context.Background(), "2026-W47")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.WriteCSV(&buf); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "tempo_w47.csv")
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(buf.String(), "\r\n", "\n"); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("csv mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
	if bytes.HasPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("csv must not start with a BOM")
	}
	// gamma (project) and planning (mode) have counted time but no ticket.
	if ids := strings.Join(rep.UnmappedIDs(), ","); ids != "gamma,planning" && ids != "planning,gamma" {
		t.Errorf("unmapped = %q", ids)
	}
	for _, r := range rep.Rows {
		if r.Hours <= 0 || strings.Contains(strings.Join(r.Contexts, ","), "break") {
			t.Errorf("bad row %+v", r)
		}
	}
}

func TestTempoRoundsAfterSumming(t *testing.T) {
	f := tempoFixture(t)
	rep, _ := f.svc.Tempo(context.Background(), "2026-W47")
	// acme 20 min + beta 20 min = 40 min -> 0.75 h. Rounding each block first
	// would give 0.25 + 0.25.
	if len(rep.Rows) == 0 || rep.Rows[0].Hours != 0.75 {
		t.Fatalf("rows = %+v", rep.Rows)
	}
}

func TestTempoEmptyWeekAndInvalid(t *testing.T) {
	f := tempoFixture(t)
	rep, err := f.svc.Tempo(context.Background(), "2026-W30")
	if err != nil || len(rep.Rows) != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	var buf bytes.Buffer
	_ = rep.WriteCSV(&buf)
	if buf.String() != "date,ticket,hours,context,description\r\n" {
		t.Errorf("header only expected, got %q", buf.String())
	}
	if _, err := f.svc.Tempo(context.Background(), "nope"); err == nil {
		t.Error("want error for bad week")
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{"": "", "plain": "plain", "=1+1": "'=1+1", "@x": "'@x", "-5": "'-5", "+1": "'+1"} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTempoDescriptionCapped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme", TempoTicket: "A-1"}); err != nil {
		t.Fatal(err)
	}
	f.switchNote(t, "2026-11-16 09:00", "acme", strings.Repeat("é", 900))
	f.switchNote(t, "2026-11-16 10:00", "off", "")
	f.clk.Set(at("2026-11-17 09:00"))
	rep, _ := f.svc.Tempo(ctx, "2026-W47")
	if len(rep.Rows) != 1 || len([]rune(rep.Rows[0].Description)) != maxDescription {
		t.Fatalf("%+v", rep.Rows)
	}
}
