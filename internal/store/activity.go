package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// ActivityUpsert is the outcome of UpsertActivity for one event.
type ActivityUpsert int

// UpsertActivity outcomes.
const (
	// ActivityInserted: the identity was new.
	ActivityInserted ActivityUpsert = iota
	// ActivityMerged: the identity existed and its duration or context grew.
	ActivityMerged
	// ActivityUnchanged: the identity existed with the same or more data.
	ActivityUnchanged
)

// UpsertActivity stores e under its identity e.ID. A known identity keeps the
// larger duration and the newest non-empty context, so replays and late or
// out-of-order arrivals give the same result.
func (s *Store) UpsertActivity(ctx context.Context, e domain.ActivityEvent) (ActivityUpsert, error) {
	var oldDur int64
	var oldCtx sql.NullString
	err := s.q.QueryRowContext(ctx, sqlGetActivity, e.ID).Scan(&oldDur, &oldCtx)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("get activity %s: %w", e.ID, err)
	}
	secs := int64(e.Duration.D() / time.Second)
	if _, err := s.q.ExecContext(ctx, sqlUpsertActivity, e.ID, FormatTS(e.TS), e.App, string(e.Category),
		nullStr(e.Workspace), nullStr(e.ContextID), secs, string(e.LockState), FormatTS(e.Received)); err != nil {
		return 0, fmt.Errorf("upsert activity: %w", err)
	}
	switch {
	case !found:
		return ActivityInserted, nil
	case secs > oldDur || (e.ContextID != "" && e.ContextID != oldCtx.String):
		return ActivityMerged, nil
	}
	return ActivityUnchanged, nil
}

// ActivityBetween returns activity events with from <= ts < to, oldest first
// (ties by identity, so the order is deterministic).
func (s *Store) ActivityBetween(ctx context.Context, from, to time.Time) ([]domain.ActivityEvent, error) {
	rows, err := s.q.QueryContext(ctx, sqlActivityBetween, FormatTS(from), FormatTS(to))
	if err != nil {
		return nil, fmt.Errorf("list activity: %w", err)
	}
	defer rows.Close()
	out := []domain.ActivityEvent{}
	for rows.Next() {
		var (
			e            domain.ActivityEvent
			ts, received string
			ws, ctxID    sql.NullString
			cat, lock    string
			secs         int64
		)
		if err := rows.Scan(&e.ID, &ts, &e.App, &cat, &ws, &ctxID, &secs, &lock, &received); err != nil {
			return nil, err
		}
		if e.TS, err = ParseTS(ts); err != nil {
			return nil, err
		}
		if e.Received, err = ParseTS(received); err != nil {
			return nil, err
		}
		e.Category, e.LockState = domain.ActivityCategory(cat), domain.LockState(lock)
		e.Workspace, e.ContextID = ws.String, ctxID.String
		e.Duration = domain.Duration(time.Duration(secs) * time.Second)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteActivityBefore removes activity events older than t and returns how
// many were removed.
func (s *Store) DeleteActivityBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.q.ExecContext(ctx, sqlDeleteActivityBef, FormatTS(t))
	if err != nil {
		return 0, fmt.Errorf("prune activity: %w", err)
	}
	return res.RowsAffected()
}
