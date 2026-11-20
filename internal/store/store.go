// Package store is the SQLite persistence layer. It holds plain SQL and no
// business logic. Only internal/service may import it.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/chris-reynier/stkr/internal/domain"
)

// tsLayout is the stored timestamp format: RFC3339 UTC with milliseconds.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// FormatTS formats t for storage.
func FormatTS(t time.Time) string { return t.UTC().Format(tsLayout) }

// ParseTS parses a stored timestamp.
func ParseTS(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

// querier is satisfied by *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store is a handle on the database. A Store passed to the InTx callback runs
// every method inside that transaction.
type Store struct {
	db   *sql.DB
	q    querier
	inTx bool
	path string
}

// Open opens (creating if needed) the SQLite database at path with WAL mode,
// a 5 second busy timeout and foreign keys on. It does not migrate.
func Open(ctx context.Context, path string) (*Store, error) {
	v := url.Values{}
	v.Add("_pragma", "busy_timeout(5000)")
	v.Add("_pragma", "journal_mode(WAL)")
	v.Add("_pragma", "foreign_keys(1)")
	v.Add("_pragma", "synchronous(NORMAL)")
	v.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", path+"?"+v.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return &Store{db: db, q: db, path: path}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// InTx runs fn in a write transaction. Nested calls reuse the outer one.
func (s *Store) InTx(ctx context.Context, fn func(tx *Store) error) error {
	if s.inTx {
		return fn(s)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(&Store{db: s.db, q: tx, inTx: true, path: s.path}); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Backup writes a consistent copy of the database to dst using VACUUM INTO.
// dst must not exist.
func (s *Store) Backup(ctx context.Context, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return domain.New(domain.ErrConflict, "backup file already exists: "+dst)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("backup to %s: %w", dst, err)
	}
	_ = os.Chmod(dst, 0o600)
	return nil
}

// Tables returns the names of the user tables, sorted.
func (s *Store) Tables(ctx context.Context) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, sqlListTables)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetKV returns the value of key, or ErrNotFound.
func (s *Store) GetKV(ctx context.Context, key string) (string, error) {
	var v sql.NullString
	err := s.q.QueryRowContext(ctx, sqlGetKV, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.New(domain.ErrNotFound, "no value for "+key)
	}
	if err != nil {
		return "", fmt.Errorf("get kv %s: %w", key, err)
	}
	return v.String, nil
}

// SetKV stores value under key.
func (s *Store) SetKV(ctx context.Context, key, value string) error {
	if _, err := s.q.ExecContext(ctx, sqlSetKV, key, value); err != nil {
		return fmt.Errorf("set kv %s: %w", key, err)
	}
	return nil
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func notFound(what string, id any) error {
	return domain.New(domain.ErrNotFound, fmt.Sprintf("%s %v not found", what, id))
}

// parseOptTS parses a nullable stored timestamp; empty yields the zero time.
func parseOptTS(s sql.NullString) (time.Time, error) {
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	return ParseTS(s.String)
}
