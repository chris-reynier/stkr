package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

// Review date bases: how ReviewContext chose its date.
const (
	BasisRequested          = "requested"
	BasisYesterday          = "yesterday"
	BasisPreviousWorkingDay = "previous_working_day"
	BasisLastWeekday        = "last_weekday"
)

// Missing evidence categories reported by ReviewContext.
const (
	MissingMeetingMinutes      = "meeting_minutes"
	MissingEmailCommitments    = "email_commitments"
	MissingLockHistory         = "lock_history"
	MissingApplicationActivity = "application_activity"
	MissingTimeTracking        = "time_tracking"
)

const (
	maxReviewOpen  = 50
	maxReviewIdeas = 20
	maxReviewText  = 300
)

// ReviewTracking is the time summary of the review day.
type ReviewTracking struct {
	Counted     domain.Duration `json:"counted_seconds"`
	Gap         domain.Duration `json:"gap_seconds"`
	IdleSuspect domain.Duration `json:"idle_suspect_seconds"`
}

// ReviewBlock is a tracked block. Private blocks (personal and system
// contexts) never carry a note.
type ReviewBlock struct {
	Start       time.Time          `json:"start"`
	End         time.Time          `json:"end"`
	ContextID   string             `json:"context_id"`
	Label       string             `json:"label"`
	Kind        domain.ContextKind `json:"kind"`
	Duration    domain.Duration    `json:"duration_seconds"`
	Counted     bool               `json:"counted"`
	IdleSuspect bool               `json:"idle_suspect"`
	Confirmed   bool               `json:"confirmed"`
	Private     bool               `json:"private"`
	Note        string             `json:"note,omitempty"`
}

// ReviewContextRef is a context the review answer may name.
type ReviewContextRef struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Kind        domain.ContextKind `json:"kind"`
	TempoTicket string             `json:"tempo_ticket,omitempty"`
	Private     bool               `json:"private"`
	// Workdir is only filled when review.include_workspace_paths is on.
	Workdir string `json:"workdir,omitempty"`
}

// ReviewIdea is an untriaged professional idea.
type ReviewIdea struct {
	ID    int64     `json:"id"`
	TS    time.Time `json:"ts,omitzero"`
	Title string    `json:"title"`
	Text  string    `json:"text,omitempty"`
}

// ReviewAgent is a coding agent instance.
type ReviewAgent struct {
	Label     string `json:"label"`
	State     string `json:"state"`
	ContextID string `json:"context_id,omitempty"`
	Summary   string `json:"summary,omitempty"`
	// Workspace is only filled when review.include_workspace_paths is on.
	Workspace string `json:"workspace,omitempty"`
}

// ReviewClose is the close-of-day state of the review day.
type ReviewClose struct {
	Due     bool       `json:"due"`
	Saved   bool       `json:"saved"`
	SavedAt *time.Time `json:"saved_at"`
}

// ReviewContext is the privacy-bounded metadata of one working day for an
// external review (contract review-context.md v1).
type ReviewContext struct {
	Date               string                `json:"date"`
	PreviousWorkingDay string                `json:"previous_working_day"`
	Basis              string                `json:"basis"`
	Timezone           string                `json:"timezone"`
	Weekday            string                `json:"weekday"`
	WorkingDay         bool                  `json:"working_day"`
	Tracking           ReviewTracking        `json:"tracking"`
	Blocks             []ReviewBlock         `json:"blocks"`
	Gaps               []domain.Gap          `json:"gaps"`
	Totals             []domain.ContextTotal `json:"totals"`
	Contexts           []ReviewContextRef    `json:"contexts"`
	Top                []string              `json:"top"`
	DayItems           []domain.Item         `json:"day_items"`
	OpenItems          []domain.Item         `json:"open_items"`
	NewIdeas           []ReviewIdea          `json:"new_ideas"`
	Agents             []ReviewAgent         `json:"agents"`
	Activity           ActivitySummary       `json:"activity"`
	Close              ReviewClose           `json:"close"`
	Missing            []string              `json:"missing"`
}

