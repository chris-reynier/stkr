package service

import (
	"context"
	"errors"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

// EventPatch changes selected fields of an event. Nil fields are kept.
type EventPatch struct {
	TS        *time.Time
	ContextID *string
	Note      *string
}

func (s *Service) local(e domain.Event) domain.Event {
	e.TS = e.TS.In(s.loc)
	return e
}

// GetEvent returns one event.
func (s *Service) GetEvent(ctx context.Context, id int64) (domain.Event, error) {
	e, err := s.st.GetEvent(ctx, id)
	return s.local(e), err
}

// EditEvent changes the time, context or note of an event. The time may not
// be in the future and the context must exist.
func (s *Service) EditEvent(ctx context.Context, id int64, p EventPatch) (domain.Event, error) {
	var out domain.Event
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		e, err := tx.GetEvent(ctx, id)
		if err != nil {
			return err
		}
		if p.TS != nil {
			if p.TS.After(s.clk.Now()) {
				return invalid("event time cannot be in the future")
			}
			e.TS = *p.TS
		}
		if p.ContextID != nil {
			if err := domain.CheckContextID(*p.ContextID); err != nil {
				return err
			}
			if _, err := tx.GetContext(ctx, *p.ContextID); err != nil {
				return err
			}
			e.ContextID = *p.ContextID
		}
		if p.Note != nil {
			if err := checkNote(*p.Note); err != nil {
				return err
			}
			e.Note = *p.Note
		}
		out = e
		return tx.UpdateEvent(ctx, e)
	})
	if err == nil {
		s.log.Info("event edited", "event", id)
	}
	return s.local(out), err
}

// SplitEvent splits the block started by event id at time at, by inserting
// a new event with the same context, source and confirmation. at must lie
// strictly after the event and before both the next event and now.
func (s *Service) SplitEvent(ctx context.Context, id int64, at time.Time) (domain.Event, error) {
	var out domain.Event
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		e, err := tx.GetEvent(ctx, id)
		if err != nil {
			return err
		}
		limit := s.clk.Now()
		if next, err := tx.NextEvent(ctx, e); err == nil && next.TS.Before(limit) {
			limit = next.TS
		} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if !at.After(e.TS) || !at.Before(limit) {
			return invalid("split time must be after the event and before the next event and now")
		}
		out, err = tx.InsertEvent(ctx, domain.Event{TS: at, ContextID: e.ContextID, Source: e.Source, Confirmed: e.Confirmed})
		return err
	})
	if err == nil {
		s.log.Info("event split", "event", id, "new_event", out.ID)
	}
	return s.local(out), err
}

// MergeEvents merges two adjacent blocks: the later event is deleted, so the
// earlier block absorbs it. Notes are joined. The ids may be given in any
// order but no other event may lie between them.
func (s *Service) MergeEvents(ctx context.Context, a, b int64) (domain.Event, error) {
	if a == b {
		return domain.Event{}, invalid("cannot merge an event with itself")
	}
	var out domain.Event
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		first, err := tx.GetEvent(ctx, a)
		if err != nil {
			return err
		}
		second, err := tx.GetEvent(ctx, b)
		if err != nil {
			return err
		}
		if second.TS.Before(first.TS) || (second.TS.Equal(first.TS) && second.ID < first.ID) {
			first, second = second, first
		}
		next, err := tx.NextEvent(ctx, first)
		if err != nil || next.ID != second.ID {
			return invalid("events %d and %d are not adjacent", a, b)
		}
		switch {
		case first.Note == "":
			first.Note = second.Note
		case second.Note != "":
			first.Note += "; " + second.Note
		}
		if err := checkNote(first.Note); err != nil {
			return err
		}
		if err := tx.UpdateEvent(ctx, first); err != nil {
			return err
		}
		out = first
		return tx.DeleteEvent(ctx, second.ID)
	})
	if err == nil {
		s.log.Info("events merged", "event", out.ID, "a", a, "b", b)
	}
	return s.local(out), err
}

// DeleteEvent removes an event; the previous block extends over its time.
func (s *Service) DeleteEvent(ctx context.Context, id int64) error {
	err := s.st.DeleteEvent(ctx, id)
	if err == nil {
		s.log.Info("event deleted", "event", id)
	}
	return err
}

// ConfirmEvent marks the block started by event id as worked (confirmed) so
// an idle-suspect block counts, or clears the mark.
func (s *Service) ConfirmEvent(ctx context.Context, id int64, confirmed bool) (domain.Event, error) {
	var out domain.Event
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		e, err := tx.GetEvent(ctx, id)
		if err != nil {
			return err
		}
		e.Confirmed = confirmed
		out = e
		return tx.UpdateEvent(ctx, e)
	})
	if err == nil {
		s.log.Info("event confirmed", "event", id, "confirmed", confirmed)
	}
	return s.local(out), err
}
