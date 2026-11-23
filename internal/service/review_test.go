package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

const reviewSentinel = "PRIVATE-SENTINEL-7c1d"

func TestReviewDateFallback(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		setup  func(f *dailyFix)
		now    string
		date   string
		want   string
		basis  string
		prev   string
		errSub string
	}{
		{name: "nothing tracked: last weekday", now: "2026-11-16 09:00", want: "2026-11-13", basis: BasisLastWeekday, prev: "2026-11-13"},
		{name: "monday falls back over the weekend", now: "2026-11-16 09:00",
			setup: func(f *dailyFix) { f.workDay(t, "2026-11-13", "other") }, want: "2026-11-13", basis: BasisPreviousWorkingDay, prev: "2026-11-13"},
		{name: "tuesday uses yesterday", now: "2026-11-17 09:00",
			setup: func(f *dailyFix) { f.workDay(t, "2026-11-13", "other"); f.workDay(t, "2026-11-16", "other") },
			want:  "2026-11-16", basis: BasisYesterday, prev: "2026-11-16"},
		{name: "a day with only off is skipped", now: "2026-11-16 09:00",
			setup: func(f *dailyFix) {
				f.workDay(t, "2026-11-12", "other")
				f.switchAt(t, "2026-11-13 10:00", "other")
				f.switchAt(t, "2026-11-13 10:00", domain.ContextOff)
				// The only Friday event left is off: delete the "other" one.
				evs, _ := f.st.EventsBetween(ctx, at("2026-11-13 00:00"), at("2026-11-14 00:00"))
				for _, e := range evs {
					if e.ContextID != domain.ContextOff {
						_ = f.svc.DeleteEvent(ctx, e.ID)
					}
				}
			}, want: "2026-11-12", basis: BasisPreviousWorkingDay, prev: "2026-11-12"},
		{name: "explicit date", now: "2026-11-16 09:00", date: "2026-11-10", want: "2026-11-10", basis: BasisRequested, prev: "2026-11-13"},
		{name: "today is allowed", now: "2026-11-16 09:00", date: "2026-11-16", want: "2026-11-16", basis: BasisRequested, prev: "2026-11-13"},
		{name: "future date", now: "2026-11-16 09:00", date: "2026-11-17", errSub: "future"},
		{name: "impossible date", now: "2026-11-16 09:00", date: "2026-02-30", errSub: "invalid date"},
		{name: "words are not dates here", now: "2026-11-16 09:00", date: "yesterday", errSub: "invalid date"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDailyFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			f.clk.Set(at(tc.now))
			rc, err := f.svc.ReviewContext(ctx, tc.date)
			if tc.errSub != "" {
				if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tc.errSub) {
					t.Fatalf("want invalid %q, got %v", tc.errSub, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rc.Date != tc.want || rc.Basis != tc.basis || rc.PreviousWorkingDay != tc.prev {
				t.Fatalf("got date=%s basis=%s prev=%s", rc.Date, rc.Basis, rc.PreviousWorkingDay)
			}
		})
	}
}

