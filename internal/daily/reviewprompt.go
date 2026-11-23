package daily

import (
	"fmt"
	"strings"
	"time"
)

// MaxReviewChars is the largest review context, in characters (runes).
const MaxReviewChars = 6000

// ReviewDayTemplate is the prompt of the daily review.
const ReviewDayTemplate = "review-day.md.tmpl"

// ReviewDayData is what the review-day template can use.
type ReviewDayData struct {
	Date    string // YYYY-MM-DD
	Weekday string
	// Basis tells how the date was chosen: requested, yesterday,
	// previous_working_day or last_weekday.
	Basis   string
	Context string
}

// BlockLine is a tracked block in the review context.
type BlockLine struct {
	Start, End string // HH:MM
	Label      string
	Duration   time.Duration
	// Flags such as "idle-suspect, not counted" or "private".
	Flags string
}

// PeriodLine is an idle, locked or break stretch.
type PeriodLine struct {
	Start, End string // HH:MM
	State      string
	Duration   time.Duration
}

// ReviewInput is everything the review context is built from. Private data
// must already be removed by the caller.
type ReviewInput struct {
	Date       string
	WorkingDay bool
	Counted    time.Duration
	GapTotal   time.Duration
	Idle       time.Duration
	Shares     []TimeShare
	Blocks     []BlockLine
	Gaps       []GapSpan
	Top        [3]string
	DayItems   []ItemLine
	Open       []ItemLine
	Ideas      []string
	Agents     []AgentLine
	// Activity evidence; HasActivity is false when no client sent any.
	HasActivity bool
	ActiveTime  time.Duration
	PrivateTime time.Duration
	Categories  []TimeShare
	Workspaces  []TimeShare
	Periods     []PeriodLine
	Contexts    []string // "id (label)"
	Missing     []string
	CloseSaved  bool
}

func shareLines(in []TimeShare) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, clip(fmt.Sprintf("- %s %s", s.Label, short(s.Time))))
	}
	return out
}

// BuildReviewContext renders the facts of one day for the review prompt, in
// at most MaxReviewChars characters. Lower-value sections lose lines first.
func BuildReviewContext(in ReviewInput) string {
	summary := []string{
		fmt.Sprintf("- working day: %v", in.WorkingDay),
		fmt.Sprintf("- counted %s, untracked gaps %s, unconfirmed idle %s", short(in.Counted), short(in.GapTotal), short(in.Idle)),
		fmt.Sprintf("- close of day saved: %v", in.CloseSaved),
	}
	var blocks []string
	for _, b := range in.Blocks {
		s := fmt.Sprintf("- %s-%s %s (%s)", b.Start, b.End, b.Label, short(b.Duration))
		if b.Flags != "" {
			s += " [" + b.Flags + "]"
		}
		blocks = append(blocks, clip(s))
	}
	var gaps []string
	for _, g := range in.Gaps {
		gaps = append(gaps, fmt.Sprintf("- %s-%s (%s)", g.Start, g.End, short(g.Duration)))
	}
	var act []string
	if in.HasActivity {
		act = append(act, fmt.Sprintf("- active %s, private %s", short(in.ActiveTime), short(in.PrivateTime)))
		for _, c := range in.Categories {
			act = append(act, clip(fmt.Sprintf("- category %s %s", c.Label, short(c.Time))))
		}
		for _, w := range in.Workspaces {
			act = append(act, clip(fmt.Sprintf("- workspace %s %s", w.Label, short(w.Time))))
		}
	}
	var periods []string
	for _, p := range in.Periods {
		periods = append(periods, fmt.Sprintf("- %s-%s %s (%s)", p.Start, p.End, p.State, short(p.Duration)))
	}
	var missing []string
	for _, m := range in.Missing {
		missing = append(missing, "- "+m)
	}
	var ideas []string
	for _, t := range in.Ideas {
		ideas = append(ideas, clip("- "+t))
	}
	ctxs := []string{"- " + strings.Join(in.Contexts, ", ")}
	if len(in.Contexts) == 0 {
		ctxs = nil
	}
	secs := []section{
		{title: "Day " + in.Date, lines: summary, priority: 10},
		{title: "Time per context (counted)", lines: shareLines(in.Shares), priority: 9},
		{title: "Tracked blocks", lines: blocks, priority: 5},
		{title: "Untracked working time (gaps)", lines: gaps, priority: 7},
		{title: "Planned top 3", lines: topLines(in.Top), priority: 6},
		{title: fmt.Sprintf("Items recorded that day (%d)", len(in.DayItems)), lines: itemLines(in.DayItems), priority: 6},
		{title: fmt.Sprintf("Open items (%d)", len(in.Open)), lines: itemLines(in.Open), priority: 4},
		{title: "Desktop activity", lines: act, priority: 3},
		{title: "Locked, idle and break periods", lines: periods, priority: 2},
		{title: "Agents needing attention", lines: agentLines(in.Agents), priority: 2},
		{title: fmt.Sprintf("New ideas (%d)", len(in.Ideas)), lines: ideas, priority: 1},
		{title: "Context ids", lines: ctxs, priority: 8},
		{title: "Missing evidence (STKR does not have it)", lines: missing, priority: 10},
	}
	return fit(secs, MaxReviewChars)
}
