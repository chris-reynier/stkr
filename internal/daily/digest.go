package daily

import (
	"fmt"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// MaxDigestChars is the largest digest, in characters (runes).
const MaxDigestChars = 2500

const (
	maxLineRunes = 160
	topContexts  = 6
)

// TimeShare is counted time on one context.
type TimeShare struct {
	Label string
	Time  time.Duration
}

// GapSpan is an uncovered stretch of working hours.
type GapSpan struct {
	Start, End string // HH:MM
	Duration   time.Duration
}

// DayDigest is one working day in the digest.
type DayDigest struct {
	Date    string // YYYY-MM-DD
	Weekday string
	// Shares are exportable contexts, largest first. Only the first six are shown.
	Shares      []TimeShare
	Total       time.Duration
	Gaps        []GapSpan
	IdleSuspect time.Duration
}

// ItemLine is an item as shown to the AI.
type ItemLine struct {
	Fingerprint string
	Type        domain.ItemType
	Project     string
	Summary     string
	Due         string
	Done        bool
}

// AgentLine is an agent instance needing attention.
type AgentLine struct {
	Label   string
	State   string
	Summary string
}

// DigestInput is everything the Start Day digest is built from.
type DigestInput struct {
	// Days are the last five working days before today, newest first.
	Days []DayDigest
	// YesterdayDate and YesterdayTop are the top three of the previous working day.
	YesterdayDate string
	YesterdayTop  [3]string
	OpenItems     []ItemLine
	// IdeaCount is the number of untriaged professional ideas; IdeaTitles the
	// first few.
	IdeaCount  int
	IdeaTitles []string
	Agents     []AgentLine
	// UnsavedClose lists previous working days that were never closed.
	UnsavedClose []string
}

// section is a titled list of lines. Lower priority sections lose lines first.
type section struct {
	title    string
	lines    []string
	priority int
	dropped  int
}

func (s section) render() string {
	var b strings.Builder
	b.WriteString("## " + s.title + "\n")
	for _, l := range s.lines {
		b.WriteString(l + "\n")
	}
	if s.dropped > 0 {
		fmt.Fprintf(&b, "- ... %d more not shown\n", s.dropped)
	}
	return b.String()
}

func renderSections(secs []section) string {
	parts := make([]string, 0, len(secs))
	for _, s := range secs {
		if len(s.lines) == 0 && s.dropped == 0 {
			continue
		}
		parts = append(parts, s.render())
	}
	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}

// fit removes lines from the lowest-priority sections until the rendering has
// at most max characters, then returns it.
func fit(secs []section, max int) string {
	for {
		out := renderSections(secs)
		if len([]rune(out)) <= max {
			return out
		}
		var v *section
		for i := range secs {
			if len(secs[i].lines) > 0 && (v == nil || secs[i].priority < v.priority) {
				v = &secs[i]
			}
		}
		if v == nil {
			return string([]rune(out)[:max])
		}
		v.lines = v.lines[:len(v.lines)-1]
		v.dropped++
	}
}

func clip(s string) string { return truncate(s, maxLineRunes) }

func short(d time.Duration) string { return domain.FormatDuration(d) }

func (l ItemLine) text() string {
	s := fmt.Sprintf("- %s | %s | %s", l.Type, truncate(l.Project, 30), truncate(l.Summary, 100))
	if l.Due != "" {
		s += " (due " + l.Due + ")"
	}
	if l.Done {
		s += " [done]"
	}
	if l.Fingerprint != "" {
		s += " #" + l.Fingerprint
	}
	return s
}

func itemLines(items []ItemLine) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.text())
	}
	return out
}

func agentLines(agents []AgentLine) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		s := "- " + a.Label + ": " + a.State
		if a.Summary != "" {
			s += " - " + a.Summary
		}
		out = append(out, clip(s))
	}
	return out
}

func topLines(top [3]string) []string {
	var out []string
	for i, t := range top {
		if t != "" {
			out = append(out, fmt.Sprintf("%d. %s", i+1, clip(t)))
		}
	}
	return out
}

func timeLine(d DayDigest) string {
	s := fmt.Sprintf("- %s %s: %s total", d.Weekday[:min(3, len(d.Weekday))], d.Date, short(d.Total))
	n := 0
	for _, sh := range d.Shares {
		if n == topContexts {
			break
		}
		s += fmt.Sprintf("; %s %s", sh.Label, short(sh.Time))
		n++
	}
	return truncate(s, 220)
}