// privateDay records Friday 2026-11-13 with a project, a break with a
// private note, ideas and items of both kinds.
func privateDay(t *testing.T, f *dailyFix) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme", Label: "Acme", Kind: domain.KindProject, Workdir: `C:\src\` + reviewSentinel + `\acme`}); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(at("2026-11-13 09:00"))
	if _, err := f.svc.Switch(ctx, "acme", domain.SourceDeck, "release prep"); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(at("2026-11-13 12:00"))
	if _, err := f.svc.Switch(ctx, "break", domain.SourceDeck, reviewSentinel+" doctor"); err != nil {
		t.Fatal(err)
	}
	f.switchOK(t, "2026-11-13 13:00", "acme")
	f.switchOK(t, "2026-11-13 17:00", domain.ContextOff)
	for _, in := range []IdeaInput{{Text: "pro idea for acme", Tag: "pro"}, {Text: reviewSentinel + " personal idea", Tag: "personal"}} {
		if _, err := f.svc.AddIdea(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	text := "```stkr-brief v1\nitem | action | acme | Ship the fix |\nitem | action | break | " + reviewSentinel + " dentist |\n```"
	if _, err := f.svc.Import(ctx, text, ViaCLI); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(at("2026-11-16 09:00"))
}

func TestReviewContextPrivacy(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	privateDay(t, f)
	rc, err := f.svc.ReviewContext(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Date != "2026-11-13" || rc.Timezone != "Europe/Paris" || !rc.WorkingDay || rc.Tracking.Counted.D() != 5*time.Hour || rc.Tracking.IdleSuspect.D() != 3*time.Hour {
		t.Fatalf("summary: %+v", rc)
	}
	raw, _ := json.Marshal(rc)
	if strings.Contains(string(raw), reviewSentinel) {
		t.Fatalf("private data leaked: %s", raw)
	}
	var sawBreak, sawNote bool
	for _, b := range rc.Blocks {
		if b.ContextID == "break" {
			sawBreak = b.Private && b.Note == ""
		}
		if b.ContextID == "acme" && b.Note == "release prep" {
			sawNote = true
		}
	}
	if !sawBreak || !sawNote {
		t.Fatalf("blocks: %+v", rc.Blocks)
	}
	if len(rc.NewIdeas) != 1 || rc.NewIdeas[0].Title == "" {
		t.Fatalf("ideas: %+v", rc.NewIdeas)
	}
	for _, it := range append(slices.Clone(rc.OpenItems), rc.DayItems...) {
		if it.Project == "break" {
			t.Fatalf("personal item returned: %+v", it)
		}
	}
	if len(rc.OpenItems) != 1 {
		t.Fatalf("open items: %+v", rc.OpenItems)
	}
	for _, c := range rc.Contexts {
		if c.Workdir != "" {
			t.Fatalf("workdir returned without opt-in: %+v", c)
		}
	}
	for _, m := range []string{MissingMeetingMinutes, MissingEmailCommitments, MissingLockHistory, MissingApplicationActivity} {
		if !slices.Contains(rc.Missing, m) {
			t.Fatalf("missing lacks %s: %v", m, rc.Missing)
		}
	}
	if !rc.Close.Due || rc.Close.Saved || rc.Close.SavedAt != nil {
		t.Fatalf("close: %+v", rc.Close)
	}
}

func TestReviewContextWorkspacePathsOptIn(t *testing.T) {
	f := newDailyFixture(t, func(c *config.Config) { c.Review.IncludeWorkspacePaths = true })
	privateDay(t, f)
	rc, err := f.svc.ReviewContext(context.Background(), "2026-11-13")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range rc.Contexts {
		found = found || (c.ID == "acme" && strings.Contains(c.Workdir, "acme"))
	}
	if !found {
		t.Fatalf("workdir not returned with opt-in: %+v", rc.Contexts)
	}
}

func act(ts, app, cat, ws, ctxID, lock string, secs int64) ActivityInput {
	return ActivityInput{TS: ts, App: app, Category: cat, Workspace: ws, ContextID: ctxID, DurationSeconds: secs, LockState: lock}
}

func TestAddActivityValidation(t *testing.T) {
	ok := act("2026-11-13T09:10:00+01:00", "Code", "development", "stream-deck", "", "active", 320)
	tests := []struct {
		name string
		ev   ActivityInput
		sub  string
	}{
		{"timestamp without offset", act("2026-11-13T09:10:00", "Code", "development", "", "", "active", 1), "ts must be"},
		{"missing app", act(ok.TS, " ", "development", "", "", "active", 1), "app is required"},
		{"app is a path", act(ok.TS, `C:\Program Files\x.exe`, "development", "", "", "active", 1), "not a path"},
		{"app is a url", act(ok.TS, "https://mail.example", "browser", "", "", "active", 1), "not a path"},
		{"app too long", act(ok.TS, strings.Repeat("a", 65), "development", "", "", "active", 1), "at most 64"},
		{"control characters", act(ok.TS, "Co\x00de", "development", "", "", "active", 1), "printable"},
		{"bad category", act(ok.TS, "Code", "gaming", "", "", "active", 1), "category must be"},
		{"workspace is a path", act(ok.TS, "Code", "development", "/home/me/src", "", "active", 1), "not a path"},
		{"zero duration", act(ok.TS, "Code", "development", "", "", "active", 0), "duration_seconds"},
		{"over a day", act(ok.TS, "Code", "development", "", "", "active", 86401), "duration_seconds"},
		{"bad lock state", act(ok.TS, "Code", "development", "", "", "asleep", 1), "lock_state"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newDailyFixture(t)
			_, err := f.svc.AddActivity(context.Background(), []ActivityInput{ok, tc.ev})
			if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tc.sub) || !strings.Contains(err.Error(), "event 2") {
				t.Fatalf("got %v", err)
			}
			// Nothing of the batch is stored.
			if sum, _ := f.svc.ActivityDay(context.Background(), "2026-11-13"); sum.Events != 0 {
				t.Fatalf("partial batch stored: %d", sum.Events)
			}
			if strings.Contains(err.Error(), "Program Files") || strings.Contains(err.Error(), "mail.example") {
				t.Fatalf("error repeats the value: %v", err)
			}
		})
	}
	f := newDailyFixture(t)
	if _, err := f.svc.AddActivity(context.Background(), make([]ActivityInput, MaxActivityBatch+1)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized batch: %v", err)
	}
	off := newDailyFixture(t, func(c *config.Config) { c.Activity.Enabled = false })
	if _, err := off.svc.AddActivity(context.Background(), []ActivityInput{ok}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("disabled: %v", err)
	}
}

