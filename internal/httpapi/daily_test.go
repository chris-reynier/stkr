package httpapi

import (
	"bytes"
	"context"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/testutil"
)

type dailyFixture struct {
	*fixture
	clip *testutil.FakeClipboard
	win  *testutil.FakeWindows
	od   string
}

func newDailyFixture(t testing.TB) *dailyFixture {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	cfg.OneDriveDir = filepath.Join(dir, "od")
	cfg.AgentWindowTitle = "Chat"
	logs := &bytes.Buffer{}
	lg := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	clip := &testutil.FakeClipboard{}
	win := &testutil.FakeWindows{Present: map[string]bool{"Chat": true}}
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), Logger: lg, Clipboard: clip, Windows: win})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := New(svc, Options{Port: testPort, Logger: lg})
	return &dailyFixture{fixture: &fixture{srv: srv, h: srv.Handler(), clk: clk, svc: svc, logs: logs}, clip: clip, win: win, od: cfg.OneDriveDir}
}

// body posts a text/plain body.
func (f *fixture) body(target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(body))
	r.Host = "127.0.0.1:7878"
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func centerColor(t *testing.T, w *httptest.ResponseRecorder) [3]uint8 {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status %d type %q", w.Code, w.Header().Get("Content-Type"))
	}
	img, err := png.Decode(w.Body)
	if err != nil || img.Bounds().Dx() != 144 || img.Bounds().Dy() != 144 {
		t.Fatalf("bad png: %v", err)
	}
	r, g, b, _ := img.At(2, 2).RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

const fixtureBrief = "```stkr-brief v1\ntop | 1 | Ship P05\nitem | action | other | Write docs |\n```"

func TestDayStartRoute(t *testing.T) {
	f := newDailyFixture(t)
	w := f.do("POST", "/api/day/start?src=deck")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got := decodeJSON[struct {
		OK        bool `json:"ok"`
		Chars     int  `json:"chars"`
		Clipboard bool `json:"clipboard"`
	}](t, w)
	if !got.OK || got.Chars < 100 || !got.Clipboard || !strings.Contains(f.clip.Text(), "stkr-brief v1") {
		t.Fatalf("%+v clip=%q", got, f.clip.Text())
	}
	if strings.Contains(w.Body.String(), "stkr-brief") {
		t.Error("the response must not carry the prompt")
	}
	if _, err := os.Stat(filepath.Join(f.od, "_daily", "daily-digest.md")); err != nil {
		t.Errorf("digest not written: %v", err)
	}
	// clipboard=0 leaves the clipboard alone.
	f.clip.Set("keep")
	if w := f.do("POST", "/api/day/start?clipboard=0"); w.Code != 200 || f.clip.Text() != "keep" {
		t.Fatalf("%d clip=%q", w.Code, f.clip.Text())
	}
	// Clipboard failure is a 503 with a JSON error.
	f.clip.Err = domain.New(domain.ErrUnavailable, "no clipboard")
	w = f.do("POST", "/api/day/start")
	if w.Code != 503 || decodeJSON[errBody](t, w).Error != "unavailable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if f.do("GET", "/api/day/start").Code == 200 {
		t.Error("GET must not start the day")
	}
}