// previousWorkingDay walks back from the day before today, at most
// historyDays, to the first working day (weekday with a non-off event). It
// returns the day and the basis: yesterday, previous_working_day or, when
// none was found, the last weekday before today (last_weekday).
func (s *Service) previousWorkingDay(ctx context.Context, today time.Time) (time.Time, string, error) {
	first := today.AddDate(0, 0, -historyDays)
	events, err := s.st.EventsBetween(ctx, first, today)
	if err != nil {
		return time.Time{}, "", err
	}
	for i := 1; i <= historyDays; i++ {
		y, m, d := today.Date()
		day := time.Date(y, m, d-i, 0, 0, 0, 0, s.loc)
		if timeline.WorkingDay(events, day, s.tl) {
			if i == 1 {
				return day, BasisYesterday, nil
			}
			return day, BasisPreviousWorkingDay, nil
		}
	}
	for i := 1; ; i++ {
		y, m, d := today.Date()
		day := time.Date(y, m, d-i, 0, 0, 0, 0, s.loc)
		if timeline.IsWeekday(day, s.loc) {
			return day, BasisLastWeekday, nil
		}
	}
}

// ResolveReviewDate returns the day a review is about and how it was chosen.
// date is "" (previous working day) or YYYY-MM-DD, not after today.
func (s *Service) ResolveReviewDate(ctx context.Context, date string) (day, previous time.Time, basis string, err error) {
	today := timeline.Midnight(s.clk.Now(), s.loc)
	previous, basis, err = s.previousWorkingDay(ctx, today)
	if err != nil {
		return
	}
	date = strings.TrimSpace(date)
	if date == "" {
		return previous, previous, basis, nil
	}
	day, perr := time.ParseInLocation(dateLayout, date, s.loc)
	if perr != nil {
		return day, previous, "", invalid("invalid date %q (want YYYY-MM-DD)", date)
	}
	if day.After(today) {
		return day, previous, "", invalid("date %s is in the future", date)
	}
	return day, previous, BasisRequested, nil
}

func (s *Service) timezoneName() string {
	if s.cfg.Timezone != "" {
		return s.cfg.Timezone
	}
	name, off := s.clk.Now().In(s.loc).Zone()
	if s.loc.String() != "Local" {
		return s.loc.String()
	}
	if name != "" {
		return name
	}
	return fmt.Sprintf("UTC%+d", off/3600)
}

