package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

func scanEvent(r scanner) (domain.Event, error) {
	var (
		e         domain.Event
		ts, src   string
		note      sql.NullString
		confirmed int
	)
	if err := r.Scan(&e.ID, &ts, &e.ContextID, &src, &note, &confirmed); err != nil {
		return e, err
	}
	t, err := ParseTS(ts)
	if err != nil {
		return e, fmt.Errorf("event %d: bad timestamp %q: %w", e.ID, ts, err)
	}
	e.TS, e.Source, e.Note, e.Confirmed = t, domain.EventSource(src), note.String, confirmed != 0
	return e, nil
}

func (s *Store) oneEvent(ctx context.Context, q string, args ...any) (domain.Event, error) {
	e, err := scanEvent(s.q.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return e, domain.New(domain.ErrNotFound, "no event")
	}
	if err != nil {
		return e, fmt.Errorf("get event: %w", err)
	}
	return e, nil
}

// InsertEvent stores e and returns it with its new id.
func (s *Store) InsertEvent(ctx context.Context, e domain.Event) (domain.Event, error) {
	res, err := s.q.ExecContext(ctx, sqlInsertEvent, FormatTS(e.TS), e.ContextID, string(e.Source), nullStr(e.Note), b2i(e.Confirmed))
	if err != nil {
		return e, fmt.Errorf("insert event: %w", err)
	}
	e.ID, err = res.LastInsertId()
	e.TS = e.TS.UTC().Truncate(time.Millisecond)
	return e, err
}

// GetEvent returns the event with id, or ErrNotFound.
func (s *Store) GetEvent(ctx context.Context, id int64) (domain.Event, error) {
	e, err := s.oneEvent(ctx, sqlGetEvent, id)
	if errors.Is(err, domain.ErrNotFound) {
		return e, notFound("event", id)
	}
	return e, err
}

// LastEvent returns the latest event at or before t, or ErrNotFound.
func (s *Store) LastEvent(ctx context.Context, t time.Time) (domain.Event, error) {
	return s.oneEvent(ctx, sqlLastEventAtOrBef, FormatTS(t))
}

// NextEvent returns the event that follows e in (ts, id) order, or ErrNotFound.
func (s *Store) NextEvent(ctx context.Context, e domain.Event) (domain.Event, error) {
	ts := FormatTS(e.TS)
	return s.oneEvent(ctx, sqlNextEvent, ts, ts, e.ID)
}

// PrevEvent returns the event that precedes e in (ts, id) order, or ErrNotFound.
func (s *Store) PrevEvent(ctx context.Context, e domain.Event) (domain.Event, error) {
	ts := FormatTS(e.TS)
	return s.oneEvent(ctx, sqlPrevEvent, ts, ts, e.ID)
}

// EventsBetween returns events with from <= ts < to, ordered by time.
func (s *Store) EventsBetween(ctx context.Context, from, to time.Time) ([]domain.Event, error) {
	rows, err := s.q.QueryContext(ctx, sqlEventsBetween, FormatTS(from), FormatTS(to))
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateEvent overwrites every column of the event e.ID.
func (s *Store) UpdateEvent(ctx context.Context, e domain.Event) error {
	res, err := s.q.ExecContext(ctx, sqlUpdateEvent, FormatTS(e.TS), e.ContextID, string(e.Source), nullStr(e.Note), b2i(e.Confirmed), e.ID)
	if err != nil {
		return fmt.Errorf("update event %d: %w", e.ID, err)
	}
	return mustAffect(res, "event", e.ID)
}

// DeleteEvent removes the event with id.
func (s *Store) DeleteEvent(ctx context.Context, id int64) error {
	res, err := s.q.ExecContext(ctx, sqlDeleteEvent, id)
	if err != nil {
		return fmt.Errorf("delete event %d: %w", id, err)
	}
	return mustAffect(res, "event", id)
}
