package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
)

// BackupGenerations is how many regular backups are kept.
const BackupGenerations = 14

const (
	backupPrefix  = "stkr-"
	kvLastBackup  = "backup.last_date"
	backupTSClock = "20060102T150405Z"
)

// Backup writes a backup now to backups/stkr-<UTC timestamp>.db, prunes
// regular backups to the newest BackupGenerations, and returns the path.
// Pre-migration backups are never pruned.
func (s *Service) Backup(ctx context.Context) (string, error) {
	if s.backupDir == "" {
		return "", domain.New(domain.ErrUnavailable, "no backup directory configured")
	}
	if err := os.MkdirAll(s.backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	now := s.clk.Now()
	base := backupPrefix + now.UTC().Format(backupTSClock)
	dst := filepath.Join(s.backupDir, base+".db")
	for i := 2; fileExists(dst); i++ {
		dst = filepath.Join(s.backupDir, fmt.Sprintf("%s_%d.db", base, i))
	}
	if err := s.st.Backup(ctx, dst); err != nil {
		return "", err
	}
	if err := s.st.SetKV(ctx, kvLastBackup, now.In(s.loc).Format(dateLayout)); err != nil {
		return dst, err
	}
	if err := s.pruneBackups(); err != nil {
		return dst, err
	}
	s.log.Info("backup written", "path", dst)
	return dst, nil
}

// DailyBackup makes a backup unless one was already made today (local day).
// It returns the path and whether a backup was written.
func (s *Service) DailyBackup(ctx context.Context) (string, bool, error) {
	today := s.clk.Now().In(s.loc).Format(dateLayout)
	last, err := s.st.GetKV(ctx, kvLastBackup)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", false, err
	}
	if last == today {
		return "", false, nil
	}
	p, err := s.Backup(ctx)
	return p, err == nil, err
}

// Backups lists regular backup files, oldest first.
func (s *Service) Backups() ([]string, error) {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, backupPrefix) && strings.HasSuffix(n, ".db") {
			out = append(out, filepath.Join(s.backupDir, n))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) pruneBackups() error {
	list, err := s.Backups()
	if err != nil {
		return err
	}
	for len(list) > BackupGenerations {
		if err := os.Remove(list[0]); err != nil {
			return fmt.Errorf("prune backup: %w", err)
		}
		list = list[1:]
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