// ReviewContext returns the privacy-bounded review metadata of a day: date
// is "" (previous working day) or YYYY-MM-DD. Personal and system contexts
// keep their time but lose notes; their items, and personal ideas, are left
// out; work folders appear only with review.include_workspace_paths. It has
// no side effects.
func (s *Service) ReviewContext(ctx context.Context, date string) (ReviewContext, error) {
	day, prev, basis, err := s.ResolveReviewDate(ctx, date)
	if err != nil {
		return ReviewContext{}, err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return ReviewContext{}, err
	}
	reps, err := s.dayReports(ctx, day, 1)
	if err != nil {
		return ReviewContext{}, err
	}
	rep := reps[0]
	ds := day.Format(dateLayout)
	out := ReviewContext{
		Date: ds, PreviousWorkingDay: prev.Format(dateLayout), Basis: basis, Timezone: s.timezoneName(),
		Weekday: rep.Weekday, WorkingDay: rep.WorkingDay,
		Tracking: ReviewTracking{Counted: rep.Counted, Gap: rep.GapTotal, IdleSuspect: rep.IdleSuspect},
		Blocks:   []ReviewBlock{}, Gaps: rep.Gaps, Totals: rep.Totals, Contexts: []ReviewContextRef{}, Top: []string{},
		DayItems: []domain.Item{}, OpenItems: []domain.Item{}, NewIdeas: []ReviewIdea{}, Agents: []ReviewAgent{}, Missing: []string{},
	}
	if out.Gaps == nil {
		out.Gaps = []domain.Gap{}
	}
	for _, b := range rep.Blocks {
		private := !b.Kind.Exportable()
		rb := ReviewBlock{Start: b.Start, End: b.End, ContextID: b.ContextID, Label: b.Label, Kind: b.Kind, Duration: b.Duration,
			Counted: b.Counted, IdleSuspect: b.IdleSuspect, Confirmed: b.Confirmed, Private: private}
		if !private {
			rb.Note = b.Note
		}
		out.Blocks = append(out.Blocks, rb)
	}
	ids := make([]string, 0, len(ctxs))
	for id := range ctxs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := ctxs[ids[i]], ctxs[ids[j]]
		if a.Sort != b.Sort {
			return a.Sort < b.Sort
		}
		return a.ID < b.ID
	})
	for _, id := range ids {
		c := ctxs[id]
		if !c.Active {
			continue
		}
		ref := ReviewContextRef{ID: c.ID, Label: c.Label, Kind: c.Kind, TempoTicket: c.TempoTicket, Private: !c.Kind.Exportable()}
		if s.cfg.Review.IncludeWorkspacePaths {
			ref.Workdir = c.Workdir
		}
		out.Contexts = append(out.Contexts, ref)
	}
	plan, err := s.st.GetDay(ctx, ds)
	if err != nil {
		return out, err
	}
	for _, t := range plan.Top {
		if t != "" {
			out.Top = append(out.Top, t)
		}
	}
	out.Close = ReviewClose{Saved: plan.CloseSaved, Due: rep.WorkingDay && !plan.CloseSaved}
	if !plan.CloseSavedAt.IsZero() {
		t := plan.CloseSavedAt.In(s.loc)
		out.Close.SavedAt = &t
	}
	if day.Equal(timeline.Midnight(s.clk.Now(), s.loc)) {
		cs, err := s.closeStateFrom(ctx, rep)
		if err != nil {
			return out, err
		}
		out.Close.Due = cs.Due
	}
	dayItems, err := s.st.ListItems(ctx, ds, "")
	if err != nil {
		return out, err
	}
	for _, it := range dayItems {
		if visibleProject(ctxs, it.Project) {
			out.DayItems = append(out.DayItems, it)
		}
	}
	open, err := s.openItems(ctx, ctxs, "")
	if err != nil {
		return out, err
	}
	if len(open) > maxReviewOpen {
		open = open[:maxReviewOpen]
	}
	out.OpenItems = append(out.OpenItems, open...)
	ideas, err := s.st.ListIdeas(ctx, domain.IdeaNew)
	if err != nil {
		return out, err
	}
	for _, i := range ideas {
		if c, ok := ctxs[i.ContextID]; i.Tag != "pro" || (ok && !c.Kind.Exportable()) {
			continue
		}
		if len(out.NewIdeas) == maxReviewIdeas {
			break
		}
		text := i.CleanText
		if text == "" {
			text = i.RawText
		}
		out.NewIdeas = append(out.NewIdeas, ReviewIdea{ID: i.ID, TS: i.TS.In(s.loc), Title: ideaTitle(i), Text: truncateRunes(strings.Join(strings.Fields(text), " "), maxReviewText)})
	}
	agents, err := s.Agents(ctx)
	if err != nil {
		return out, err
	}
	for _, a := range agents.Instances {
		ra := ReviewAgent{Label: a.Label, State: string(a.State), ContextID: a.ContextID, Summary: a.Summary}
		if s.cfg.Review.IncludeWorkspacePaths {
			ra.Workspace = a.Workspace
		} else if looksLikePath(ra.Label) {
			ra.Label = a.ID
		}
		out.Agents = append(out.Agents, ra)
	}
	if out.Activity, err = s.activitySummary(ctx, day, ctxs); err != nil {
		return out, err
	}
	if len(rep.Events) == 0 {
		out.Missing = append(out.Missing, MissingTimeTracking)
	}
	out.Missing = append(out.Missing, MissingMeetingMinutes, MissingEmailCommitments)
	if out.Activity.Events == 0 {
		out.Missing = append(out.Missing, MissingLockHistory, MissingApplicationActivity)
	}
	return out, nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// reviewInput converts a review context for the prompt.
