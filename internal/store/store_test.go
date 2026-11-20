package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/chris-reynier/stkr/internal/domain"
)

var t0 = time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(context.Background(), filepath.Join(dir, "stkr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(context.Background(), backups, t0); err != nil {
		t.Fatal(err)
	}
	return s, backups
}

func TestMigrateCreatesEverythingAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, backups := open(t)
	tables, err := s.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"agent_instances", "bookmarks", "contexts", "days", "events", "ideas", "items", "kv", "schema_migrations"}
	if diff := cmp.Diff(want, tables); diff != "" {
		t.Fatalf("tables (-want +got):\n%s", diff)
	}
	cs, err := s.ListContexts(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.ID+":"+string(c.Kind))
	}
	if diff := cmp.Diff([]string{"off:system", "break:personal", "planning:mode", "meeting:mode", "other:project"}, ids); diff != "" {
		t.Fatalf("seed (-want +got):\n%s", diff)
	}
	// A fresh database gets no pre-migration backup.
	if entries, _ := os.ReadDir(backups); len(entries) != 0 {
		t.Fatalf("unexpected backups: %v", entries)
	}
	applied, err := s.Migrate(ctx, backups, t0)
	if err != nil || len(applied) != 0 {
		t.Fatalf("second migrate: %v %v", applied, err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 1 {
		t.Fatalf("version = %d", v)
	}
	var pragmas [3]string
	for i, p := range []string{"journal_mode", "foreign_keys", "busy_timeout"} {
		if err := s.q.QueryRowContext(ctx, "PRAGMA "+p).Scan(&pragmas[i]); err != nil {
			t.Fatal(err)
		}
	}
	if pragmas != [3]string{"wal", "1", "5000"} {
		t.Fatalf("pragmas = %v", pragmas)
	}
}

func TestPreMigrationBackupFromFakeV0(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "stkr.db")
	// A v0 database: tables exist but no migration has been recorded.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE legacy_note (x TEXT); INSERT INTO legacy_note VALUES ('keep me')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	applied, err := s.Migrate(ctx, dir, t0)
	if err != nil || len(applied) != 1 {
		t.Fatalf("migrate: %v %v", applied, err)
	}
	bk := filepath.Join(dir, "pre-migrate-0-20261116T080000Z.db")
	b, err := sql.Open("sqlite", bk)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var x string
	if err := b.QueryRow(`SELECT x FROM legacy_note`).Scan(&x); err != nil || x != "keep me" {
		t.Fatalf("backup content: %q %v", x, err)
	}
	var n int
	if err := b.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'contexts'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("backup must hold the pre-migration schema: %d %v", n, err)
	}
}

func TestPendingMigrationBacksUpAndFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	s, backups := open(t)
	if _, err := s.InsertEvent(ctx, domain.Event{TS: t0, ContextID: "other", Source: domain.SourceCLI}); err != nil {
		t.Fatal(err)
	}
	first := embedded(t, "0001_init.sql")
	fsys := fstest.MapFS{
		"migrations/0001_init.sql":  first,
		"migrations/0002_extra.sql": {Data: []byte(`CREATE TABLE extra (id INTEGER); INSERT INTO extra VALUES (1);`)},
	}
	applied, err := s.migrate(ctx, fsys, backups, t0.Add(time.Hour))
	if err != nil || !cmp.Equal(applied, []int{2}) {
		t.Fatalf("migrate: %v %v", applied, err)
	}
	if _, err := os.Stat(filepath.Join(backups, "pre-migrate-1-20261116T090000Z.db")); err != nil {
		t.Fatalf("no pre-migration backup: %v", err)
	}
	// A failing migration leaves no partial state and keeps the version.
	fsys["migrations/0003_bad.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE half (id INTEGER); NOT SQL;`)}
	if _, err := s.migrate(ctx, fsys, backups, t0.Add(2*time.Hour)); err == nil {
		t.Fatal("bad migration should fail")
	}
	if v, _ := s.SchemaVersion(ctx); v != 2 {
		t.Fatalf("version after failure = %d", v)
	}
	tables, _ := s.Tables(ctx)
	for _, tb := range tables {
		if tb == "half" {
			t.Fatal("failed migration left a table behind")
		}
	}
}

func embedded(t *testing.T, name string) *fstest.MapFile {
	t.Helper()
	b, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return &fstest.MapFile{Data: b}
}

