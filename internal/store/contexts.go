package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
)

type scanner interface{ Scan(dest ...any) error }

func scanContext(r scanner) (domain.Context, error) {
	var (
		c                               domain.Context
		kind                            string
		ticket, notebook, workdir, icon sql.NullString
		active                          int
		sortV                           sql.NullInt64
	)
	if err := r.Scan(&c.ID, &c.Label, &kind, &ticket, &active, &notebook, &workdir, &icon, &sortV); err != nil {
		return c, err
	}
	c.Kind = domain.ContextKind(kind)
	c.TempoTicket, c.NotebookURL, c.Workdir, c.Icon = ticket.String, notebook.String, workdir.String, icon.String
	c.Active = active != 0
	c.Sort = int(sortV.Int64)
	return c, nil
}

// GetContext returns the context with id, or ErrNotFound.
func (s *Store) GetContext(ctx context.Context, id string) (domain.Context, error) {
	c, err := scanContext(s.q.QueryRowContext(ctx, sqlGetContext, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, notFound("context", id)
	}
	if err != nil {
		return c, fmt.Errorf("get context %s: %w", id, err)
	}
	return c, nil
}

// ListContexts returns contexts ordered by sort then id. Parked contexts are
// included only when includeParked is true.
func (s *Store) ListContexts(ctx context.Context, includeParked bool) ([]domain.Context, error) {
	q := sqlListActiveCtx
	if includeParked {
		q = sqlListContexts
	}
	rows, err := s.q.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list contexts: %w", err)
	}
	defer rows.Close()
	out := []domain.Context{}
	for rows.Next() {
		c, err := scanContext(rows)
		if err != nil {
			return nil, fmt.Errorf("list contexts: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertContext adds c. A duplicate id returns ErrConflict.
func (s *Store) InsertContext(ctx context.Context, c domain.Context) error {
	_, err := s.q.ExecContext(ctx, sqlInsertContext, c.ID, c.Label, string(c.Kind), nullStr(c.TempoTicket),
		b2i(c.Active), nullStr(c.NotebookURL), nullStr(c.Workdir), nullStr(c.Icon), c.Sort)
	if isUnique(err) {
		return domain.New(domain.ErrConflict, fmt.Sprintf("context %q already exists", c.ID))
	}
	if err != nil {
		return fmt.Errorf("insert context %s: %w", c.ID, err)
	}
	return nil
}

// UpdateContext overwrites every column of the context c.ID.
func (s *Store) UpdateContext(ctx context.Context, c domain.Context) error {
	res, err := s.q.ExecContext(ctx, sqlUpdateContext, c.Label, string(c.Kind), nullStr(c.TempoTicket), b2i(c.Active),
		nullStr(c.NotebookURL), nullStr(c.Workdir), nullStr(c.Icon), c.Sort, c.ID)
	if err != nil {
		return fmt.Errorf("update context %s: %w", c.ID, err)
	}
	return mustAffect(res, "context", c.ID)
}

// SetContextActive parks (false) or activates (true) a context.
func (s *Store) SetContextActive(ctx context.Context, id string, active bool) error {
	res, err := s.q.ExecContext(ctx, sqlSetContextActiv, b2i(active), id)
	if err != nil {
		return fmt.Errorf("set context %s active: %w", id, err)
	}
	return mustAffect(res, "context", id)
}

func mustAffect(res sql.Result, what string, id any) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound(what, id)
	}
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