func TestAddActivityMergeRangeAndSummary(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "deck", Label: "Deck", Kind: domain.KindProject, Workdir: `C:\src\stream-deck`}); err != nil {
		t.Fatal(err)
	}
	day := []ActivityInput{
		act("2026-11-13T09:00:00+01:00", "Code", "development", "stream-deck", "", "active", 600),
		act("2026-11-13T09:10:00+01:00", "Teams", "meeting", "", "meeting", "active", 1800),
		act("2026-11-13T09:40:00+01:00", "LockApp", "lock", "", "", "locked", 300),
		act("2026-11-13T09:45:30+01:00", "LockApp", "lock", "", "", "locked", 300),         // joins the previous period
		act("2026-11-13T10:00:00+01:00", "Browser", "browser", "", "break", "active", 900), // private context
		act("2026-11-13T12:00:00+01:00", "Idle", "other", "", "", "idle", 1200),
		act("2026-11-13T13:00:00+01:00", "Code", "development", "stream-deck", "ghost", "active", 60), // unknown context
		act("2026-10-01T09:00:00+01:00", "Code", "development", "", "", "active", 60),                 // past retention? no: 43 days
		act("2026-06-01T09:00:00+01:00", "Code", "development", "", "", "active", 60),                 // older than 90 days
		act("2026-11-16T09:30:00+01:00", "Code", "development", "", "", "active", 60),                 // 30 min in the future
	}
	// Late, out-of-order arrival: send the day reversed.
	rev := slices.Clone(day)
	slices.Reverse(rev)
	res, err := f.svc.AddActivity(ctx, rev)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 8 || res.OutOfRange != 2 || res.Unmapped != 1 || len(res.Warnings) != 2 {
		t.Fatalf("first: %+v", res)
	}
	// A replay changes nothing; a grown event merges.
	day[0].DurationSeconds = 900
	res, err = f.svc.AddActivity(ctx, day)
	if err != nil || res.Inserted != 0 || res.Merged != 1 || res.Unchanged != 7 {
		t.Fatalf("replay: %+v %v", res, err)
	}
	sum, err := f.svc.ActivityDay(ctx, "2026-11-13")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Events != 7 || sum.Active.D() != 900*time.Second+1800*time.Second+60*time.Second || sum.Private.D() != 15*time.Minute ||
		sum.Locked.D() != 10*time.Minute || sum.Idle.D() != 20*time.Minute {
		t.Fatalf("summary totals: %+v", sum)
	}
	if len(sum.Periods) != 2 || sum.Periods[0].State != domain.LockLocked || sum.Periods[0].Duration.D() != 10*time.Minute+30*time.Second {
		t.Fatalf("periods: %+v", sum.Periods)
	}
	if len(sum.Workspaces) != 1 || sum.Workspaces[0].Name != "stream-deck" || sum.Workspaces[0].ContextID != "deck" {
		t.Fatalf("workspaces (mapped by work folder): %+v", sum.Workspaces)
	}
	for _, a := range sum.Apps {
		if a.Name == "Browser" {
			t.Fatal("private activity shows its application")
		}
	}
	if sum.Categories[0].Name != "meeting" {
		t.Fatalf("categories: %+v", sum.Categories)
	}
	// Retention removes old events on the next write.
	f.clk.Set(at("2027-03-01 09:00"))
	res, err = f.svc.AddActivity(ctx, nil)
	if err != nil || res.Pruned != 8 {
		t.Fatalf("prune: %+v %v", res, err)
	}
	// The review context reflects the evidence.
	f.clk.Set(at("2026-11-16 09:00"))
	if _, err := f.svc.AddActivity(ctx, day[:3]); err != nil {
		t.Fatal(err)
	}
	rc, err := f.svc.ReviewContext(ctx, "2026-11-13")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Activity.Events != 3 || slices.Contains(rc.Missing, MissingLockHistory) || slices.Contains(rc.Missing, MissingApplicationActivity) {
		t.Fatalf("review activity: %+v missing=%v", rc.Activity, rc.Missing)
	}
	p, err := f.svc.ReviewPrompt(ctx, "2026-11-13", false)
	if err != nil || !strings.Contains(p.Prompt, "category meeting") || !strings.Contains(p.Prompt, "stream-deck -> deck") {
		t.Fatalf("prompt activity: %v\n%s", err, p.Prompt)
	}
	if strings.Contains(f.logs.String(), "Teams") || strings.Contains(f.logs.String(), "stream-deck") {
		t.Fatal("activity content logged")
	}
}

