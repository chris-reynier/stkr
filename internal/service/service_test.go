package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

var paris, _ = time.LoadLocation("Europe/Paris")

// at parses "2006-01-02 15:04" in Paris.
func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, paris)
	if err != nil {
		panic(err)
	}
	return t
}

type fixture struct {
	svc     *Service
	st      *store.Store
	clk     *clock.Fake
	backups string
	logs    *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "stkr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	backups := filepath.Join(dir, "backups")
	if _, err := st.Migrate(ctx, backups, at("2026-11-16 08:00")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Timezone = "Europe/Paris"
	clk := clock.NewFake(at("2026-11-16 09:00"))
	logs := &bytes.Buffer{}
	svc, err := New(st, clk, cfg, Deps{BackupDir: backups, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{svc: svc, st: st, clk: clk, backups: backups, logs: logs}
}

func (f *fixture) switchAt(t *testing.T, ts, ctxID string) domain.SwitchResult {
	t.Helper()
	f.clk.Set(at(ts))
	res, err := f.svc.Switch(context.Background(), ctxID, domain.SourceDeck, "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (f *fixture) addCtx(t *testing.T, id string, kind domain.ContextKind) {
	t.Helper()
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: id, Kind: kind}); err != nil {
		t.Fatal(err)
	}
}

func eventCount(t *testing.T, f *fixture) int {
	t.Helper()
	evs, err := f.st.EventsBetween(context.Background(), at("2000-01-01 00:00"), at("2100-01-01 00:00"))
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

func TestNewRejectsBadConfig(t *testing.T) {
	for name, mod := range map[string]func(*config.Config){
		"timezone":  func(c *config.Config) { c.Timezone = "Nowhere/Land" },
		"day_start": func(c *config.Config) { c.DayStart = "9" },
		"day_end":   func(c *config.Config) { c.DayEnd = "x" },
	} {
		cfg := config.Default()
		mod(cfg)
		if _, err := New(nil, clock.Real{}, cfg, Deps{}); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	s, err := New(nil, clock.Real{}, config.Default(), Deps{})
	if err != nil || s.Location() != time.Local || s.Config() == nil {
		t.Fatalf("defaults: %v", err)
	}
}

func TestDoubleSwitchIsIdempotent(t *testing.T) {
	f := newFixture(t)
	r1 := f.switchAt(t, "2026-11-16 09:00", "other")
	f.clk.Advance(1500 * time.Millisecond)
	r2, err := f.svc.Switch(context.Background(), "other", domain.SourceDeck, "")
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Created || r2.Created || r2.Event.ID != r1.Event.ID {
		t.Fatalf("r1=%+v r2=%+v", r1, r2)
	}
	if n := eventCount(t, f); n != 1 {
		t.Fatalf("double switch created %d events", n)
	}
	// After the debounce window, or to another context, a new event is made.
	f.clk.Advance(time.Second)
	if r3, _ := f.svc.Switch(context.Background(), "other", domain.SourceDeck, ""); !r3.Created {
		t.Fatal("switch after 2s should create an event")
	}
	if r4, _ := f.svc.Switch(context.Background(), "meeting", domain.SourceDeck, ""); !r4.Created {
		t.Fatal("switch to another context should create an event")
	}
	if n := eventCount(t, f); n != 3 {
		t.Fatalf("events = %d", n)
	}
}

func TestSwitchErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.svc.Switch(ctx, "nope", domain.SourceCLI, "")
	if !errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "stkr ctx add nope") {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := f.svc.Switch(ctx, "Bad Id", domain.SourceCLI, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := f.svc.Switch(ctx, "other", "phone", ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bad source: %v", err)
	}
	if _, err := f.svc.Switch(ctx, "other", domain.SourceCLI, strings.Repeat("x", MaxNoteLen+1)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("long note: %v", err)
	}
	_ = f.svc.ParkContext(ctx, "meeting")
	if _, err := f.svc.Switch(ctx, "meeting", domain.SourceCLI, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("parked: %v", err)
	}
	if n := eventCount(t, f); n != 0 {
		t.Fatalf("errors created %d events", n)
	}
}

func TestStopAndNow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if r, err := f.svc.Stop(ctx, domain.SourceCLI); err != nil || r.Created {
		t.Fatalf("stop with no events: %+v %v", r, err)
	}
	st, err := f.svc.Now(ctx)
	if err != nil || st.Tracking || st.Today != 0 {
		t.Fatalf("now empty: %+v %v", st, err)
	}
	f.switchAt(t, "2026-11-16 09:00", "other")
	f.clk.Set(at("2026-11-16 09:45"))
	st, _ = f.svc.Now(ctx)
	if !st.Tracking || st.Context.ID != "other" || st.Elapsed.D() != 45*time.Minute || st.Today.D() != 45*time.Minute || st.IdleSuspect {
		t.Fatalf("now: %+v", st)
	}
	if !st.Since.Equal(at("2026-11-16 09:00")) || st.Since.Location().String() != "Europe/Paris" {
		t.Fatalf("since: %v", st.Since)
	}
	f.clk.Set(at("2026-11-16 11:00"))
	if st, _ = f.svc.Now(ctx); !st.IdleSuspect || st.Today != 0 {
		t.Fatalf("idle now: %+v", st)
	}
	r, err := f.svc.Stop(ctx, domain.SourceCLI)
	if err != nil || !r.Created || r.Event.ContextID != domain.ContextOff {
		t.Fatalf("stop: %+v %v", r, err)
	}
	f.clk.Advance(time.Minute)
	if r, _ := f.svc.Stop(ctx, domain.SourceCLI); r.Created {
		t.Fatal("second stop should not create an event")
	}
	if st, _ = f.svc.Now(ctx); st.Tracking {
		t.Fatalf("after stop: %+v", st)
	}
	if _, err := f.svc.ConfirmEvent(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if st, _ = f.svc.Now(ctx); st.Today.D() != 2*time.Hour {
		t.Fatalf("confirmed today: %v", st.Today.D())
	}
}

func TestDayReport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.addCtx(t, "acme", domain.KindProject)
	f.switchAt(t, "2026-11-16 09:30", "acme")
	f.switchAt(t, "2026-11-16 10:30", "break")
	f.switchAt(t, "2026-11-16 11:00", "meeting")
	f.switchAt(t, "2026-11-16 13:00", "acme") // meeting block 2h: idle-suspect
	f.clk.Set(at("2026-11-16 14:00"))
	if _, err := f.svc.Stop(ctx, domain.SourceCLI); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(at("2026-11-17 10:00"))
	rep, err := f.svc.Day(ctx, "yesterday")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Date != "2026-11-16" || rep.Weekday != "Monday" || !rep.WorkingDay || len(rep.Events) != 5 || len(rep.Blocks) != 4 {
		t.Fatalf("report: %+v", rep)
	}
	if rep.Counted.D() != 2*time.Hour+30*time.Minute || rep.IdleSuspect.D() != 2*time.Hour {
		t.Fatalf("counted %v idle %v", rep.Counted.D(), rep.IdleSuspect.D())
	}
	// Gaps: 09:00-09:30 and 14:00-17:00.
	if len(rep.Gaps) != 2 || rep.GapTotal.D() != 3*time.Hour+30*time.Minute {
		t.Fatalf("gaps: %+v", rep.Gaps)
	}
	if rep.Blocks[2].Label != "Meeting" || rep.Blocks[2].Kind != domain.KindMode || !rep.Blocks[2].IdleSuspect {
		t.Fatalf("block enrichment: %+v", rep.Blocks[2])
	}
	if len(rep.Totals) != 2 || rep.Totals[0].ContextID != "acme" || rep.Totals[0].Counted.D() != 2*time.Hour || rep.Totals[1].Exportable {
		t.Fatalf("totals: %+v", rep.Totals)
	}
	if rep.Events[0].TS.Location().String() != "Europe/Paris" || !rep.WindowStart.Equal(at("2026-11-16 09:00")) {
		t.Fatal("times must be local")
	}
	same, _ := f.svc.Day(ctx, "2026-11-16")
	if same.Counted != rep.Counted {
		t.Fatal("explicit date differs")
	}
	for _, bad := range []string{"16/11/2026", "2026-13-01", "tomorrow"} {
		if _, err := f.svc.Day(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Day(%q): %v", bad, err)
		}
	}
}

func TestWeekReport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.addCtx(t, "acme", domain.KindProject)
	f.addCtx(t, "gym", domain.KindPersonal)
	_, _ = f.svc.UpdateContext(ctx, "acme", ContextPatch{TempoTicket: ptr("AC-1")})
	f.switchAt(t, "2026-11-16 09:00", "acme")
	f.switchAt(t, "2026-11-16 10:00", "gym")
	f.switchAt(t, "2026-11-16 10:30", domain.ContextOff)
	f.switchAt(t, "2026-11-18 09:00", "acme")
	f.switchAt(t, "2026-11-18 09:45", domain.ContextOff)
	f.switchAt(t, "2026-11-21 10:00", "acme") // Saturday
	f.switchAt(t, "2026-11-21 10:15", domain.ContextOff)
	f.clk.Set(at("2026-11-23 12:00"))

	rep, err := f.svc.Week(ctx, "2026-w47")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Week != "2026-W47" || rep.Start != "2026-11-16" || rep.End != "2026-11-22" || len(rep.Days) != 7 {
		t.Fatalf("week header: %+v", rep)
	}
	if rep.WorkingDays != 2 || rep.Counted.D() != 2*time.Hour+30*time.Minute {
		t.Fatalf("working %d counted %v", rep.WorkingDays, rep.Counted.D())
	}
	if rep.Totals[0].ContextID != "acme" || rep.Totals[0].Counted.D() != 2*time.Hour {
		t.Fatalf("totals: %+v", rep.Totals)
	}
	rows := ExportRows(rep)
	if len(rows) != 3 {
		t.Fatalf("export rows: %+v", rows)
	}
	for _, r := range rows {
		if r.Total.ContextID != "acme" || r.Total.TempoTicket != "AC-1" {
			t.Fatalf("personal time exported or ticket missing: %+v", r)
		}
	}
	cur, err := f.svc.Week(ctx, "")
	if err != nil || cur.Week != "2026-W48" || cur.Start != "2026-11-23" {
		t.Fatalf("current week: %+v %v", cur.Week, err)
	}
	for _, bad := range []string{"2026-47", "2025-W53", "2026-W00", "2026-W7", "x"} {
		if _, err := f.svc.Week(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Week(%q): %v", bad, err)
		}
	}
	if w, err := f.svc.Week(ctx, "2026-W53"); err != nil || w.Start != "2026-12-28" {
		t.Errorf("2026-W53: %v %v", w.Start, err)
	}
	if w, err := f.svc.Week(ctx, "2027-W01"); err != nil || w.Start != "2027-01-04" {
		t.Errorf("2027-W01: %v %v", w.Start, err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestContexts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c, err := f.svc.AddContext(ctx, domain.Context{ID: "acme"})
	if err != nil || c.Label != "acme" || c.Kind != domain.KindProject || !c.Active {
		t.Fatalf("add defaults: %+v %v", c, err)
	}
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dup: %v", err)
	}
	for name, bad := range map[string]domain.Context{
		"id":     {ID: "-x"},
		"kind":   {ID: "x", Kind: "work"},
		"label":  {ID: "x", Label: strings.Repeat("l", 101)},
		"ticket": {ID: "x", TempoTicket: strings.Repeat("t", 1001)},
	} {
		if _, err := f.svc.AddContext(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	u, err := f.svc.UpdateContext(ctx, "acme", ContextPatch{
		Label: ptr("ACME"), Kind: ptr(domain.KindMode), TempoTicket: ptr(" AC-9 "),
		NotebookURL: ptr("https://nb"), Workdir: ptr("/src"), Icon: ptr("star"), Sort: ptr(3),
	})
	if err != nil || u.Label != "ACME" || u.Kind != domain.KindMode || u.TempoTicket != "AC-9" || u.Sort != 3 || u.Icon != "star" {
		t.Fatalf("update: %+v %v", u, err)
	}
	if got, _ := f.svc.GetContext(ctx, "acme"); got != u {
		t.Fatalf("get after update: %+v", got)
	}
	if _, err := f.svc.UpdateContext(ctx, "acme", ContextPatch{Label: ptr(" ")}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty label: %v", err)
	}
	if _, err := f.svc.UpdateContext(ctx, "off", ContextPatch{Kind: ptr(domain.KindProject)}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("off kind: %v", err)
	}
	if _, err := f.svc.UpdateContext(ctx, "nope", ContextPatch{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := f.svc.UpdateContext(ctx, "NO", ContextPatch{}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := f.svc.GetContext(ctx, "NO"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("get bad id: %v", err)
	}
	modes, _ := f.svc.ListContexts(ctx, ContextFilter{Kind: domain.KindMode})
	if len(modes) != 3 {
		t.Fatalf("modes: %+v", modes)
	}
	if _, err := f.svc.ListContexts(ctx, ContextFilter{Kind: "x"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if err := f.svc.ParkContext(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	active, _ := f.svc.ListContexts(ctx, ContextFilter{})
	all, _ := f.svc.ListContexts(ctx, ContextFilter{IncludeParked: true})
	if len(active) != 5 || len(all) != 6 {
		t.Fatalf("park: %d %d", len(active), len(all))
	}
	if err := f.svc.ActivateContext(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ParkContext(ctx, "off"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("park off: %v", err)
	}
	for _, err := range []error{f.svc.ParkContext(ctx, "Bad"), f.svc.ActivateContext(ctx, "Bad")} {
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("bad id: %v", err)
		}
	}
	if err := f.svc.ActivateContext(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("activate missing: %v", err)
	}
}

func TestEventEditing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	e1 := f.switchAt(t, "2026-11-16 09:00", "other").Event
	e2 := f.switchAt(t, "2026-11-16 10:00", "meeting").Event
	f.clk.Set(at("2026-11-16 11:00"))

	// Edit.
	ed, err := f.svc.EditEvent(ctx, e1.ID, EventPatch{TS: ptr(at("2026-11-16 09:15")), ContextID: ptr("planning"), Note: ptr("n1")})
	if err != nil || ed.ContextID != "planning" || ed.Note != "n1" || !ed.TS.Equal(at("2026-11-16 09:15")) {
		t.Fatalf("edit: %+v %v", ed, err)
	}
	for name, p := range map[string]EventPatch{
		"future":    {TS: ptr(at("2026-11-16 12:00"))},
		"unknown":   {ContextID: ptr("nope")},
		"bad id":    {ContextID: ptr("Bad")},
		"long note": {Note: ptr(strings.Repeat("x", MaxNoteLen+1))},
	} {
		if _, err := f.svc.EditEvent(ctx, e1.ID, p); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := f.svc.EditEvent(ctx, 999, EventPatch{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}

	// Split: must be inside (event, min(next, now)).
	for _, bad := range []string{"2026-11-16 09:15", "2026-11-16 10:00", "2026-11-16 08:00"} {
		if _, err := f.svc.SplitEvent(ctx, e1.ID, at(bad)); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("split at %s: %v", bad, err)
		}
	}
	if _, err := f.svc.SplitEvent(ctx, e2.ID, at("2026-11-16 11:30")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("split last after now: %v", err)
	}
	sp, err := f.svc.SplitEvent(ctx, e1.ID, at("2026-11-16 09:30"))
	if err != nil || sp.ContextID != "planning" || sp.Note != "" {
		t.Fatalf("split: %+v %v", sp, err)
	}
	if _, err := f.svc.SplitEvent(ctx, e2.ID, at("2026-11-16 10:30")); err != nil {
		t.Fatalf("split last block: %v", err)
	}
	if _, err := f.svc.SplitEvent(ctx, 999, at("2026-11-16 10:30")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	rep, _ := f.svc.Day(ctx, "")
	if len(rep.Blocks) != 4 {
		t.Fatalf("after splits: %d blocks", len(rep.Blocks))
	}

	// Merge: adjacency, any order, notes joined.
	if _, err := f.svc.MergeEvents(ctx, e1.ID, e2.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("non-adjacent: %v", err)
	}
	if _, err := f.svc.MergeEvents(ctx, e1.ID, e1.ID); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("self: %v", err)
	}
	if _, err := f.svc.MergeEvents(ctx, e1.ID, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := f.svc.MergeEvents(ctx, 999, e1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing first: %v", err)
	}
	_, _ = f.svc.EditEvent(ctx, sp.ID, EventPatch{Note: ptr("n2")})
	m, err := f.svc.MergeEvents(ctx, sp.ID, e1.ID)
	if err != nil || m.ID != e1.ID || m.Note != "n1; n2" {
		t.Fatalf("merge: %+v %v", m, err)
	}
	if _, err := f.svc.GetEvent(ctx, sp.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("merged event still exists")
	}
	_, _ = f.svc.EditEvent(ctx, e1.ID, EventPatch{Note: ptr("")})
	_, _ = f.svc.EditEvent(ctx, e2.ID, EventPatch{Note: ptr("only")})
	if m, _ = f.svc.MergeEvents(ctx, e1.ID, e2.ID); m.Note != "only" {
		t.Fatalf("note from second: %q", m.Note)
	}

	// Delete and confirm.
	if err := f.svc.DeleteEvent(ctx, e1.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteEvent(ctx, e1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := f.svc.ConfirmEvent(ctx, e1.ID, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConfirmMakesIdleBlockCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	e := f.switchAt(t, "2026-11-16 09:00", "other").Event
	f.switchAt(t, "2026-11-16 12:00", domain.ContextOff)
	rep, _ := f.svc.Day(ctx, "")
	if rep.Counted != 0 || rep.IdleSuspect.D() != 3*time.Hour {
		t.Fatalf("before: %+v", rep)
	}
	c, err := f.svc.ConfirmEvent(ctx, e.ID, true)
	if err != nil || !c.Confirmed {
		t.Fatal(err)
	}
	rep, _ = f.svc.Day(ctx, "")
	if rep.Counted.D() != 3*time.Hour || rep.IdleSuspect != 0 {
		t.Fatalf("after: %+v", rep)
	}
	_, _ = f.svc.ConfirmEvent(ctx, e.ID, false)
	if rep, _ = f.svc.Day(ctx, ""); rep.Counted != 0 {
		t.Fatal("undo confirm")
	}
}

func TestDailyBackupKeeps14Generations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.clk.Set(at("2026-11-01 08:00"))
	for day := 0; day < 20; day++ {
		p, created, err := f.svc.DailyBackup(ctx)
		if err != nil || !created || p == "" {
			t.Fatalf("day %d: %v %v", day, created, err)
		}
		f.clk.Advance(3 * time.Hour)
		if _, created, _ := f.svc.DailyBackup(ctx); created {
			t.Fatalf("day %d: second backup on the same day", day)
		}
		f.clk.Advance(21 * time.Hour)
	}
	list, err := f.svc.Backups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != BackupGenerations {
		t.Fatalf("kept %d backups", len(list))
	}
	// The oldest six are gone: the first kept is day 7 (2026-11-07).
	if !strings.Contains(filepath.Base(list[0]), "20261107") {
		t.Fatalf("oldest kept: %s", list[0])
	}
	// Two forced backups in the same second get distinct names.
	p1, err1 := f.svc.Backup(ctx)
	p2, err2 := f.svc.Backup(ctx)
	if err1 != nil || err2 != nil || p1 == p2 {
		t.Fatalf("same-second backups: %s %s %v %v", p1, p2, err1, err2)
	}
	if list, _ = f.svc.Backups(); len(list) != BackupGenerations || list[len(list)-1] != p2 {
		t.Fatalf("after forced: %v", list)
	}
	// Pre-migration backups are never pruned.
	pre := filepath.Join(f.backups, "pre-migrate-0-x.db")
	_ = os.WriteFile(pre, nil, 0o600)
	_, _ = f.svc.Backup(ctx)
	if _, err := os.Stat(pre); err != nil {
		t.Fatal("pre-migration backup was pruned")
	}
}

func TestBackupWithoutDir(t *testing.T) {
	f := newFixture(t)
	f.svc.backupDir = ""
	if _, err := f.svc.Backup(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	f.svc.backupDir = filepath.Join(t.TempDir(), "missing")
	if _, err := f.svc.Backups(); err == nil {
		t.Fatal("want error for missing dir")
	}
}

// Notes are user content and must never reach the log.
func TestLogsDoNotContainNotes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const secret = "call the lawyer about the merger"
	r, err := f.svc.Switch(ctx, "other", domain.SourceCLI, secret)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.svc.EditEvent(ctx, r.Event.ID, EventPatch{Note: ptr(secret + " edited")})
	out := f.logs.String()
	if strings.Contains(out, "lawyer") {
		t.Fatalf("note leaked into log: %s", out)
	}
	if !strings.Contains(out, "len=32") {
		t.Fatalf("expected redacted note length in log: %s", out)
	}
}

func TestOpen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	clk := clock.NewFake(at("2026-11-16 09:00"))
	s, err := Open(ctx, filepath.Join(dir, "stkr.db"), clk, config.Default(), Deps{BackupDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Switch(ctx, "other", domain.SourceCLI, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, filepath.Join(dir, "no", "such", "x.db"), clk, config.Default(), Deps{}); err == nil {
		t.Fatal("want open error")
	}
	bad := config.Default()
	bad.Timezone = "Nowhere/Land"
	if _, err := Open(ctx, filepath.Join(dir, "stkr.db"), clk, bad, Deps{BackupDir: dir}); err == nil {
		t.Fatal("want config error")
	}
}
