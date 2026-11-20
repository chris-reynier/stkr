package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: name must be NNNN_name.sql", e.Name())
		}
		v, err := strconv.Atoi(prefix)
		if err != nil || v < 1 {
			return nil, fmt.Errorf("migration %s: bad version", e.Name())
		}
		b, err := fs.ReadFile(fsys, path.Join("migrations", e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

// Migrate applies pending embedded migrations, each in its own transaction.
// If the database already holds tables, it is first copied to
// backupDir/pre-migrate-<current version>-<UTC timestamp>.db. now names the
// backup file. It returns the versions applied.
func (s *Store) Migrate(ctx context.Context, backupDir string, now time.Time) ([]int, error) {
	return s.migrate(ctx, migrationFS, backupDir, now)
}

func (s *Store) migrate(ctx context.Context, fsys fs.FS, backupDir string, now time.Time) ([]int, error) {
	all, err := loadMigrations(fsys)
	if err != nil {
		return nil, err
	}
	tables, err := s.Tables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	existing := len(tables) > 0
	if _, err := s.db.ExecContext(ctx, sqlCreateMigrations); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	cur, err := s.SchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	var pending []migration
	for _, m := range all {
		if m.version > cur {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil, nil
	}
	if existing {
		name := fmt.Sprintf("pre-migrate-%d-%s.db", cur, now.UTC().Format("20060102T150405Z"))
		if err := s.Backup(ctx, filepath.Join(backupDir, name)); err != nil {
			return nil, fmt.Errorf("pre-migration backup: %w", err)
		}
	}
	var applied []int
	for _, m := range pending {
		err := s.InTx(ctx, func(tx *Store) error {
			if _, err := tx.q.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("apply migration %s: %w", m.name, err)
			}
			_, err := tx.q.ExecContext(ctx, sqlInsertMigration, m.version, FormatTS(now))
			return err
		})
		if err != nil {
			return applied, err
		}
		applied = append(applied, m.version)
	}
	return applied, nil
}

// SchemaVersion returns the highest applied migration version (0 if none).
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	if err := s.q.QueryRowContext(ctx, sqlSchemaVersion).Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}