const reviewBlock = "Here is the review.\n\n```stkr-review v1\ndate|type|context|text\n" +
	"2026-11-13|achievement|acme|Completed API authentication changes\n" +
	"2026-11-13|action|Acme|Prepare deployment rollback plan\n" +
	"2026-11-13|risk|acme|Waiting for client security approval\n" +
	"2026-11-13|decision|meeting|Move the weekly review to Tuesday\n" +
	"2026-11-13|gap|administration|Confirm missing Friday meeting follow-up\n" +
	"2026-11-13|action|acme|Prepare deployment rollback plan.\n" +
	"2026-11-13|followup|break|" + reviewSentinel + " call the dentist\n" +
	"2026-11-13|praise|acme|Nice work\n" +
	"```\n"

func TestImportReview(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme", Label: "Acme", Kind: domain.KindProject}); err != nil {
		t.Fatal(err)
	}
	bad := "```stkr-review v1\n2026-11-13|note|acme|good line\n2026-11-13|note|acme\n```\n" +
		"```stkr-review v1\n2026-11-20|note|acme|next week\n```\n"
	res, err := f.svc.Import(ctx, reviewBlock+bad, ViaAPI)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "review" || res.ItemsCreated != 6 || res.Duplicates != 1 || res.Private != 1 || res.ReviewBlocks != 1 ||
		res.BlocksRejected != 2 || res.Ignored != 4 || res.State != ImportWarn {
		t.Fatalf("result: %+v", res)
	}
	joined := strings.Join(res.Warnings, "\n")
	for _, sub := range []string{"unknown review record type", "in the future", "unknown context", "never exported", "review block rejected"} {
		if !strings.Contains(joined, sub) {
			t.Fatalf("warnings lack %q:\n%s", sub, joined)
		}
	}
	if strings.Contains(joined, reviewSentinel) || strings.Contains(joined, "good line") {
		t.Fatal("warnings repeat record text")
	}
	items, _ := f.svc.ItemsOn(ctx, "2026-11-13")
	if len(items) != 6 {
		t.Fatalf("items on the record date: %+v", items)
	}
	for _, it := range items {
		if it.Origin != OriginReview || it.Source != ViaAPI {
			t.Fatalf("origin/source: %+v", it)
		}
		if (it.Type == domain.ItemAchievement) != (it.Status == "done") {
			t.Fatalf("achievement must be done, others open: %+v", it)
		}
		if strings.Contains(it.Summary, "good line") || strings.Contains(it.Summary, "next week") {
			t.Fatal("a rejected block created a record")
		}
	}
	// The private record is stored, never exported.
	if strings.Contains(f.read(t, "acme/log.md")+f.read(t, "_daily/inbox.md"), reviewSentinel) {
		t.Fatal("private record exported")
	}
	for _, e := range res.Exported {
		if strings.Contains(e, "break") {
			t.Fatalf("exported %s", e)
		}
	}
	// Day flags are not touched by a review.
	if d, _ := f.svc.DayPlan(ctx, "today"); d.BriefSaved || d.CloseSaved {
		t.Fatalf("day flags: %+v", d)
	}
	// Importing again creates nothing.
	res, err = f.svc.Import(ctx, reviewBlock, ViaClipboard)
	if err != nil || res.ItemsCreated != 0 || res.ItemsExisting != 6 {
		t.Fatalf("re-import: %+v %v", res, err)
	}
	// Only rejected blocks: nothing written, 422 path.
	_, err = f.svc.Import(ctx, bad, ViaAPI)
	var nb *NoValidBlockError
	if !errors.As(err, &nb) || nb.Result.BlocksRejected != 2 || nb.Result.Redirect == "" {
		t.Fatalf("only bad blocks: %v", err)
	}
}

