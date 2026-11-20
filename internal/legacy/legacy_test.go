package legacy

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

var now = time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)

type snap struct {
	sum   [32]byte
	mtime time.Time
}

func snapshot(t *testing.T, dir string) map[string]snap {
	t.Helper()
	out := map[string]snap{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fi, _ := os.Stat(p)
		out[e.Name()] = snap{sha256.Sum256(b), fi.ModTime()}
	}
	return out
}

func TestArchiveCopiesKnownFilesOnly(t *testing.T) {
	src := t.TempDir()
	home := t.TempDir()
	backups := filepath.Join(home, "backups")
	files := map[string]string{"tasklog.json": `{"a":1}`, "settings.json": `{}`, "itemcount.json": "3", "notes.txt": "skip"}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(src, n), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, src)

	res, err := Archive(src, backups, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Dir != filepath.Join(backups, "legacy-20261120T180000Z") || len(res.Files) != 3 {
		t.Fatalf("result: %+v", res)
	}
	for _, n := range res.Files {
		b, err := os.ReadFile(filepath.Join(res.Dir, n))
		if err != nil || string(b) != files[n] {
			t.Errorf("%s: %q %v", n, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "notes.txt")); err == nil {
		t.Error("unknown file copied")
	}
	// Sources unchanged, and no database was created anywhere in home.
	after := snapshot(t, src)
	if len(after) != len(before) {
		t.Fatal("source dir changed")
	}
	for n, s := range before {
		if after[n] != s {
			t.Errorf("source %s modified", n)
		}
	}
	_ = filepath.Walk(home, func(p string, _ os.FileInfo, _ error) error {
		if filepath.Ext(p) == ".db" {
			t.Errorf("database touched: %s", p)
		}
		return nil
	})
	// Running again in the same second refuses to overwrite.
	if _, err := Archive(src, backups, now); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second run: %v", err)
	}
}

func TestArchiveSingleFileAndErrors(t *testing.T) {
	src := t.TempDir()
	backups := t.TempDir()
	p := filepath.Join(src, "entries.json")
	_ = os.WriteFile(p, []byte("[]"), 0o600)
	res, err := Archive(p, backups, now)
	if err != nil || len(res.Files) != 1 || res.Files[0] != "entries.json" {
		t.Fatalf("single: %+v %v", res, err)
	}
	other := filepath.Join(src, "other.json")
	_ = os.WriteFile(other, []byte("{}"), 0o600)
	for _, bad := range []string{other, filepath.Join(src, "missing"), t.TempDir()} {
		if _, err := Archive(bad, backups, now.Add(time.Hour)); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
