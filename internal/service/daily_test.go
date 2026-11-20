package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
	"github.com/chris-reynier/stkr/internal/testutil"
)

type dailyFix struct {
	*fixture
	clip     *testutil.FakeClipboard
	win      *testutil.FakeWindows
	od       string // onedrive_dir
	prompts  string
	loggedTo *bytes.Buffer
}

// newDailyFixture builds a service on Monday 2026-11-16 09:00 (Paris) with a
// fake clipboard, fake windows and an export folder.
func newDailyFixture(t *testing.T, mutate ...func(*config.Config)) *dailyFix {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "stkr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(ctx, filepath.Join(dir, "backups"), at("2026-11-16 08:00")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Timezone = "Europe/Paris"
	cfg.OneDriveDir = filepath.Join(dir, "onedrive")
	for _, m := range mutate {
		m(cfg)
	}
	clk := clock.NewFake(at("2026-11-16 09:00"))
	logs := &bytes.Buffer{}
	f := &dailyFix{clip: &testutil.FakeClipboard{}, win: &testutil.FakeWindows{Present: map[string]bool{}}, od: cfg.OneDriveDir,
		prompts: filepath.Join(dir, "prompts"), loggedTo: logs}
	svc, err := New(st, clk, cfg, Deps{
		BackupDir: filepath.Join(dir, "backups"), Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Clipboard: f.clip, Windows: f.win, PromptsDir: f.prompts,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.fixture = &fixture{svc: svc, st: st, clk: clk, logs: logs}
	return f
}

// workDay records a tracked day for ctxID: 09:00 to 17:00 then off.
func (f *dailyFix) workDay(t *testing.T, date, ctxID string) {
	t.Helper()
	f.switchOK(t, date+" 09:00", ctxID)
	f.switchOK(t, date+" 17:00", domain.ContextOff)
}

func (f *dailyFix) read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.od, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f *dailyFix) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.od, filepath.FromSlash(rel)))
	return err == nil
}