func TestImportCloseStampsSavedAtAndMixedKind(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	f.clk.Set(at("2026-11-16 17:30"))
	text := "```stkr-close v1\nitem | note | other | n |\n```\n```stkr-review v1\n2026-11-16|note|other|r\n```"
	res, err := f.svc.Import(ctx, text, ViaAPI)
	if err != nil || res.Kind != "close+review" || res.ItemsCreated != 2 {
		t.Fatalf("mixed: %+v %v", res, err)
	}
	f.clk.Set(at("2026-11-16 18:00"))
	if _, err := f.svc.Import(ctx, text, ViaAPI); err != nil {
		t.Fatal(err)
	}
	rc, err := f.svc.ReviewContext(ctx, "2026-11-16")
	if err != nil || rc.Close.SavedAt == nil || !rc.Close.SavedAt.Equal(at("2026-11-16 17:30")) || !rc.Close.Saved {
		t.Fatalf("saved_at keeps the first close: %+v %v", rc.Close, err)
	}
}

func TestPreviewImport(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme", Label: "Acme", Kind: domain.KindProject}); err != nil {
		t.Fatal(err)
	}
	first := "```stkr-review v1\n2026-11-13|achievement|acme|Completed API authentication changes\n```"
	if _, err := f.svc.Import(ctx, first, ViaAPI); err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.PreviewImport(ctx, reviewBlock)
	if err != nil || !p.Valid || p.Kind != "review" {
		t.Fatalf("preview: %+v %v", p, err)
	}
	status := map[string]int{}
	for _, r := range p.Records {
		status[r.Status]++
		if r.Context == "break" && !r.Private {
			t.Fatal("private flag")
		}
		if r.Context == "administration" && r.KnownContext {
			t.Fatal("known flag")
		}
	}
	if status["existing"] != 1 || status["duplicate"] != 1 || status["new"] != 5 {
		t.Fatalf("statuses: %v", status)
	}
	if items, _ := f.svc.ItemsOn(ctx, "2026-11-13"); len(items) != 1 {
		t.Fatalf("preview wrote: %d items", len(items))
	}
	if st, _ := f.svc.ImportKeyState(ctx); st != ImportOK {
		t.Fatalf("preview changed the key state: %q", st)
	}
	if _, err := f.svc.PreviewImport(ctx, strings.Repeat("x", maxImportBytes+1)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("too large: %v", err)
	}
}

func TestReviewPrompt(t *testing.T) {
	ctx := context.Background()
	f := newDailyFixture(t)
	privateDay(t, f)
	res, err := f.svc.ReviewPrompt(ctx, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Date != "2026-11-13" || res.Basis != BasisPreviousWorkingDay || !res.Clipboard || f.clip.Text() != res.Prompt {
		t.Fatalf("result: %+v", res)
	}
	for _, sub := range []string{"Friday 2026-11-13", "```stkr-review v1", "Do not invent", "meeting_minutes", "acme", "personal", "release prep", "(assumption)"} {
		if !strings.Contains(res.Prompt, sub) {
			t.Fatalf("prompt lacks %q:\n%s", sub, res.Prompt)
		}
	}
	if strings.Contains(res.Prompt, reviewSentinel) {
		t.Fatal("private data in the prompt")
	}
	if strings.Contains(f.logs.String(), "release prep") {
		t.Fatal("prompt logged")
	}
	// The prompt left on the clipboard imports nothing.
	if _, err := f.svc.Import(ctx, res.Prompt, ViaClipboard); err == nil {
		t.Fatal("the prompt itself must not import")
	}
}