func (s *Service) reviewInput(rc ReviewContext) daily.ReviewInput {
	in := daily.ReviewInput{
		Date: rc.Date, WorkingDay: rc.WorkingDay, Counted: rc.Tracking.Counted.D(), GapTotal: rc.Tracking.Gap.D(),
		Idle: rc.Tracking.IdleSuspect.D(), CloseSaved: rc.Close.Saved, Missing: rc.Missing,
		HasActivity: rc.Activity.Events > 0, ActiveTime: rc.Activity.Active.D(), PrivateTime: rc.Activity.Private.D(),
	}
	for _, t := range rc.Totals {
		if t.Counted > 0 && t.Exportable {
			in.Shares = append(in.Shares, daily.TimeShare{Label: t.ContextID, Time: t.Counted.D()})
		}
	}
	for _, b := range rc.Blocks {
		bl := daily.BlockLine{Start: b.Start.In(s.loc).Format("15:04"), End: b.End.In(s.loc).Format("15:04"), Label: b.ContextID, Duration: b.Duration.D()}
		var flags []string
		if b.IdleSuspect && !b.Counted {
			flags = append(flags, "unconfirmed idle, not counted")
		}
		if b.Private {
			bl.Label, flags = string(b.Kind), append(flags, "private")
		}
		if b.Note != "" {
			flags = append(flags, "note: "+b.Note)
		}
		bl.Flags = strings.Join(flags, "; ")
		in.Blocks = append(in.Blocks, bl)
	}
	for _, g := range rc.Gaps {
		in.Gaps = append(in.Gaps, daily.GapSpan{Start: g.Start.In(s.loc).Format("15:04"), End: g.End.In(s.loc).Format("15:04"), Duration: g.Duration.D()})
	}
	copy(in.Top[:], rc.Top)
	for _, it := range rc.DayItems {
		in.DayItems = append(in.DayItems, itemLine(it))
	}
	for _, it := range rc.OpenItems {
		if it.Date != rc.Date && len(in.Open) < maxOpenInDigest {
			in.Open = append(in.Open, itemLine(it))
		}
	}
	for _, i := range rc.NewIdeas {
		in.Ideas = append(in.Ideas, i.Title)
	}
	for _, a := range rc.Agents {
		if a.State == string(domain.AgentActionRequired) || a.State == string(domain.AgentError) {
			in.Agents = append(in.Agents, daily.AgentLine{Label: a.Label, State: a.State, Summary: a.Summary})
		}
	}
	for _, c := range rc.Activity.Categories {
		in.Categories = append(in.Categories, daily.TimeShare{Label: c.Name, Time: c.Seconds.D()})
	}
	for _, w := range rc.Activity.Workspaces {
		label := w.Name
		if w.ContextID != "" {
			label += " -> " + w.ContextID
		}
		in.Workspaces = append(in.Workspaces, daily.TimeShare{Label: label, Time: w.Seconds.D()})
	}
	for _, p := range rc.Activity.Periods {
		in.Periods = append(in.Periods, daily.PeriodLine{Start: p.Start.In(s.loc).Format("15:04"), End: p.End.In(s.loc).Format("15:04"),
			State: string(p.State), Duration: p.Duration.D()})
	}
	for _, c := range rc.Contexts {
		if !c.Private {
			in.Contexts = append(in.Contexts, c.ID)
		}
	}
	return in
}

// ReviewPromptResult is the outcome of ReviewPrompt.
type ReviewPromptResult struct {
	Date      string `json:"date"`
	Basis     string `json:"basis"`
	Chars     int    `json:"chars"`
	Clipboard bool   `json:"clipboard"`
	Prompt    string `json:"prompt"`
}

// ReviewPrompt renders the daily review prompt for a day ("" = previous
// working day) and copies it to the clipboard when asked. When only the
// clipboard fails, the result is filled in and the error wraps
// domain.ErrUnavailable. The prompt text is never logged.
func (s *Service) ReviewPrompt(ctx context.Context, date string, clipboard bool) (ReviewPromptResult, error) {
	rc, err := s.ReviewContext(ctx, date)
	if err != nil {
		return ReviewPromptResult{}, err
	}
	day, _ := time.ParseInLocation(dateLayout, rc.Date, s.loc)
	prompt, err := daily.Render(s.promptsDir, daily.ReviewDayTemplate, daily.ReviewDayData{
		Date: rc.Date, Weekday: day.Weekday().String(), Basis: rc.Basis, Context: daily.BuildReviewContext(s.reviewInput(rc)),
	})
	if err != nil {
		return ReviewPromptResult{}, err
	}
	res := ReviewPromptResult{Date: rc.Date, Basis: rc.Basis, Chars: len([]rune(prompt)), Prompt: prompt}
	s.log.Info("review prompt", "date", rc.Date, "basis", rc.Basis, "chars", res.Chars, "clipboard", clipboard)
	if clipboard {
		if err := s.clipboard.WriteText(ctx, prompt); err != nil {
			return res, err
		}
		res.Clipboard = true
	}
	return res, nil
}
