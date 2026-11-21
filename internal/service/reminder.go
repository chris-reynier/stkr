package service

import (
	"context"
	"errors"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

const kvCloseDue = "reminder.close_due"

// CloseState is the state of today's close, for the close key and the UI.
type CloseState struct {
	// Date is the local date this state is about.
	Date string `json:"date"`
	// WorkingDay is true on a weekday with tracked time.
	WorkingDay bool `json:"working_day"`
	// Saved is true once a close was imported today.
	Saved bool `json:"saved"`
	// Due is true after close_reminder on a working day while Saved is false.
	Due bool `json:"due"`
	// Blink is the current phase of the blinking key: true on even seconds
	// while Due (the key is yellow), false otherwise.
	Blink bool `json:"blink"`
}

// CloseState computes whether the close of today is due (initial.md 12.5).
func (s *Service) CloseState(ctx context.Context) (CloseState, error) {
	rep, err := s.Day(ctx, "today")
	if err != nil {
		return CloseState{}, err
	}
	return s.closeStateFrom(ctx, rep)
}

// closeStateFrom is CloseState with today's report already computed.
func (s *Service) closeStateFrom(ctx context.Context, rep domain.DayReport) (CloseState, error) {
	now := s.clk.Now().In(s.loc)
	today := timeline.Midnight(now, s.loc)
	rt, err := timeline.ParseTimeOfDay(s.cfg.CloseReminder)
	if err != nil {
		return CloseState{}, err
	}
	plan, err := s.st.GetDay(ctx, today.Format(dateLayout))
	if err != nil {
		return CloseState{}, err
	}
	st := CloseState{Date: rep.Date, WorkingDay: rep.WorkingDay, Saved: plan.CloseSaved}
	st.Due = st.WorkingDay && !st.Saved && now.After(rt.On(today, s.loc))
	st.Blink = st.Due && now.Unix()%2 == 0
	return st, nil
}

// ReminderTick is the close reminder worker: once per day it records (and
// logs) that the close became due. The key image itself is computed from
// CloseState on every request, so it never depends on this worker.
func (s *Service) ReminderTick(ctx context.Context) error {
	st, err := s.CloseState(ctx)
	if err != nil || !st.Due {
		return err
	}
	last, err := s.st.GetKV(ctx, kvCloseDue)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if last == st.Date {
		return nil
	}
	s.log.Info("close reminder due", "date", st.Date, "at", s.clk.Now().Format(time.RFC3339))
	return s.st.SetKV(ctx, kvCloseDue, st.Date)
}
