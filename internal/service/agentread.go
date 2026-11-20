package service

import (
	"context"
	"sort"
	"strings"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

// ItemFilter selects items for ListItems.
type ItemFilter struct {
	// Status is open or done; empty matches any.
	Status string
	// Project is a context id or free text; empty matches any.
	Project string
	// Since is a local date (YYYY-MM-DD, today, yesterday); empty matches any.
	Since string
}

// ListItems returns items oldest first. Items of personal and system
// projects are never returned, so the result is safe to hand to agents.
func (s *Service) ListItems(ctx context.Context, f ItemFilter) ([]domain.Item, error) {
	status := strings.ToLower(strings.TrimSpace(f.Status))
	if status != "" && status != "open" && status != "done" {
		return nil, invalid("status must be open or done (got %q)", f.Status)
	}
	since := ""
	if strings.TrimSpace(f.Since) != "" {
		d, err := s.ParseDate(f.Since)
		if err != nil {
			return nil, err
		}
		since = d.Format(dateLayout)
	}
	ctxs, err := s.allContexts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Context, len(ctxs))
	for _, c := range ctxs {
		byID[c.ID] = c
	}
	all, err := s.st.ListItems(ctx, "", status)
	if err != nil {
		return nil, err
	}
	out := []domain.Item{}
	for _, it := range all {
		if !visibleProject(byID, it.Project) || it.Date < since {
			continue
		}
		if f.Project != "" && !strings.EqualFold(it.Project, f.Project) {
			continue
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// Digest kinds for Digest.
const (
	DigestStart = "start"
	DigestClose = "close"
)

// Digest returns the Start Day or Close Day prompt text. Unlike StartDay and
// ClosePrompt it has no side effects: no planning switch, no export, no AI
// call and no clipboard.
func (s *Service) Digest(ctx context.Context, kind string) (string, error) {
	today := timeline.Midnight(s.clk.Now(), s.loc)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case DigestStart:
		in, err := s.digestInput(ctx, today)
		if err != nil {
			return "", err
		}
		return daily.Render(s.promptsDir, daily.StartDayTemplate, daily.StartDayData{
			Date: today.Format(dateLayout), Weekday: today.Weekday().String(), Digest: daily.BuildDigest(in),
		})
	case DigestClose:
		in, err := s.closeInput(ctx, today)
		if err != nil {
			return "", err
		}
		return daily.Render(s.promptsDir, daily.CloseDayTemplate, daily.CloseDayData{
			Date: today.Format(dateLayout), Weekday: today.Weekday().String(), Context: daily.BuildCloseContext(in),
		})
	}
	return "", invalid("kind must be start or close (got %q)", kind)
}