func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"no underscore": {"migrations/0001.sql": {Data: []byte("")}},
		"bad version":   {"migrations/abc_x.sql": {Data: []byte("")}},
		"duplicate":     {"migrations/0001_a.sql": {Data: []byte("")}, "migrations/0001_b.sql": {Data: []byte("")}},
		"missing dir":   {},
	} {
		if _, err := loadMigrations(fsys); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestContexts(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	c := domain.Context{ID: "acme", Label: "ACME", Kind: domain.KindProject, TempoTicket: "AC-1", Active: true, NotebookURL: "https://n", Workdir: "/w", Icon: "rocket", Sort: 5}
	if err := s.InsertContext(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertContext(ctx, c); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	got, err := s.GetContext(ctx, "acme")
	if err != nil || !cmp.Equal(c, got) {
		t.Fatalf("get: %+v %v", got, err)
	}
	c.Label, c.TempoTicket = "Acme", ""
	if err := s.UpdateContext(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetContext(ctx, "acme"); got.Label != "Acme" || got.TempoTicket != "" {
		t.Fatalf("update: %+v", got)
	}
	if err := s.SetContextActive(ctx, "acme", false); err != nil {
		t.Fatal(err)
	}
	active, _ := s.ListContexts(ctx, false)
	all, _ := s.ListContexts(ctx, true)
	if len(active) != 5 || len(all) != 6 {
		t.Fatalf("active %d all %d", len(active), len(all))
	}
	for _, err := range []error{
		s.UpdateContext(ctx, domain.Context{ID: "nope", Label: "x", Kind: domain.KindMode}),
		s.SetContextActive(ctx, "nope", true),
		func() error { _, err := s.GetContext(ctx, "nope"); return err }(),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("want not found, got %v", err)
		}
	}
	if err := s.InsertContext(ctx, domain.Context{ID: "bad", Label: "x", Kind: "weird"}); err == nil {
		t.Error("CHECK constraint on kind not enforced")
	}
}

func TestEvents(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	if _, err := s.LastEvent(ctx, t0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty last: %v", err)
	}
	e1, err := s.InsertEvent(ctx, domain.Event{TS: t0.Add(123456 * time.Microsecond), ContextID: "other", Source: domain.SourceDeck, Note: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if e1.TS != t0.Add(123*time.Millisecond) {
		t.Fatalf("ts not truncated to ms: %v", e1.TS)
	}
	e2, _ := s.InsertEvent(ctx, domain.Event{TS: t0.Add(time.Hour), ContextID: "meeting", Source: domain.SourceCLI})
	e3, _ := s.InsertEvent(ctx, domain.Event{TS: t0.Add(time.Hour), ContextID: "off", Source: domain.SourceCLI})

	got, err := s.GetEvent(ctx, e1.ID)
	if err != nil || !cmp.Equal(e1, got) {
		t.Fatalf("get: %+v vs %+v (%v)", got, e1, err)
	}
	if last, _ := s.LastEvent(ctx, t0.Add(2*time.Hour)); last.ID != e3.ID {
		t.Fatalf("last = %d", last.ID)
	}
	if last, _ := s.LastEvent(ctx, t0.Add(30*time.Minute)); last.ID != e1.ID {
		t.Fatalf("last before = %d", last.ID)
	}
	if n, _ := s.NextEvent(ctx, e2); n.ID != e3.ID {
		t.Fatalf("next of e2 = %d (same ts, higher id)", n.ID)
	}
	if p, _ := s.PrevEvent(ctx, e3); p.ID != e2.ID {
		t.Fatalf("prev of e3 = %d", p.ID)
	}
	if _, err := s.NextEvent(ctx, e3); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("next of last: %v", err)
	}
	evs, _ := s.EventsBetween(ctx, t0, t0.Add(time.Hour))
	if len(evs) != 1 || evs[0].ID != e1.ID {
		t.Fatalf("between: %+v", evs)
	}
	e1.Confirmed, e1.Note = true, ""
	if err := s.UpdateEvent(ctx, e1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetEvent(ctx, e1.ID); !got.Confirmed || got.Note != "" {
		t.Fatalf("update: %+v", got)
	}
	if _, err := s.InsertEvent(ctx, domain.Event{TS: t0, ContextID: "missing", Source: domain.SourceCLI}); err == nil {
		t.Fatal("foreign key not enforced")
	}
	if err := s.DeleteEvent(ctx, e2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEvent(ctx, e2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := s.UpdateEvent(ctx, e2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if _, err := s.GetEvent(ctx, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
}

func TestInTxRollsBack(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	boom := errors.New("boom")
	err := s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.InsertEvent(ctx, domain.Event{TS: t0, ContextID: "other", Source: domain.SourceCLI}); err != nil {
			return err
		}
		// Nested calls reuse the transaction.
		return tx.InTx(ctx, func(*Store) error { return boom })
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.LastEvent(ctx, t0.Add(time.Hour)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("transaction was not rolled back")
	}
}

func TestKVAndDays(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	if _, err := s.GetKV(ctx, "k"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	_ = s.SetKV(ctx, "k", "1")
	_ = s.SetKV(ctx, "k", "2")
	if v, _ := s.GetKV(ctx, "k"); v != "2" {
		t.Fatalf("kv = %q", v)
	}
	d, err := s.GetDay(ctx, "2026-11-16")
	if err != nil || d.Date != "2026-11-16" || d.BriefSaved {
		t.Fatalf("empty day: %+v %v", d, err)
	}
	d.Top = [3]string{"a", "", "c"}
	d.BriefSaved = true
	if err := s.UpsertDay(ctx, d); err != nil {
		t.Fatal(err)
	}
	d.CloseSaved = true
	_ = s.UpsertDay(ctx, d)
	if got, _ := s.GetDay(ctx, "2026-11-16"); !cmp.Equal(d, got) {
		t.Fatalf("day: %+v", got)
	}
}

func TestAgents(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	a := domain.AgentInstance{ID: "w1", Slot: 2, ContextID: "other", Workspace: "/ws", WindowTitle: "VS Code", State: domain.AgentWorking, Summary: "s", Updated: t0, Queued: true}
	if err := s.UpsertAgent(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.State = domain.AgentDone
	_ = s.UpsertAgent(ctx, a)
	_ = s.UpsertAgent(ctx, domain.AgentInstance{ID: "w0", Slot: 1, State: domain.AgentIdle, Updated: t0})
	got, err := s.GetAgent(ctx, "w1")
	if err != nil || !cmp.Equal(a, got) {
		t.Fatalf("get: %+v %v", got, err)
	}
	list, _ := s.ListAgents(ctx)
	if len(list) != 2 || list[0].ID != "w0" {
		t.Fatalf("list: %+v", list)
	}
	if err := s.DeleteAgent(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgent(ctx, "w1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.DeleteAgent(ctx, "w1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestIdeas(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	i, err := s.InsertIdea(ctx, domain.Idea{TS: t0, RawText: "raw", Source: "cli"})
	if err != nil || i.Tag != "pro" || i.Status != domain.IdeaNew || i.AIState != domain.AIPending {
		t.Fatalf("insert: %+v %v", i, err)
	}
	_, _ = s.InsertIdea(ctx, domain.Idea{TS: t0.Add(time.Hour), RawText: "two", Tag: "personal", Status: domain.IdeaTriaged, AIState: domain.AISkipped})
	i.CleanText, i.Title, i.Status, i.AIState = "clean", "T", domain.IdeaDone, domain.AIDone
	if err := s.UpdateIdea(ctx, i); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetIdea(ctx, i.ID)
	if err != nil || !cmp.Equal(i, got) {
		t.Fatalf("get: %+v %v", got, err)
	}
	all, _ := s.ListIdeas(ctx, "")
	done, _ := s.ListIdeas(ctx, domain.IdeaDone)
	skipped, _ := s.ListIdeasByAIState(ctx, domain.AISkipped)
	if len(all) != 2 || all[0].RawText != "two" || len(done) != 1 || len(skipped) != 1 {
		t.Fatalf("lists: %d %d %d", len(all), len(done), len(skipped))
	}
	if _, err := s.GetIdea(ctx, 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.UpdateIdea(ctx, domain.Idea{ID: 99}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestBookmarks(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	b, err := s.InsertBookmark(ctx, domain.Bookmark{Title: "Go", URL: "https://go.dev", Tags: []string{"lang", " dev "}, Created: t0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertBookmark(ctx, domain.Bookmark{URL: "https://go.dev", Created: t0}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dup: %v", err)
	}
	b2, _ := s.InsertBookmark(ctx, domain.Bookmark{URL: "https://x.test", Pinned: true, Created: t0})
	got, err := s.GetBookmark(ctx, b.ID)
	if err != nil || !cmp.Equal([]string{"lang", "dev"}, got.Tags) || got.Title != "Go" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if byURL, _ := s.GetBookmarkByURL(ctx, "https://x.test"); byURL.ID != b2.ID {
		t.Fatal("by url")
	}
	list, _ := s.ListBookmarks(ctx)
	if len(list) != 2 || list[0].ID != b2.ID {
		t.Fatalf("pinned first: %+v", list)
	}
	b.URL = "https://x.test"
	if err := s.UpdateBookmark(ctx, b); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("update dup: %v", err)
	}
	b.URL, b.Tags = "https://go.dev/doc", nil
	if err := s.UpdateBookmark(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetBookmark(ctx, b.ID); got.URL != "https://go.dev/doc" || len(got.Tags) != 0 {
		t.Fatalf("updated: %+v", got)
	}
	if err := s.DeleteBookmark(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		s.DeleteBookmark(ctx, b.ID),
		s.UpdateBookmark(ctx, domain.Bookmark{ID: 999, URL: "u"}),
		func() error { _, err := s.GetBookmark(ctx, b.ID); return err }(),
		func() error { _, err := s.GetBookmarkByURL(ctx, "nope"); return err }(),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("want not found: %v", err)
		}
	}
}

func TestItems(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	it := domain.Item{Date: "2026-11-16", Project: "acme", Type: domain.ItemAction, Summary: "do it", Fingerprint: "fp1"}
	got, created, err := s.InsertItem(ctx, it)
	if err != nil || !created || got.Status != "open" {
		t.Fatalf("insert: %+v %v %v", got, created, err)
	}
	dup, created, err := s.InsertItem(ctx, domain.Item{Summary: "other", Type: domain.ItemNote, Fingerprint: "fp1"})
	if err != nil || created || dup.ID != got.ID || dup.Summary != "do it" {
		t.Fatalf("duplicate: %+v %v %v", dup, created, err)
	}
	_, _, _ = s.InsertItem(ctx, domain.Item{Date: "2026-11-17", Type: domain.ItemRisk, Summary: "r", Fingerprint: "fp2"})
	if err := s.SetItemStatus(ctx, got.ID, "done"); err != nil {
		t.Fatal(err)
	}
	byDate, _ := s.ListItems(ctx, "2026-11-16", "")
	open, _ := s.ListItems(ctx, "", "open")
	all, _ := s.ListItems(ctx, "", "")
	if len(byDate) != 1 || len(open) != 1 || open[0].Fingerprint != "fp2" || len(all) != 2 {
		t.Fatalf("lists: %d %d %d", len(byDate), len(open), len(all))
	}
	if g, err := s.GetItem(ctx, got.ID); err != nil || g.Status != "done" {
		t.Fatalf("get: %+v %v", g, err)
	}
	if _, err := s.GetItem(ctx, 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.SetItemStatus(ctx, 99, "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.InsertItem(ctx, domain.Item{Type: "bogus", Summary: "x", Fingerprint: "fp3"}); err == nil {
		t.Fatal("CHECK on type not enforced")
	}
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	s, backups := open(t)
	dst := filepath.Join(backups, "b.db")
	if err := s.Backup(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, dst); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("existing dst: %v", err)
	}
	b, err := Open(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if v, _ := b.SchemaVersion(ctx); v != 1 {
		t.Fatalf("backup version %d", v)
	}
	if !strings.HasSuffix(s.Path(), "stkr.db") {
		t.Fatal(s.Path())
	}
}

func TestOpenFails(t *testing.T) {
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "missing", "dir", "x.db")); err == nil {
		t.Fatal("want error for missing directory")
	}
}

func TestParseTS(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.FixedZone("x", 3600))
	s := FormatTS(ts)
	if s != "2026-01-02T02:04:05.006Z" {
		t.Fatalf("format: %s", s)
	}
	if got, err := ParseTS(s); err != nil || !got.Equal(ts) {
		t.Fatalf("parse: %v %v", got, err)
	}
}