func TestStartDay(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	f.addCtx(t, "alpha", domain.KindProject)
	f.addCtx(t, "home", domain.KindPersonal)
	// Last week: Mon..Fri on alpha; Wednesday has a personal block as well.
	for _, d := range []string{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13"} {
		f.workDay(t, d, "alpha")
	}
	f.switchOK(t, "2026-11-11 12:00", "home")
	f.switchOK(t, "2026-11-11 13:00", "alpha")
	f.switchOK(t, "2026-11-13 17:00", domain.ContextOff)
	// Previous Friday was closed with a top 3; Thursday was not closed.
	if err := f.st.UpsertDay(ctx, domain.Day{Date: "2026-11-13", Top: [3]string{"Finish API", "", "Review"}, CloseSaved: true}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-11-09", "2026-11-10", "2026-11-11"} {
		if err := f.st.UpsertDay(ctx, domain.Day{Date: d, CloseSaved: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Items: one open with due date, a done one, a personal one.
	for _, it := range []domain.Item{
		{Date: "2026-11-13", Project: "alpha", Type: domain.ItemAction, Summary: "Ship it", Due: "2026-11-17", Fingerprint: "aaaaaaaaaaaaaaaa"},
		{Date: "2026-11-13", Project: "alpha", Type: domain.ItemNote, Summary: "Old news", Status: "done", Fingerprint: "bbbbbbbbbbbbbbbb"},
		{Date: "2026-11-13", Project: "home", Type: domain.ItemAction, Summary: "Buy milk", Fingerprint: "cccccccccccccccc"},
	} {
		if _, _, err := f.st.InsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	for i, tag := range []string{"pro", "pro", "personal"} {
		if _, err := f.st.InsertIdea(ctx, domain.Idea{TS: at("2026-11-13 10:00"), Tag: tag, Title: fmt.Sprintf("idea %d", i), RawText: "raw"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.AgentEvent(ctx, AgentEvent{Instance: "a1", Event: "state", State: "action_required", Summary: "approve me"}); err != nil {
		t.Fatal(err)
	}

	f.clk.Set(at("2026-11-16 09:05"))
	res, err := f.svc.StartDay(ctx, StartDayOptions{Source: domain.SourceCLI, Clipboard: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Clipboard || res.Chars != len([]rune(res.Prompt)) || f.clip.Text() != res.Prompt {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{
		"Monday 2026-11-16", "```stkr-brief v1",
		"## Yesterday's top 3 (2026-11-13)", "1. Finish API", "3. Review",
		"- action | alpha | Ship it (due 2026-11-17) #aaaaaaaaaaaaaaaa",
		"## Untriaged ideas (2)", "- idea 0",
		"- a1: action_required - approve me",
		"Fri 2026-11-13: 8h00m total; alpha 8h00m",
		"## Previous days not closed", "- 2026-11-12: close was not saved",
	} {
		if !strings.Contains(res.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, res.Prompt)
		}
	}
	for _, bad := range []string{"Buy milk", "Old news", "home", "idea 2", "2026-11-13: close was not saved"} {
		if strings.Contains(res.Prompt, bad) {
			t.Errorf("prompt must not contain %q", bad)
		}
	}
	// Wednesday has 1h of personal time which is not shown or counted.
	if !strings.Contains(res.Prompt, "Wed 2026-11-11: 7h00m total") {
		t.Errorf("personal time leaked into the digest:\n%s", res.Prompt)
	}
	// planning was logged.
	now, err := f.svc.Now(ctx)
	if err != nil || !now.Tracking || now.Context.ID != "planning" {
		t.Fatalf("now = %+v %v", now, err)
	}
	// The digest file holds the digest, not the instructions.
	dg := f.read(t, "_daily/daily-digest.md")
	if !strings.HasPrefix(dg, "# Daily digest 2026-11-16\n") || strings.Contains(dg, "stkr-brief") || !strings.Contains(dg, "Finish API") {
		t.Errorf("digest file:\n%s", dg)
	}
	if res.Exported != "_daily/daily-digest.md" {
		t.Errorf("exported = %q", res.Exported)
	}
}

func TestStartDayOptions(t *testing.T) {
	ctx := context.Background()

	// Export disabled and no clipboard: no file, no clipboard write.
	f := newDailyFixture(t, func(c *config.Config) { c.OneDriveDir = "" })
	res, err := f.svc.StartDay(ctx, StartDayOptions{})
	if err != nil || res.Clipboard || res.Exported != "" || f.clip.Text() != "" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if !strings.Contains(res.Prompt, "Nothing recorded yet.") {
		t.Errorf("empty digest text missing:\n%s", res.Prompt)
	}

	// Clipboard failure: the day was started, the result is filled in, the error is unavailable.
	f = newDailyFixture(t)
	f.clip.Err = domain.New(domain.ErrUnavailable, "no clipboard")
	f.clk.Set(at("2026-11-16 09:30"))
	res, err = f.svc.StartDay(ctx, StartDayOptions{Clipboard: true})
	if !errors.Is(err, domain.ErrUnavailable) || res.Prompt == "" || res.Clipboard {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if !f.exists("_daily/daily-digest.md") {
		t.Error("digest file must still be written")
	}
	if n, _ := f.svc.Now(ctx); !n.Tracking || n.Context.ID != "planning" {
		t.Error("planning must be logged even if the clipboard fails")
	}

	// A parked planning context is a conflict and nothing else happens.
	f = newDailyFixture(t)
	if err := f.svc.ParkContext(ctx, "planning"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartDay(ctx, StartDayOptions{Clipboard: true}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v", err)
	}

	// Export folder that cannot be written: warning, not failure.
	f = newDailyFixture(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.svc.cfg.OneDriveDir = filepath.Join(blocker, "sub")
	res, err = f.svc.StartDay(ctx, StartDayOptions{})
	if err != nil || len(res.Warnings) != 1 || res.Exported != "" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestStartDayHeavyHistoryFitsLimit(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("proj%d", i)
		f.addCtx(t, id, domain.KindProject)
	}
	for _, d := range []string{"2026-11-09", "2026-11-10", "2026-11-11", "2026-11-12", "2026-11-13"} {
		for i := 0; i < 12; i++ {
			f.switchOK(t, fmt.Sprintf("%s %02d:00", d, 7+i), fmt.Sprintf("proj%d", i))
		}
		f.switchOK(t, d+" 19:30", domain.ContextOff)
	}
	for i := 0; i < 60; i++ {
		it := domain.Item{Date: "2026-11-13", Project: "proj1", Type: domain.ItemAction, Summary: strings.Repeat("long summary ", 20) + fmt.Sprint(i),
			Due: "2026-12-01", Fingerprint: fmt.Sprintf("%016d", i)}
		if _, _, err := f.st.InsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.InsertIdea(ctx, domain.Idea{TS: at("2026-11-13 10:00"), Tag: "pro", Title: strings.Repeat("idea ", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		if _, err := f.svc.AgentEvent(ctx, AgentEvent{Instance: fmt.Sprintf("agent-%d", i), Event: "state", State: "error", Summary: strings.Repeat("boom ", 40)}); err != nil {
			t.Fatal(err)
		}
	}
	f.clk.Set(at("2026-11-16 09:30"))
	res, err := f.svc.StartDay(ctx, StartDayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dg := f.read(t, "_daily/daily-digest.md")
	body := strings.TrimPrefix(dg, "# Daily digest 2026-11-16\n\n")
	if n := len([]rune(strings.TrimSpace(body))); n > daily.MaxDigestChars {
		t.Fatalf("digest has %d chars", n)
	}
	if res.Chars > daily.MaxDigestChars+1500 {
		t.Errorf("prompt unexpectedly long: %d chars", res.Chars)
	}
	if !strings.Contains(dg, "more not shown") || !strings.Contains(dg, "Open items (12)") {
		t.Errorf("expected truncation:\n%s", dg)
	}
}

func TestClosePromptAndPaste(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t, func(c *config.Config) { c.AgentWindowTitle = "Chat Window" })
	f.addCtx(t, "alpha", domain.KindProject)
	f.switchOK(t, "2026-11-16 09:00", "alpha")
	if err := f.st.UpsertDay(ctx, domain.Day{Date: "2026-11-16", Top: [3]string{"Morning goal", "", ""}, BriefSaved: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.InsertItem(ctx, domain.Item{Date: "2026-11-16", Project: "alpha", Type: domain.ItemAction, Summary: "Today task", Fingerprint: "1111111111111111"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.InsertItem(ctx, domain.Item{Date: "2026-11-12", Project: "alpha", Type: domain.ItemFollowup, Summary: "Old task", Fingerprint: "2222222222222222"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.InsertIdea(ctx, domain.Idea{TS: at("2026-11-16 10:00"), Tag: "pro", Title: "Great idea"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.InsertIdea(ctx, domain.Idea{TS: at("2026-11-15 10:00"), Tag: "pro", Title: "Yesterday idea"}); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(at("2026-11-16 16:50"))

	res, err := f.svc.ClosePrompt(ctx, CloseOptions{})
	if err != nil || res.Clipboard || f.clip.Text() != "" {
		t.Fatalf("no-clipboard call: %+v %v", res, err)
	}
	for _, want := range []string{"Time today (2026-11-16)", "alpha 7h50m", "Morning top 3", "1. Morning goal", "Items captured today (1)",
		"#1111111111111111", "Other open items (1)", "#2222222222222222", "Ideas captured today (1)", "- Great idea", "```stkr-close v1"} {
		if !strings.Contains(res.Prompt, want) {
			t.Errorf("close prompt lacks %q:\n%s", want, res.Prompt)
		}
	}
	if strings.Contains(res.Prompt, "Yesterday idea") {
		t.Error("only today's ideas belong in the close prompt")
	}

	// Copy only.
	res, err = f.svc.ClosePrompt(ctx, CloseOptions{Clipboard: true})
	if err != nil || !res.Clipboard || res.Pasted || f.clip.Text() != res.Prompt || len(f.win.Pastes) != 0 {
		t.Fatalf("copy: %+v %v", res, err)
	}

	// Paste into the configured window.
	f.win.Present["Chat Window"] = true
	res, err = f.svc.ClosePrompt(ctx, CloseOptions{Paste: true, Enter: true})
	if err != nil || !res.Pasted || len(f.win.Pastes) != 1 || f.win.Pastes[0] != (testutil.PasteCall{Title: "Chat Window", Enter: true}) {
		t.Fatalf("paste: %+v %v %+v", res, err, f.win.Pastes)
	}
	// An explicit window overrides the setting; a missing one is not found.
	res, err = f.svc.ClosePrompt(ctx, CloseOptions{Paste: true, Window: "Nope"})
	if !errors.Is(err, domain.ErrNotFound) || !res.Clipboard || res.Pasted {
		t.Fatalf("missing window: %+v %v", res, err)
	}
	if f.clip.Text() != res.Prompt {
		t.Error("the prompt must be on the clipboard even when paste fails")
	}
	// No window title anywhere.
	g := newDailyFixture(t)
	if err := g.svc.Paste(ctx, "", false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("no title: %v", err)
	}
	if err := g.svc.Paste(ctx, strings.Repeat("x", 300), false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("long title: %v", err)
	}
	// Platform without paste support.
	g.win.Err = domain.New(domain.ErrUnavailable, "paste is only supported on Windows")
	if err := g.svc.Paste(ctx, "x", false); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("unavailable: %v", err)
	}
}

const brief = "Sure!\n```stkr-brief v1\ntop | 1 | Ship P05\ntop | 2 | Review PRs\nitem | action | alpha | Write docs | 2026-11-20\nitem | decision | Alpha Project | Use SQLite |\nitem | risk | unknown thing | Slow CI |\nitem | action | home | Buy milk |\n```\n"

func TestImportBriefAndDuplicates(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "alpha", Label: "Alpha Project", Kind: domain.KindProject}); err != nil {
		t.Fatal(err)
	}
	f.addCtx(t, "home", domain.KindPersonal)

	res, err := f.svc.Import(ctx, brief, ViaAPI)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.State != ImportOK || res.Kind != "brief" || res.ItemsCreated != 4 || res.TopsSaved != 2 || res.Format != "fenced" {
		t.Fatalf("result = %+v", res)
	}
	day, err := f.svc.DayPlan(ctx, "today")
	if err != nil || day.Top != [3]string{"Ship P05", "Review PRs", ""} || !day.BriefSaved || day.CloseSaved {
		t.Fatalf("day = %+v %v", day, err)
	}
	items, _ := f.svc.ItemsOn(ctx, "today")
	if len(items) != 4 {
		t.Fatalf("items = %+v", items)
	}
	// Label resolved to the context id; unknown project kept as text.
	byType := map[string]string{}
	for _, it := range items {
		byType[string(it.Type)+"/"+it.Summary] = it.Project
	}
	if byType["decision/Use SQLite"] != "alpha" || byType["risk/Slow CI"] != "unknown thing" || items[0].Source != ViaAPI || items[0].Origin != "brief" {
		t.Errorf("projects = %v, first = %+v", byType, items[0])
	}

	// Export: alpha folder, inbox with the unknown project and the top 3, nothing for the personal item.
	alpha := f.read(t, "alpha/log.md")
	for _, want := range []string{"# Alpha Project", "- [ ] action: Write docs (due 2026-11-20)", "### 2026-11-16", "- decision: Use SQLite"} {
		if !strings.Contains(alpha, want) {
			t.Errorf("alpha/log.md lacks %q:\n%s", want, alpha)
		}
	}
	inbox := f.read(t, "_daily/inbox.md")
	if !strings.Contains(inbox, "risk: Slow CI") || !strings.Contains(inbox, "Top 1 (2026-11-16): Ship P05") {
		t.Errorf("inbox:\n%s", inbox)
	}
	if f.exists("home/log.md") || strings.Contains(inbox, "milk") || strings.Contains(alpha, "milk") {
		t.Error("personal items must not be exported")
	}
	if len(res.Exported) != 2 {
		t.Errorf("exported = %v", res.Exported)
	}

	// Duplicate import: no new items, state ok, files untouched.
	before := f.read(t, "alpha/log.md")
	res, err = f.svc.Import(ctx, brief, ViaClipboard)
	if err != nil || res.ItemsCreated != 0 || res.ItemsExisting != 4 || res.State != ImportOK || len(res.Exported) != 0 {
		t.Fatalf("duplicate: %+v %v", res, err)
	}
	if items, _ = f.svc.ItemsOn(ctx, "today"); len(items) != 4 || f.read(t, "alpha/log.md") != before {
		t.Error("a duplicate import must not change anything")
	}
	// Same summary with different case/spacing is the same item.
	res, _ = f.svc.Import(ctx, "```stkr-close v1\nitem | action | ALPHA | write   docs. | 2026-11-20\n```", ViaAPI)
	if res.ItemsCreated != 0 || res.ItemsExisting != 1 {
		t.Errorf("normalised duplicate: %+v", res)
	}
}

func TestImportCloseDone(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	f.addCtx(t, "alpha", domain.KindProject)
	if _, err := f.svc.Import(ctx, "```stkr-brief v1\nitem | action | alpha | First thing |\nitem | followup | alpha | Second thing |\nitem | action | alpha | Third thing |\n```", ViaAPI); err != nil {
		t.Fatal(err)
	}
	items, _ := f.svc.ItemsOn(ctx, "today")
	fp := items[0].Fingerprint
	res, err := f.svc.Import(ctx, "```stkr-close v1\ndone | "+fp+"\ndone | second THING.\ndone | nothing like this\nitem | note | alpha | A new note |\n```", ViaAPI)
	if err != nil {
		t.Fatal(err)
	}
	if res.ItemsDone != 2 || res.ItemsCreated != 1 || res.State != ImportWarn || res.Kind != "close" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(strings.Join(res.Warnings, "|"), "1 done line(s) matched no open item") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	open, _ := f.st.ListItems(ctx, "", "open")
	if len(open) != 2 { // Third thing + the new note
		t.Errorf("open = %+v", open)
	}
	day, _ := f.svc.DayPlan(ctx, "today")
	if !day.CloseSaved || !day.BriefSaved {
		t.Errorf("day = %+v", day)
	}
	alpha := f.read(t, "alpha/log.md")
	if !strings.Contains(alpha, "- [x] action: First thing") || !strings.Contains(alpha, "- [x] followup: Second thing") || !strings.Contains(alpha, "- [ ] action: Third thing") {
		t.Errorf("log:\n%s", alpha)
	}
	focus := alpha[strings.Index(alpha, "## Current focus"):strings.Index(alpha, "## Log")]
	if strings.Contains(focus, "First thing") || !strings.Contains(focus, "Third thing") {
		t.Errorf("focus lists done items or misses open ones:\n%s", focus)
	}
}

func TestImportNoValidBlock(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	for _, text := range []string{"", "just chatting", "```stkr-brief v1\n```", "```stkr-brief v1\ntop | 1 | <priority text>\n```"} {
		res, err := f.svc.Import(ctx, text, ViaUI)
		var nb *NoValidBlockError
		if !errors.As(err, &nb) || !errors.Is(err, domain.ErrInvalid) || res.OK || res.Redirect != ImportRedirect || res.State != ImportFail {
			t.Fatalf("%q: %+v %v", text, res, err)
		}
	}
	items, _ := f.svc.ItemsOn(ctx, "today")
	day, _ := f.svc.DayPlan(ctx, "today")
	if len(items) != 0 || day.BriefSaved || day.CloseSaved || day.Top != [3]string{} {
		t.Error("a failed import must write nothing")
	}
	if f.exists("_daily/inbox.md") {
		t.Error("a failed import must not export")
	}
	if s, _ := f.svc.ImportKeyState(ctx); s != ImportFail {
		t.Errorf("key = %q", s)
	}
	// Oversize text.
	if _, err := f.svc.Import(ctx, strings.Repeat("x", maxImportBytes+1), ViaAPI); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("oversize: %v", err)
	}
}

func TestImportKeyStateExpires(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	if s, err := f.svc.ImportKeyState(ctx); err != nil || s != "" {
		t.Fatalf("initial = %q %v", s, err)
	}
	// Pipe fallback gives a warning.
	res, err := f.svc.Import(ctx, "top | 1 | only a plain line", ViaCLI)
	if err != nil || res.State != ImportWarn || res.Format != "pipe" {
		t.Fatalf("%+v %v", res, err)
	}
	if s, _ := f.svc.ImportKeyState(ctx); s != ImportWarn {
		t.Errorf("key = %q", s)
	}
	f.clk.Advance(9 * time.Second)
	if s, _ := f.svc.ImportKeyState(ctx); s != ImportWarn {
		t.Errorf("key after 9s = %q", s)
	}
	f.clk.Advance(2 * time.Second)
	if s, _ := f.svc.ImportKeyState(ctx); s != "" {
		t.Errorf("key after 11s = %q", s)
	}
	// Clean import is green.
	res, _ = f.svc.Import(ctx, "```stkr-brief v1\ntop | 2 | clean\n```", ViaAPI)
	if s, _ := f.svc.ImportKeyState(ctx); res.State != ImportOK || s != ImportOK {
		t.Errorf("clean = %+v key %q", res, s)
	}
}

func TestImportClipboard(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	f.clip.Set(brief)
	res, err := f.svc.ImportClipboard(ctx)
	if err != nil || res.ItemsCreated != 4 {
		t.Fatalf("%+v %v", res, err)
	}
	f.clip.Err = domain.New(domain.ErrUnavailable, "no clipboard")
	if _, err := f.svc.ImportClipboard(ctx); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportClipboardContentNeverLogged(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t, func(c *config.Config) { c.AgentWindowTitle = "Chat" })
	f.win.Present["Chat"] = true
	const secret = "SECRET-PAYLOAD-12345"
	f.clip.Set("```stkr-brief v1\ntop | 1 | " + secret + "\nitem | action | alpha | " + secret + " item |\nbogus " + secret + "\n```")
	if _, err := f.svc.ImportClipboard(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Import(ctx, "no block here "+secret, ViaAPI); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := f.svc.StartDay(ctx, StartDayOptions{Clipboard: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ClosePrompt(ctx, CloseOptions{Paste: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.loggedTo.String(), secret) {
		t.Fatalf("clipboard content reached the logs:\n%s", f.loggedTo)
	}
	if !strings.Contains(f.loggedTo.String(), "import") {
		t.Error("import should still be logged (lengths and counts only)")
	}
}

func TestExportKeepsNotesAcrossExports(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	f.addCtx(t, "alpha", domain.KindProject)
	f.workDay(t, "2026-11-16", "alpha")
	f.clk.Set(at("2026-11-16 18:00"))
	imp := func(n int) {
		t.Helper()
		if _, err := f.svc.Import(ctx, fmt.Sprintf("```stkr-close v1\nitem | action | alpha | Step %d |\n```", n), ViaAPI); err != nil {
			t.Fatal(err)
		}
	}
	imp(1)
	path := filepath.Join(f.od, "alpha", "log.md")
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "- Time: 8h00m") {
		t.Errorf("tracked hours missing:\n%s", b)
	}
	// The user edits the file: intro, own notes and an unknown section.
	userNotes := "## Notes\nMy notes \u00e9\u00e8\n\n- keep me exactly\n   trailing spaces   \n"
	edited := strings.Replace(string(b), "## Notes\n\n", "Intro by hand.\n\n## Ideas\n\nmine\n\n"+userNotes, 1)
	if edited == string(b) {
		t.Fatal("test setup: Notes section not found")
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 4; i++ {
		imp(i)
		got, _ := os.ReadFile(path)
		if !strings.HasSuffix(string(got), userNotes) || !strings.Contains(string(got), "## Ideas\n\nmine\n\n") {
			t.Fatalf("export %d changed user content:\n%s", i, got)
		}
		if !strings.Contains(string(got), fmt.Sprintf("Step %d", i)) {
			t.Fatalf("export %d lacks its item", i)
		}
	}
	// ExportAll rewrites nothing when nothing changed.
	written, err := f.svc.ExportAll(ctx)
	if err != nil || len(written) != 0 {
		t.Errorf("ExportAll = %v %v", written, err)
	}
	// ... and recreates a deleted file.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	written, err = f.svc.ExportAll(ctx)
	if err != nil || len(written) != 1 || written[0] != "alpha/log.md" {
		t.Errorf("ExportAll after delete = %v %v", written, err)
	}
	// Export is off without onedrive_dir.
	g := newDailyFixture(t, func(c *config.Config) { c.OneDriveDir = "" })
	if w, err := g.svc.ExportAll(ctx); err != nil || w != nil {
		t.Errorf("disabled ExportAll = %v %v", w, err)
	}
}

func TestExportKeyRules(t *testing.T) {
	ctxs := map[string]domain.Context{
		"alpha":    {ID: "alpha", Kind: domain.KindProject},
		"other":    {ID: "other", Kind: domain.KindProject},
		"planning": {ID: "planning", Kind: domain.KindMode},
		"break":    {ID: "break", Kind: domain.KindPersonal},
		"off":      {ID: "off", Kind: domain.KindSystem},
	}
	for project, want := range map[string]string{"alpha": "alpha", "other": inboxKey, "planning": inboxKey, "break": "", "off": "", "mystery": inboxKey} {
		if got := exportKey(ctxs, project); got != want {
			t.Errorf("exportKey(%q) = %q, want %q", project, got, want)
		}
	}
	f := newDailyFixture(t)
	if err := f.svc.writeExport("../escape.md", []byte("x")); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("path escape: %v", err)
	}
}

func TestCloseStateAndBlink(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t) // close_reminder 16:45
	f.addCtx(t, "alpha", domain.KindProject)
	f.switchOK(t, "2026-11-16 09:00", "alpha")

	state := func(ts string) CloseState {
		t.Helper()
		f.clk.Set(at(ts))
		st, err := f.svc.CloseState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	if st := state("2026-11-16 16:44"); st.Due || st.Blink || !st.WorkingDay || st.Saved {
		t.Errorf("before the reminder: %+v", st)
	}
	if st := state("2026-11-16 16:45"); st.Due {
		t.Errorf("exactly at the reminder is not yet after it: %+v", st)
	}
	st := state("2026-11-16 16:46")
	if !st.Due {
		t.Fatalf("after the reminder: %+v", st)
	}
	// Blinks: the phase alternates every second.
	var phases []bool
	for i := 0; i < 4; i++ {
		f.clk.Advance(time.Second)
		s, _ := f.svc.CloseState(ctx)
		phases = append(phases, s.Blink)
	}
	if phases[0] == phases[1] || phases[0] != phases[2] || phases[1] != phases[3] {
		t.Errorf("blink does not alternate: %v", phases)
	}
	// Saving the close stops it.
	if _, err := f.svc.Import(ctx, "```stkr-close v1\nitem | note | alpha | done for today |\n```", ViaAPI); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		f.clk.Advance(time.Second)
		if s, _ := f.svc.CloseState(ctx); s.Due || s.Blink || !s.Saved {
			t.Errorf("after the close was saved: %+v", s)
		}
	}
	// A brief alone does not count as a close.
	g := newDailyFixture(t)
	g.addCtx(t, "alpha", domain.KindProject)
	g.switchOK(t, "2026-11-16 09:00", "alpha")
	if _, err := g.svc.Import(ctx, "```stkr-brief v1\ntop | 1 | x\n```", ViaAPI); err != nil {
		t.Fatal(err)
	}
	g.clk.Set(at("2026-11-16 17:30"))
	if s, _ := g.svc.CloseState(ctx); !s.Due {
		t.Errorf("brief must not satisfy the close: %+v", s)
	}
	// No tracked time (not a working day): never due. Also weekends.
	h := newDailyFixture(t)
	h.clk.Set(at("2026-11-16 18:00"))
	if s, _ := h.svc.CloseState(ctx); s.Due || s.WorkingDay {
		t.Errorf("no events: %+v", s)
	}
	h.switchOK(t, "2026-11-21 10:00", "planning") // Saturday
	h.clk.Set(at("2026-11-21 18:00"))
	if s, _ := h.svc.CloseState(ctx); s.Due || s.WorkingDay {
		t.Errorf("weekend: %+v", s)
	}
	// The next day starts clean even though yesterday was never closed.
	f2 := newDailyFixture(t)
	f2.addCtx(t, "alpha", domain.KindProject)
	f2.workDay(t, "2026-11-16", "alpha")
	f2.clk.Set(at("2026-11-17 08:00"))
	if s, _ := f2.svc.CloseState(ctx); s.Due || s.WorkingDay {
		t.Errorf("next morning: %+v", s)
	}
	f2.clk.Set(at("2026-11-17 09:30"))
	res, err := f2.svc.StartDay(ctx, StartDayOptions{})
	if err != nil || !strings.Contains(res.Prompt, "- 2026-11-16: close was not saved") {
		t.Fatalf("a missing close must show in the next digest: %v\n%s", err, res.Prompt)
	}
}

func TestReminderTickLogsOncePerDay(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	f.addCtx(t, "alpha", domain.KindProject)
	f.switchOK(t, "2026-11-16 09:00", "alpha")
	count := func() int { return strings.Count(f.loggedTo.String(), "close reminder due") }
	f.clk.Set(at("2026-11-16 16:00"))
	if err := f.svc.ReminderTick(ctx); err != nil || count() != 0 {
		t.Fatalf("before: %v %d", err, count())
	}
	f.clk.Set(at("2026-11-16 17:00"))
	for i := 0; i < 3; i++ {
		if err := f.svc.ReminderTick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if count() != 1 {
		t.Errorf("logged %d times", count())
	}
}

func TestHighPrecisionSharesIgnoreZero(t *testing.T) {
	r := domain.DayReport{Totals: []domain.ContextTotal{
		{Label: "A", Exportable: true, Counted: domain.Duration(time.Hour)},
		{Label: "B", Exportable: true, Counted: 0},
		{Label: "C", Exportable: false, Counted: domain.Duration(time.Hour)},
	}}
	shares, total := sharesOf(r)
	if len(shares) != 1 || total != time.Hour {
		t.Errorf("shares = %+v total = %v", shares, total)
	}
}

// switchOK switches at ts and confirms the new block, so long blocks count
// (otherwise a block longer than idle_gap_minutes is idle-suspect).
func (f *dailyFix) switchOK(t *testing.T, ts, ctxID string) {
	t.Helper()
	res := f.switchAt(t, ts, ctxID)
	if _, err := f.svc.ConfirmEvent(context.Background(), res.Event.ID, true); err != nil {
		t.Fatal(err)
	}
}