// BuildDigest renders the Start Day digest. It never exceeds MaxDigestChars
// characters: when the content is too long, lines are dropped from the least
// important section first (time per day, then ideas, gaps, agents, open items,
// yesterday's top three, unsaved closes).
func BuildDigest(in DigestInput) string {
	secs := []section{
		{title: "Previous working days (time per context, top 6)", priority: 1},
		{title: "Yesterday's top 3 (" + in.YesterdayDate + ")", lines: topLines(in.YesterdayTop), priority: 6},
		{title: fmt.Sprintf("Open items (%d)", len(in.OpenItems)), lines: itemLines(in.OpenItems), priority: 5},
		{title: fmt.Sprintf("Untriaged ideas (%d)", in.IdeaCount), priority: 2},
		{title: "Agents needing attention", lines: agentLines(in.Agents), priority: 4},
		{title: "Timesheet gaps (not counted)", priority: 3},
		{title: "Previous days not closed", priority: 7},
	}
	for _, d := range in.Days {
		secs[0].lines = append(secs[0].lines, timeLine(d))
		var parts []string
		for _, g := range d.Gaps {
			parts = append(parts, fmt.Sprintf("%s-%s", g.Start, g.End))
		}
		if len(parts) > 0 || d.IdleSuspect > 0 {
			s := fmt.Sprintf("- %s:", d.Date)
			if len(parts) > 0 {
				s += " gap " + strings.Join(parts, ", ")
			}
			if d.IdleSuspect > 0 {
				s += fmt.Sprintf(" unconfirmed idle %s", short(d.IdleSuspect))
			}
			secs[5].lines = append(secs[5].lines, clip(s))
		}
	}
	if in.IdeaCount > 0 {
		for _, t := range in.IdeaTitles {
			secs[3].lines = append(secs[3].lines, clip("- "+t))
		}
	}
	for _, d := range in.UnsavedClose {
		secs[6].lines = append(secs[6].lines, "- "+d+": close was not saved")
	}
	if in.IdeaCount == 0 {
		secs[3].title = "Untriaged ideas (0)"
	}
	out := fit(secs, MaxDigestChars)
	if out == "" {
		return "Nothing recorded yet."
	}
	return out
}

// CloseInput is everything the Close Day prompt context is built from.
type CloseInput struct {
	Date    string
	Shares  []TimeShare
	Total   time.Duration
	Gaps    []GapSpan
	TopsAM  [3]string
	Today   []ItemLine
	Open    []ItemLine
	Ideas   []string
	Agents  []AgentLine
	IdeaCnt int
}

// BuildCloseContext renders the facts of today for the Close Day prompt, in
// at most MaxDigestChars characters.
func BuildCloseContext(in CloseInput) string {
	timeLines := make([]string, 0, len(in.Shares)+1)
	timeLines = append(timeLines, fmt.Sprintf("- total %s", short(in.Total)))
	for _, sh := range in.Shares {
		timeLines = append(timeLines, fmt.Sprintf("- %s %s", sh.Label, short(sh.Time)))
	}
	var gapLines []string
	for _, g := range in.Gaps {
		gapLines = append(gapLines, fmt.Sprintf("- %s-%s (%s)", g.Start, g.End, short(g.Duration)))
	}
	secs := []section{
		{title: "Time today (" + in.Date + ")", lines: timeLines, priority: 3},
		{title: "Morning top 3", lines: topLines(in.TopsAM), priority: 6},
		{title: fmt.Sprintf("Items captured today (%d)", len(in.Today)), lines: itemLines(in.Today), priority: 5},
		{title: fmt.Sprintf("Other open items (%d)", len(in.Open)), lines: itemLines(in.Open), priority: 4},
		{title: fmt.Sprintf("Ideas captured today (%d)", in.IdeaCnt), priority: 1},
		{title: "Agents needing attention", lines: agentLines(in.Agents), priority: 2},
		{title: "Timesheet gaps today (not counted)", lines: gapLines, priority: 1},
	}
	for _, t := range in.Ideas {
		secs[4].lines = append(secs[4].lines, clip("- "+t))
	}
	return fit(secs, MaxDigestChars)
}