func TestClosePromptRoute(t *testing.T) {
	f := newDailyFixture(t)
	w := f.do("POST", "/api/close/prompt?paste=1&enter=1&src=deck")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got := decodeJSON[struct {
		OK     bool `json:"ok"`
		Pasted bool `json:"pasted"`
	}](t, w)
	if !got.OK || !got.Pasted || len(f.win.Pastes) != 1 || !f.win.Pastes[0].Enter || f.win.Pastes[0].Title != "Chat" {
		t.Fatalf("%+v %+v", got, f.win.Pastes)
	}
	if !strings.Contains(f.clip.Text(), "stkr-close v1") {
		t.Error("close prompt not on the clipboard")
	}
	// Window not found.
	w = f.do("POST", "/api/close/prompt?paste=1&window=Missing")
	if w.Code != 404 || decodeJSON[errBody](t, w).Error != "window_not_found" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestPasteRoute(t *testing.T) {
	f := newDailyFixture(t)
	if w := f.do("POST", "/api/paste?window=Chat&enter=1"); w.Code != 200 || len(f.win.Pastes) != 1 || !f.win.Pastes[0].Enter {
		t.Fatalf("%d %s %+v", w.Code, w.Body, f.win.Pastes)
	}
	if w := f.do("POST", "/api/paste"); w.Code != 200 || f.win.Pastes[1].Title != "Chat" {
		t.Fatalf("default title: %d %s", w.Code, w.Body)
	}
	w := f.do("POST", "/api/paste?window=Nowhere")
	e := decodeJSON[errBody](t, w)
	if w.Code != 404 || e.Error != "window_not_found" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	f.win.Err = domain.New(domain.ErrUnavailable, "paste is only supported on Windows")
	if w := f.do("POST", "/api/paste?window=Chat"); w.Code != 503 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g := newDailyFixture(t)
	g.svc.Config().AgentWindowTitle = ""
	if w := g.do("POST", "/api/paste"); w.Code != 400 || decodeJSON[errBody](t, w).Error != "invalid" {
		t.Fatalf("no title: %d %s", w.Code, w.Body)
	}
}

func TestImportRoutes(t *testing.T) {
	f := newDailyFixture(t)
	w := f.body("/api/import", fixtureBrief)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	res := decodeJSON[service.ImportResult](t, w)
	if !res.OK || res.State != "ok" || res.ItemsCreated != 1 || res.TopsSaved != 1 || res.Kind != "brief" {
		t.Fatalf("%+v", res)
	}
	// Same text again through the clipboard: no new items.
	f.clip.Set(fixtureBrief)
	res = decodeJSON[service.ImportResult](t, f.do("POST", "/api/import/clipboard"))
	if res.ItemsCreated != 0 || res.ItemsExisting != 1 {
		t.Fatalf("duplicate: %+v", res)
	}
	// Nothing valid: 422 with the redirect, and a red key.
	w = f.body("/api/import", "hello there")
	if w.Code != 422 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	fail := decodeJSON[struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Redirect string `json:"redirect"`
		State    string `json:"state"`
	}](t, w)
	if fail.OK || fail.Error != "no_valid_block" || fail.Redirect != "/ui/today#import" || fail.State != "fail" {
		t.Fatalf("%+v", fail)
	}
	if c := centerColor(t, f.do("GET", "/img/key/import.png")); c != [3]uint8{185, 28, 28} {
		t.Errorf("fail key color = %v", c)
	}
	f.clip.Err = domain.New(domain.ErrUnavailable, "no clipboard")
	if w := f.do("POST", "/api/import/clipboard"); w.Code != 503 {
		t.Fatalf("clipboard unavailable: %d %s", w.Code, w.Body)
	}
	// Too large for the body limit.
	if w := f.body("/api/import", strings.Repeat("x", 2<<20)); w.Code != 413 {
		t.Fatalf("oversize: %d %s", w.Code, w.Body)
	}
	// Foreign origins are refused like every other POST.
	r := httptest.NewRequest("POST", "/api/import", strings.NewReader(fixtureBrief))
	r.Host = "127.0.0.1:7878"
	r.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("origin guard: %d", rec.Code)
	}
}

func TestImportKeyImageStates(t *testing.T) {
	f := newDailyFixture(t)
	neutral := centerColor(t, f.do("GET", "/img/key/import.png"))
	if neutral != [3]uint8{75, 85, 99} {
		t.Errorf("neutral = %v", neutral)
	}
	f.body("/api/import", fixtureBrief)
	if c := centerColor(t, f.do("GET", "/img/key/import.png")); c != [3]uint8{21, 128, 61} {
		t.Errorf("ok = %v", c)
	}
	f.clk.Advance(11 * time.Second)
	if c := centerColor(t, f.do("GET", "/img/key/import.png")); c != neutral {
		t.Errorf("after 10 s = %v", c)
	}
	f.body("/api/import", "top | 2 | pipe only")
	if c := centerColor(t, f.do("GET", "/img/key/import.png")); c != [3]uint8{250, 204, 21} {
		t.Errorf("warn = %v", c)
	}
}

func TestCloseKeyImageBlinks(t *testing.T) {
	f := newDailyFixture(t)
	if _, err := f.svc.Switch(context.Background(), "planning", domain.SourceAPI, ""); err != nil {
		t.Fatal(err)
	}
	// 09:00 Monday, a working day (planning is tracked): not due yet.
	neutral := centerColor(t, f.do("GET", "/img/key/close.png"))
	if neutral != [3]uint8{75, 85, 99} {
		t.Errorf("before reminder = %v", neutral)
	}
	f.clk.Set(time.Date(2026, 11, 16, 16, 50, 0, 0, time.UTC))
	seen := map[[3]uint8]int{}
	for i := 0; i < 6; i++ {
		seen[centerColor(t, f.do("GET", "/img/key/close.png"))]++
		f.clk.Advance(time.Second)
	}
	if seen[[3]uint8{250, 204, 21}] != 3 || seen[[3]uint8{17, 24, 39}] != 3 || len(seen) != 2 {
		t.Errorf("blink colors = %v", seen)
	}
	// Saved: green and steady.
	f.body("/api/import", "```stkr-close v1\nitem | note | other | wrapped up |\n```")
	for i := 0; i < 2; i++ {
		if c := centerColor(t, f.do("GET", "/img/key/close.png")); c != [3]uint8{21, 128, 61} {
			t.Errorf("saved = %v", c)
		}
		f.clk.Advance(time.Second)
	}
}

func TestDailyLogsNeverContainContent(t *testing.T) {
	f := newDailyFixture(t)
	const secret = "TOP-SECRET-CLIPBOARD-TEXT"
	f.clip.Set("```stkr-brief v1\ntop | 1 | " + secret + "\n```")
	f.do("POST", "/api/import/clipboard")
	f.body("/api/import?x="+secret, "nothing "+secret)
	f.do("POST", "/api/paste?window=Chat")
	f.do("POST", "/api/day/start")
	f.do("POST", "/api/close/prompt?paste=1")
	if strings.Contains(f.logs.String(), secret) {
		t.Fatalf("content leaked into logs:\n%s", f.logs)
	}
}
