package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

// PasteWait is how long the paste helper waits for the target window.
const PasteWait = 10 * time.Second

const (
	maxWindowTitle = 200
	// historyDays is how far back Start Day looks for the last five working
	// days (enough for holidays and long weekends).
	historyDays     = 21
	maxOpenInDigest = 12
	maxIdeaTitles   = 5
)

// StartDayOptions tune StartDay.
type StartDayOptions struct {
	// Source is the event source of the planning switch (default api).
	Source domain.EventSource
	// Clipboard copies the prompt to the clipboard.
	Clipboard bool
}

// DayResult is the outcome of StartDay and ClosePrompt.
type DayResult struct {
	OK    bool `json:"ok"`
	Chars int  `json:"chars"`
	// Clipboard tells whether the prompt was copied.
	Clipboard bool `json:"clipboard"`
	// Pasted tells whether the prompt was pasted into the target window.
	Pasted bool `json:"pasted,omitempty"`
	// Exported is the path, relative to onedrive_dir, of the digest file.
	Exported string   `json:"exported,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	// Prompt is the full text; it is only for callers that print it.
	Prompt string `json:"-"`
}

// StartDay logs planning, builds the digest, renders the Start Day prompt,
// writes _daily/daily-digest.md (when onedrive_dir is set) and optionally
// copies the prompt to the clipboard. When only the clipboard fails, the
// result is filled in and the error wraps domain.ErrUnavailable.
func (s *Service) StartDay(ctx context.Context, opts StartDayOptions) (DayResult, error) {
	src := opts.Source
	if src == "" {
		src = domain.SourceAPI
	}
	if _, err := s.Switch(ctx, "planning", src, ""); err != nil {
		return DayResult{}, err
	}
	today := timeline.Midnight(s.clk.Now(), s.loc)
	in, err := s.digestInput(ctx, today)
	if err != nil {
		return DayResult{}, err
	}
	digest := daily.BuildDigest(in)
	prompt, err := daily.Render(s.promptsDir, daily.StartDayTemplate, daily.StartDayData{
		Date: today.Format(dateLayout), Weekday: today.Weekday().String(), Digest: digest,
	})
	if err != nil {
		return DayResult{}, err
	}
	res := DayResult{OK: true, Chars: len([]rune(prompt)), Prompt: prompt}
	if s.cfg.OneDriveDir != "" {
		body := "# Daily digest " + today.Format(dateLayout) + "\n\n" + digest + "\n"
		if err := s.writeExport(digestFile, []byte(body)); err != nil {
			s.log.Warn("digest export failed", "err", err)
			res.Warnings = append(res.Warnings, "could not write "+digestFile+": "+err.Error())
		} else {
			res.Exported = digestFile
		}
	}
	s.log.Info("start day", "chars", res.Chars, "clipboard", opts.Clipboard)
	if opts.Clipboard {
		if err := s.clipboard.WriteText(ctx, prompt); err != nil {
			return res, err
		}
		res.Clipboard = true
	}
	return res, nil
}

// CloseOptions tune ClosePrompt.
type CloseOptions struct {
	// Clipboard copies the prompt to the clipboard (implied by Paste).
	Clipboard bool
	// Paste pastes the prompt into the window named by Window (default
	// agent_window_title) after copying it.
	Paste  bool
	Window string
	Enter  bool
}

// ClosePrompt builds the Close Day prompt from today's time, items, ideas,
// agent state and the morning top three, and copies and pastes it as asked.
func (s *Service) ClosePrompt(ctx context.Context, opts CloseOptions) (DayResult, error) {
	today := timeline.Midnight(s.clk.Now(), s.loc)
	in, err := s.closeInput(ctx, today)
	if err != nil {
		return DayResult{}, err
	}
	prompt, err := daily.Render(s.promptsDir, daily.CloseDayTemplate, daily.CloseDayData{
		Date: today.Format(dateLayout), Weekday: today.Weekday().String(), Context: daily.BuildCloseContext(in),
	})
	if err != nil {
		return DayResult{}, err
	}
	res := DayResult{OK: true, Chars: len([]rune(prompt)), Prompt: prompt}
	s.log.Info("close prompt", "chars", res.Chars, "clipboard", opts.Clipboard || opts.Paste, "paste", opts.Paste)
	if opts.Clipboard || opts.Paste {
		if err := s.clipboard.WriteText(ctx, prompt); err != nil {
			return res, err
		}
		res.Clipboard = true
	}
	if opts.Paste {
		if err := s.Paste(ctx, opts.Window, opts.Enter); err != nil {
			return res, err
		}
		res.Pasted = true
	}
	return res, nil
}

// Paste pastes the clipboard into the window whose title contains window
// (default: the agent_window_title setting), waiting up to PasteWait for it.
// A missing window is domain.ErrNotFound; no window title at all is
// domain.ErrInvalid.
func (s *Service) Paste(ctx context.Context, window string, enter bool) error {
	window = strings.TrimSpace(window)
	if window == "" {
		window = strings.TrimSpace(s.cfg.AgentWindowTitle)
	}
	if window == "" {
		return invalid("no window title: pass one or set agent_window_title (`stkr config set agent_window_title <text>`)")
	}
	if len([]rune(window)) > maxWindowTitle {
		return invalid("window title is longer than %d characters", maxWindowTitle)
	}
	err := s.windows.PasteInto(ctx, window, enter, PasteWait)
	switch {
	case err == nil:
		s.log.Info("paste", "window_len", len(window), "enter", enter)
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return domain.New(domain.ErrNotFound, fmt.Sprintf("no window with a title containing %q appeared within %s", window, PasteWait))
	}
	return err
}

// dayReports returns the reports of n consecutive local days from first,
// oldest first, with one database read.
func (s *Service) dayReports(ctx context.Context, first time.Time, n int) ([]domain.DayReport, error) {
	y, m, d := first.In(s.loc).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, s.loc)
	end := time.Date(y, m, d+n, 0, 0, 0, 0, s.loc)
	events, err := s.st.EventsBetween(ctx, start, end)
	if err != nil {
		return nil, err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clk.Now()
	out := make([]domain.DayReport, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s.dayReport(time.Date(y, m, d+i, 0, 0, 0, 0, s.loc), events, ctxs, now))
	}
	return out, nil
}

func sharesOf(r domain.DayReport) ([]daily.TimeShare, time.Duration) {
	var out []daily.TimeShare
	var total time.Duration
	for _, t := range r.Totals {
		if !t.Exportable || t.Counted <= 0 {
			continue
		}
		out = append(out, daily.TimeShare{Label: t.Label, Time: t.Counted.D()})
		total += t.Counted.D()
	}
	return out, total
}

func (s *Service) gapsOf(r domain.DayReport) []daily.GapSpan {
	var out []daily.GapSpan
	for _, g := range r.Gaps {
		out = append(out, daily.GapSpan{Start: g.Start.In(s.loc).Format("15:04"), End: g.End.In(s.loc).Format("15:04"), Duration: g.Duration.D()})
	}
	return out
}

// visibleProject reports whether items of a project may be shown to an AI
// chat: personal and system contexts are never included.
func visibleProject(ctxs map[string]domain.Context, project string) bool {
	c, ok := ctxs[project]
	return !ok || c.Kind.Exportable()
}

func itemLine(it domain.Item) daily.ItemLine {
	return daily.ItemLine{Fingerprint: it.Fingerprint, Type: it.Type, Project: it.Project, Summary: it.Summary, Due: it.Due, Done: it.Status == "done"}
}

// openItems returns open items for AI prompts, most urgent first (due date,
// then newest), without personal or system projects.
func (s *Service) openItems(ctx context.Context, ctxs map[string]domain.Context, skipDate string) ([]domain.Item, error) {
	all, err := s.st.ListItems(ctx, "", "open")
	if err != nil {
		return nil, err
	}
	var out []domain.Item
	for _, it := range all {
		if it.Date == skipDate || !visibleProject(ctxs, it.Project) {
			continue
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Due == "") != (b.Due == "") {
			return a.Due != ""
		}
		if a.Due != b.Due {
			return a.Due < b.Due
		}
		return a.Date > b.Date
	})
	return out, nil
}

func (s *Service) attentionAgents(ctx context.Context) ([]daily.AgentLine, error) {
	l, err := s.Agents(ctx)
	if err != nil {
		return nil, err
	}
	var out []daily.AgentLine
	for _, a := range l.Instances {
		if a.State == domain.AgentActionRequired || a.State == domain.AgentError {
			out = append(out, daily.AgentLine{Label: a.Label, State: string(a.State), Summary: a.Summary})
		}
	}
	return out, nil
}

func ideaTitle(i domain.Idea) string {
	for _, t := range []string{i.Title, i.CleanText, i.RawText} {
		if t = strings.Join(strings.Fields(t), " "); t != "" {
			return t
		}
	}
	return fmt.Sprintf("idea %d", i.ID)
}

func (s *Service) digestInput(ctx context.Context, today time.Time) (daily.DigestInput, error) {
	var in daily.DigestInput
	first := today.AddDate(0, 0, -historyDays)
	reps, err := s.dayReports(ctx, first, historyDays) // up to and excluding today
	if err != nil {
		return in, err
	}
	for i := len(reps) - 1; i >= 0 && len(in.Days) < 5; i-- {
		r := reps[i]
		if !r.WorkingDay {
			continue
		}
		shares, total := sharesOf(r)
		in.Days = append(in.Days, daily.DayDigest{
			Date: r.Date, Weekday: r.Weekday, Shares: shares, Total: total,
			Gaps: s.gapsOf(r), IdleSuspect: r.IdleSuspect.D(),
		})
	}
	for i, d := range in.Days {
		row, err := s.st.GetDay(ctx, d.Date)
		if err != nil {
			return in, err
		}
		if i == 0 {
			in.YesterdayDate, in.YesterdayTop = d.Date, row.Top
		}
		if !row.CloseSaved && len(in.UnsavedClose) < 3 {
			in.UnsavedClose = append(in.UnsavedClose, d.Date)
		}
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return in, err
	}
	open, err := s.openItems(ctx, ctxs, "")
	if err != nil {
		return in, err
	}
	for i, it := range open {
		if i == maxOpenInDigest {
			break
		}
		in.OpenItems = append(in.OpenItems, itemLine(it))
	}
	ideas, err := s.st.ListIdeas(ctx, domain.IdeaNew)
	if err != nil {
		return in, err
	}
	for _, i := range ideas {
		if i.Tag != "pro" {
			continue
		}
		in.IdeaCount++
		if len(in.IdeaTitles) < maxIdeaTitles {
			in.IdeaTitles = append(in.IdeaTitles, ideaTitle(i))
		}
	}
	in.Agents, err = s.attentionAgents(ctx)
	return in, err
}

func (s *Service) closeInput(ctx context.Context, today time.Time) (daily.CloseInput, error) {
	date := today.Format(dateLayout)
	in := daily.CloseInput{Date: date}
	rep, err := s.Day(ctx, date)
	if err != nil {
		return in, err
	}
	in.Shares, in.Total = sharesOf(rep)
	in.Gaps = s.gapsOf(rep)
	row, err := s.st.GetDay(ctx, date)
	if err != nil {
		return in, err
	}
	in.TopsAM = row.Top
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return in, err
	}
	todays, err := s.st.ListItems(ctx, date, "")
	if err != nil {
		return in, err
	}
	for _, it := range todays {
		if visibleProject(ctxs, it.Project) {
			in.Today = append(in.Today, itemLine(it))
		}
	}
	open, err := s.openItems(ctx, ctxs, date)
	if err != nil {
		return in, err
	}
	for i, it := range open {
		if i == maxOpenInDigest {
			break
		}
		in.Open = append(in.Open, itemLine(it))
	}
	ideas, err := s.st.ListIdeas(ctx, "")
	if err != nil {
		return in, err
	}
	for _, i := range ideas {
		if i.Tag == "pro" && !i.TS.IsZero() && timeline.Midnight(i.TS, s.loc).Equal(today) {
			in.IdeaCnt++
			if len(in.Ideas) < maxIdeaTitles {
				in.Ideas = append(in.Ideas, ideaTitle(i))
			}
		}
	}
	in.Agents, err = s.attentionAgents(ctx)
	return in, err
}

// DayPlan returns the stored top three and saved flags of a local date.
func (s *Service) DayPlan(ctx context.Context, date string) (domain.Day, error) {
	d, err := s.ParseDate(date)
	if err != nil {
		return domain.Day{}, err
	}
	return s.st.GetDay(ctx, d.Format(dateLayout))
}

// ItemsOn returns the items captured on a local date.
func (s *Service) ItemsOn(ctx context.Context, date string) ([]domain.Item, error) {
	d, err := s.ParseDate(date)
	if err != nil {
		return nil, err
	}
	return s.st.ListItems(ctx, d.Format(dateLayout), "")
}
