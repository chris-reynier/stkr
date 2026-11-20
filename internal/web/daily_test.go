package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/httpapi"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/testutil"
)

func newDailyFixture(t *testing.T) (*fixture, *testutil.FakeClipboard, *testutil.FakeWindows) {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(utc("2026-11-16 09:00"))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	cfg.OneDriveDir = filepath.Join(dir, "od")
	cfg.AgentWindowTitle = "Chat"
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	clip := &testutil.FakeClipboard{}
	win := &testutil.FakeWindows{Present: map[string]bool{"Chat": true}}
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), Logger: lg, Clipboard: clip, Windows: win})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := httpapi.New(svc, httpapi.Options{Port: port, Logger: lg})
	if _, err := Mount(srv.Mux(), svc, Options{Logger: lg}); err != nil {
		t.Fatal(err)
	}
	return &fixture{h: srv.Handler(), svc: svc, clk: clk}, clip, win
}

// follow returns the page at target as the browser sees it after w, with the
// flash cookie of w.
func (f *fixture) follow(w *httptest.ResponseRecorder, target string) string {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = "127.0.0.1:7878"
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec.Body.String()
}

func TestTodayDailyLoop(t *testing.T) {
	f, clip, win := newDailyFixture(t)
	page := body(t, f.get("/ui/today"), 200)
	mustContain(t, page, `action="/ui/day/start"`, `action="/ui/close"`, `action="/ui/import"`, `name="text"`, `name="paste"`, "No priorities saved", "No items captured")

	// Start Day copies the prompt and plans.
	w := f.post("/ui/day/start", url.Values{"next": {"/ui/today"}})
	if w.Code != 303 || w.Header().Get("Location") != "/ui/today" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Location"))
	}
	if !strings.Contains(clip.Text(), "stkr-brief v1") {
		t.Error("prompt not copied")
	}
	mustContain(t, f.follow(w, "/ui/today"), `class="flash ok"`, "Start Day prompt copied")

	// Import an answer; the page then shows top 3 and items, escaped.
	answer := "```stkr-brief v1\ntop | 1 | Ship <b>P05</b>\nitem | action | other | Write <script>docs</script> | 2026-11-20\n```"
	w = f.post("/ui/import", url.Values{"next": {"/ui/today#import"}, "text": {answer}})
	if w.Code != 303 || w.Header().Get("Location") != "/ui/today#import" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Location"))
	}
	page = f.follow(w, "/ui/today")
	mustContain(t, page, "Imported: 1 new item(s), 1 of the top 3 saved.", "Ship &lt;b&gt;P05&lt;/b&gt;", "Write &lt;script&gt;docs&lt;/script&gt;", "2026-11-20", "brief saved")
	if strings.Contains(page, "<script>docs") || strings.Contains(page, "<b>P05") {
		t.Error("imported text must be escaped")
	}

	// Garbage: nothing imported, an error is flashed and the user lands at the import box.
	w = f.post("/ui/import", url.Values{"next": {"/ui/today#import"}, "text": {"hello"}})
	if w.Code != 303 || w.Header().Get("Location") != "/ui/today#import" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Location"))
	}
	mustContain(t, f.follow(w, "/ui/today"), `class="flash error"`, "nothing was imported")

	// Close Day with paste.
	f.clk.Set(utc("2026-11-16 16:50"))
	w = f.post("/ui/close", url.Values{"next": {"/ui/today"}, "paste": {"1"}})
	if len(win.Pastes) != 1 || !strings.Contains(clip.Text(), "stkr-close v1") {
		t.Fatalf("pastes=%v", win.Pastes)
	}
	page = f.follow(w, "/ui/today")
	mustContain(t, page, "copied and pasted")
	// planning (from Start Day) is tracked, so the close is due now.
	mustContain(t, page, "past close time")

	// Paste to a missing window shows the error.
	win.Present["Chat"] = false
	w = f.post("/ui/close", url.Values{"next": {"/ui/today"}, "paste": {"1"}})
	mustContain(t, f.follow(w, "/ui/today"), `class="flash error"`, "no window")

	// Saving the close removes the notice.
	f.post("/ui/import", url.Values{"next": {"/ui/today"}, "text": {"```stkr-close v1\nitem | note | other | wrapped up |\n```"}})
	if p := body(t, f.get("/ui/today"), 200); strings.Contains(p, "past close time") || !strings.Contains(p, "close saved") {
		t.Error("close notice should be gone and the flags shown")
	}
	// Old days show their own plan and no import box.
	old := body(t, f.get("/ui/today?d=2026-11-15"), 200)
	mustContain(t, old, "No priorities saved", "No items captured")
	if strings.Contains(old, `id="import"`) || strings.Contains(old, `action="/ui/day/start"`) {
		t.Error("actions belong to today only")
	}
}

func TestDailyActionsRejectForeignOrigin(t *testing.T) {
	f, clip, _ := newDailyFixture(t)
	w := f.post("/ui/day/start", url.Values{"next": {"/ui/today"}}, "Origin", "https://evil.example")
	if w.Code != 403 || clip.Text() != "" {
		t.Fatalf("%d clip=%q", w.Code, clip.Text())
	}
}
