// Package legacy archives files from the earlier Python version of the tool.
// It only copies files; it never parses them and never touches the database.
package legacy

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// KnownFiles are the legacy files that are archived when present.
var KnownFiles = []string{"tasklog.json", "entries.json", "archive.json", "settings.json", "itemcount.json"}

// Result describes an archive run.
type Result struct {
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
}

// Archive copies the known legacy files from src (a directory, or one known
// file) into backupsDir/legacy-<UTC timestamp>/. Sources are opened
// read-only. It returns ErrNotFound when src holds no known file.
func Archive(src, backupsDir string, now time.Time) (Result, error) {
	info, err := os.Stat(src)
	if err != nil {
		return Result{}, domain.New(domain.ErrNotFound, fmt.Sprintf("legacy path %s: %v", src, err))
	}
	var found []string
	if info.IsDir() {
		for _, name := range KnownFiles {
			p := filepath.Join(src, name)
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				found = append(found, p)
			}
		}
	} else if isKnown(filepath.Base(src)) {
		found = append(found, src)
	}
	if len(found) == 0 {
		return Result{}, domain.New(domain.ErrNotFound, fmt.Sprintf("no legacy files found in %s (looked for %v)", src, KnownFiles))
	}
	dir := filepath.Join(backupsDir, "legacy-"+now.UTC().Format("20060102T150405Z"))
	if _, err := os.Stat(dir); err == nil {
		return Result{}, domain.New(domain.ErrConflict, "archive directory already exists: "+dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create archive dir: %w", err)
	}
	res := Result{Dir: dir}
	for _, p := range found {
		name := filepath.Base(p)
		if err := copyFile(p, filepath.Join(dir, name)); err != nil {
			return res, err
		}
		res.Files = append(res.Files, name)
	}
	return res, nil
}

func isKnown(name string) bool {
	for _, k := range KnownFiles {
		if k == name {
			return true
		}
	}
	return false
}

// copyFile copies src to dst via a temp file, fsync and rename.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // user-supplied legacy path, opened read-only
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync %s: %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %s: %w", dst, err)
	}
	return nil
}
