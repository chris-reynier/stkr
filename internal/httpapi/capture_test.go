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

	"github.com/chris-reynier/stkr/internal/capture"
	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/testutil"
)

type recStub struct{ rec bool }

func (r *recStub) Start(context.Context, string) error { r.rec = true; return nil }
func (r *recStub) Stop() error                         { r.rec = false; return nil }
func (r *recStub) Recording() bool                     { return r.rec }

type trStub struct{}

func (trStub) Transcribe(context.Context, string) (string, error) { return "words", nil }

// captureFixture is a fixture whose service uses fake recording tools.
func captureFixture(t *testing.T, rec capture.Recorder, mutate ...func(*config.Config)) (*fixture, *testutil.FakeClipboard) {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	cfg.Bookmarks.FetchTitle = false
	for _, m := range mutate {
		m(cfg)
	}
	logs := &bytes.Buffer{}
	lg := slog.New(slog.NewTextHandler(logs, nil))
	clip := &testutil.FakeClipboard{}
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg, service.Deps{
		BackupDir: filepath.Join(dir, "backups"), Logger: lg, AudioDir: filepath.Join(dir, "audio"),
		Recorder: rec, Transcriber: trStub{}, Clipboard: clip,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := New(svc, Options{Port: testPort, Logger: lg})
	return &fixture{srv: srv, h: srv.Handler(), clk: clk, svc: svc, logs: logs}, clip
}

func TestIdeaEndpoint(t *testing.T) {
	f, _ := captureFixture(t, &recStub{})
	start := time.Now()
	w := f.post("/api/idea", `{"text":"try htmx swaps"}`)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("idea took %v, want < 100 ms", d)
	}
	got := decodeJSON[service.IdeaResult](t, w)
	if w.Code != 201 || got.Idea == nil || got.Idea.RawText != "try htmx swaps" || got.Idea.Tag != "pro" || got.Idea.AIState != domain.AIPending {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := f.post("/api/idea", `{"text":"dentist","tag":"personal"}`); w.Code != 201 || !strings.Contains(w.Body.String(), `"personal"`) {
		t.Errorf("personal: %d %s", w.Code, w.Body)
	}

	// Meeting action: item with origin meeting; the same text again is a 200 duplicate.
	w = f.post("/api/idea", `{"text":"send the budget","type":"action"}`, "X-STKR-Source", "deck")
	got = decodeJSON[service.IdeaResult](t, w)
	if w.Code != 201 || got.Item == nil || got.Item.Origin != "meeting" || got.Item.Project != "other" || got.Item.Source != "deck" {
		t.Fatalf("action: %d %s", w.Code, w.Body)
	}
	if w = f.post("/api/idea", `{"text":"send the budget","type":"action"}`); w.Code != 200 || decodeJSON[service.IdeaResult](t, w).Created {
		t.Errorf("duplicate: %d %s", w.Code, w.Body)
	}

	for body, want := range map[string]int{
		`{"text":""}`:                400,
		`{"text":"x","type":"todo"}`: 400,
		`{"text":"x","tag":"work"}`:  400,
		`not json`:                   400,
		`{"text":"x","type":"action","tag":"personal"}`: 400,
	} {
		if w := f.post("/api/idea", body); w.Code != want || decodeJSON[errBody](t, w).Error != "invalid" {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := f.do("GET", "/api/idea"); w.Code == 200 {
		t.Error("GET must not create ideas")
	}
	if strings.Contains(f.logs.String(), "htmx") || strings.Contains(f.logs.String(), "dentist") || strings.Contains(f.logs.String(), "budget") {
		t.Error("idea text in logs")
	}
}

type writingRec struct {
	recStub
	path string
}

func (r *writingRec) Start(_ context.Context, p string) error {
	r.path = p
	return r.recStub.Start(context.Background(), p)
}
func (r *writingRec) Stop() error {
	_ = r.recStub.Stop()
	return os.WriteFile(r.path, []byte("RIFF"), 0o600)
}

func TestRecordToggleAndKeyImage(t *testing.T) {
	f, _ := captureFixture(t, &writingRec{})
	idle := keyColor(t, f, "/img/key/idea.png")
	w := f.post("/api/idea/record/toggle", "")
	if got := decodeJSON[service.RecordResult](t, w); w.Code != 200 || !got.Recording {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	rec := keyColor(t, f, "/img/key/idea.png")
	if rec == idle || rec[0] < 150 || rec[1] > 60 {
		t.Errorf("recording key = %v (idle %v), want red", rec, idle)
	}
	w = f.post("/api/idea/record/toggle", "")
	got := decodeJSON[service.RecordResult](t, w)
	if w.Code != 200 || got.Recording || got.Idea == nil || got.Idea.AudioPath == "" {
		t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
	if keyColor(t, f, "/img/key/idea.png") != idle {
		t.Error("key must return to idle")
	}
	if w := f.post("/api/idea/record/toggle?tag=bogus", ""); w.Code != 400 {
		t.Errorf("bad tag: %d", w.Code)
	}
}

func TestRecordMissingFFmpeg(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-ffmpeg")
	f, _ := captureFixture(t, capture.NewFFmpegRecorder(missing, "Mic"))
	w := f.post("/api/idea/record/toggle", "")
	e := decodeJSON[errBody](t, w)
	if w.Code != 503 || e.Error != "unavailable" || !strings.Contains(e.Message, "ffmpeg") {
		t.Errorf("%d %+v", w.Code, e)
	}
	h := decodeJSON[service.CaptureHealth](t, f.do("GET", "/api/capture/health"))
	_ = h
}

func keyColor(t *testing.T, f *fixture, path string) [3]uint8 {
	t.Helper()
	w := f.do("GET", path)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("%s: %d %s", path, w.Code, w.Header().Get("Content-Type"))
	}
	img, err := png.Decode(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 144 || b.Dy() != 144 {
		t.Fatalf("size = %v", b)
	}
	r, g, b, _ := img.At(2, 2).RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func TestIdeasInboxKey(t *testing.T) {
	f, _ := captureFixture(t, &recStub{})
	empty := keyColor(t, f, "/img/key/ideas-inbox.png")
	f.post("/api/idea", `{"text":"one"}`)
	f.post("/api/idea", `{"text":"private","tag":"personal"}`)
	one := keyColor(t, f, "/img/key/ideas-inbox.png")
	if one == empty {
		t.Error("key must change when untriaged pro ideas exist")
	}
	if n, _ := f.svc.IdeasInboxCount(context.Background()); n != 1 {
		t.Errorf("count = %d, want 1 (personal ideas are not counted)", n)
	}
}

func TestBookmarkEndpoints(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<title>Example Page</title>"))
	}))
	defer page.Close()
	f, clip := captureFixture(t, &recStub{}, func(c *config.Config) { c.Bookmarks.FetchTitle = true })

	w := f.post("/api/bookmark", `{"url":"`+page.URL+`","tags":["a, b"]}`)
	type resp struct {
		Bookmark domain.Bookmark `json:"bookmark"`
		errBody
	}
	got := decodeJSON[resp](t, w)
	if w.Code != 201 || got.Bookmark.Title != "Example Page" || strings.Join(got.Bookmark.Tags, ",") != "a,b" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Duplicate: 409 carrying the stored bookmark.
	w = f.post("/api/bookmark", `{"url":"`+page.URL+`"}`)
	dup := decodeJSON[resp](t, w)
	if w.Code != 409 || dup.Error != "conflict" || dup.Bookmark.ID != got.Bookmark.ID || dup.Bookmark.Title != "Example Page" {
		t.Errorf("duplicate: %d %s", w.Code, w.Body)
	}
	// From the clipboard.
	_ = clip.WriteText(context.Background(), "https://example.com/from-clipboard")
	w = f.post("/api/bookmark/from-clipboard?tags=x", "")
	got = decodeJSON[resp](t, w)
	if w.Code != 201 || got.Bookmark.URL != "https://example.com/from-clipboard" || len(got.Bookmark.Tags) != 1 {
		t.Errorf("clipboard: %d %s", w.Code, w.Body)
	}
	if w = f.post("/api/bookmark/from-clipboard", ""); w.Code != 409 {
		t.Errorf("clipboard duplicate: %d", w.Code)
	}
	_ = clip.WriteText(context.Background(), "just words")
	if w = f.post("/api/bookmark/from-clipboard", ""); w.Code != 400 {
		t.Errorf("clipboard text: %d %s", w.Code, w.Body)
	}
	clip.Err = domain.New(domain.ErrUnavailable, "no clipboard tool")
	if w = f.post("/api/bookmark/from-clipboard", ""); w.Code != 503 {
		t.Errorf("clipboard unavailable: %d", w.Code)
	}
	for _, body := range []string{`{"url":"not a url"}`, `{"url":"ftp://x.test/"}`, `{"url":""}`, `{`} {
		if w := f.post("/api/bookmark", body); w.Code != 400 {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
}

func TestCaptureHealthEndpoint(t *testing.T) {
	f, _ := captureFixture(t, &recStub{}, func(c *config.Config) {
		c.FFmpeg = filepath.Join(os.TempDir(), "no-such-ffmpeg")
		c.Whisper.Bin = filepath.Join(os.TempDir(), "no-such-whisper")
	})
	h := decodeJSON[map[string]any](t, f.do("GET", "/api/capture/health"))
	if h["ffmpeg"] != "missing" || h["whisper"] != "missing" || h["phone_inbox"] != "disabled" || h["recording"] != false {
		t.Errorf("health = %v", h)
	}
}
