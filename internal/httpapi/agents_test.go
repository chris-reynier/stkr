package httpapi

import (
	"bytes"
	"context"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

type fakeWin struct{ titles []string }

func (w *fakeWin) Focus(_ context.Context, title string, _ time.Duration) error {
	w.titles = append(w.titles, title)
	return nil
}

type fakeLauncher struct{ opened []string }

func (l *fakeLauncher) OpenWorkspace(_ context.Context, p string) error {
	l.opened = append(l.opened, p)
	return nil
}

func newAgentFixture(t testing.TB) (*fixture, *fakeWin) {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	logs := &bytes.Buffer{}
	lg := slog.New(slog.NewTextHandler(logs, nil))
	win := &fakeWin{}
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), Logger: lg, Windows: win, Launcher: &fakeLauncher{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := New(svc, Options{Port: testPort, Logger: lg})
	return &fixture{srv: srv, h: srv.Handler(), clk: clk, svc: svc, logs: logs}, win
}

func (f *fixture) post(target, body string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", target, strings.NewReader(body))
	r.Host = "127.0.0.1:7878"
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

type instanceBody struct {
	Instance *struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Label string `json:"label"`
	} `json:"instance"`
	Switched bool   `json:"switched"`
	Result   string `json:"result"`
}

func TestAgentEventsAndNext(t *testing.T) {
	f, win := newAgentFixture(t)
	if w := f.do("GET", "/api/agents/next"); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"instance":null}` {
		t.Fatalf("empty next = %d %s", w.Code, w.Body)
	}
	for _, b := range []string{
		`{"instance":"a","event":"state","state":"working","slot":1,"workspace":"/w/a"}`,
		`{"instance":"b","event":"state","state":"error","slot":2,"workspace":"/w/b","summary":"boom"}`,
		`{"instance":"c","event":"state","state":"done","slot":3,"unknown_field":1}`,
	} {
		if w := f.post("/api/agents/event", b); w.Code != 200 {
			t.Fatalf("event %s = %d %s", b, w.Code, w.Body)
		}
	}
	n := decodeJSON[instanceBody](t, f.do("GET", "/api/agents/next"))
	if n.Instance == nil || n.Instance.ID != "b" || n.Instance.Label != "b" {
		t.Fatalf("next = %+v", n.Instance)
	}
	list := decodeJSON[struct {
		Instances []struct{ ID string } `json:"instances"`
		Counts    struct{ Error, Done, Working int }
		Unknown   []struct {
			Workspace string `json:"workspace"`
		} `json:"unknown_workspaces"`
	}](t, f.do("GET", "/api/agents"))
	if len(list.Instances) != 3 || list.Instances[0].ID != "a" || list.Counts.Error != 1 || len(list.Unknown) != 2 {
		t.Fatalf("list = %+v", list)
	}

	g := decodeJSON[instanceBody](t, f.post("/api/agents/next/go", "", "X-STKR-Source", "deck"))
	if g.Result != "focused" || g.Instance == nil || g.Instance.ID != "b" || len(win.titles) != 1 || win.titles[0] != "b" {
		t.Fatalf("go = %+v titles=%v", g, win.titles)
	}

	if w := f.post("/api/agents/c/queue", ""); w.Code != 200 {
		t.Fatalf("queue = %d %s", w.Code, w.Body)
	}
	if w := f.post("/api/agents/nope/queue", ""); w.Code != 404 {
		t.Fatalf("queue unknown = %d", w.Code)
	}
	ack := decodeJSON[struct{ Acknowledged int }](t, f.post("/api/agents/ack-done", ""))
	if ack.Acknowledged != 1 {
		t.Fatalf("ack = %+v", ack)
	}
	if w := f.post("/api/agents/b/queue", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := f.post("/api/agents/event", `{"instance":"a","event":"closed"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	l2 := decodeJSON[struct{ Instances []struct{ ID string } }](t, f.do("GET", "/api/agents"))
	if len(l2.Instances) != 2 {
		t.Fatalf("after close = %+v", l2)
	}
}

func TestAgentGoNone(t *testing.T) {
	f, _ := newAgentFixture(t)
	g := decodeJSON[instanceBody](t, f.post("/api/agents/next/go", ""))
	if g.Result != "none" || g.Instance != nil {
		t.Fatalf("%+v", g)
	}
}

func TestAgentFocusedLogsDeckSwitch(t *testing.T) {
	f, _ := newAgentFixture(t)
	dir := "/w/m"
	if _, err := f.svc.UpdateContext(context.Background(), "meeting", service.ContextPatch{Workdir: &dir}); err != nil {
		t.Fatal(err)
	}
	r := decodeJSON[instanceBody](t, f.post("/api/agents/event", `{"instance":"a","event":"focused","workspace":"/w/m"}`))
	if !r.Switched {
		t.Fatalf("%+v", r)
	}
	now := decodeJSON[domain.NowStatus](t, f.do("GET", "/api/now"))
	if now.Context == nil || now.Context.ID != "meeting" {
		t.Fatalf("now = %+v", now)
	}
}

func TestAgentSync(t *testing.T) {
	f, _ := newAgentFixture(t)
	f.post("/api/agents/event", `{"instance":"old","event":"state","state":"idle"}`)
	w := f.post("/api/agents/sync", `{"instances":[{"instance":"x","state":"working","slot":4},{"instance":"y","state":"done"}]}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	l := decodeJSON[struct{ Instances []struct{ ID string } }](t, f.do("GET", "/api/agents"))
	if len(l.Instances) != 2 || l.Instances[0].ID != "x" {
		t.Fatalf("%+v", l)
	}
}

func TestAgentErrors(t *testing.T) {
	f, _ := newAgentFixture(t)
	cases := []struct {
		target, body string
		code         int
	}{
		{"/api/agents/event", `not json`, 400},
		{"/api/agents/event", `{"instance":"","event":"state","state":"idle"}`, 400},
		{"/api/agents/event", `{"instance":"a","event":"state","state":"bogus"}`, 400},
		{"/api/agents/event", `{"instance":"a","event":"state","state":"idle","slot":9}`, 400},
		{"/api/agents/sync", `{"instances":[{"instance":"a"}]}`, 400},
		{"/api/agents/event", `{"instance":"` + strings.Repeat("a", 2<<20) + `"}`, 413},
	}
	for _, c := range cases {
		if w := f.post(c.target, c.body); w.Code != c.code {
			t.Errorf("%s %.40s = %d, want %d (%s)", c.target, c.body, w.Code, c.code, w.Body)
		}
	}
	if w := f.do("GET", "/api/agents/event"); w.Code == 200 {
		t.Error("GET must not reach the event handler")
	}
	if w := f.post("/api/agents/event", `{}`, "Origin", "https://evil.example"); w.Code != 403 {
		t.Errorf("foreign origin = %d", w.Code)
	}
}

func TestAgentsImage(t *testing.T) {
	f, _ := newAgentFixture(t)
	px := func() [4]uint8 {
		w := f.do("GET", "/img/agents.png")
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("image = %d %s", w.Code, w.Header().Get("Content-Type"))
		}
		img, err := png.Decode(w.Body)
		if err != nil || img.Bounds().Dx() != 144 || img.Bounds().Dy() != 144 {
			t.Fatalf("decode: %v %v", err, img.Bounds())
		}
		r, g, b, a := img.At(2, 2).RGBA()
		return [4]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
	}
	idle := px()
	want := map[string][4]uint8{
		"working":         {37, 99, 235, 255},
		"done":            {21, 128, 61, 255},
		"action_required": {234, 88, 12, 255},
		"error":           {185, 28, 28, 255},
	}
	seen := map[[4]uint8]bool{idle: true}
	for _, st := range []string{"working", "done", "action_required", "error"} {
		f.post("/api/agents/event", `{"instance":"`+st+`","event":"state","state":"`+st+`"}`)
		got := px()
		if got != want[st] {
			t.Errorf("%s pixel = %v, want %v", st, got, want[st])
		}
		seen[got] = true
	}
	if len(seen) != 5 {
		t.Errorf("expected 5 distinct colours, got %d", len(seen))
	}
}

func TestAgentEventPerf(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	f, _ := newAgentFixture(t)
	states := []string{"working", "action_required", "done", "error"}
	d := make([]time.Duration, 200)
	for i := range d {
		start := time.Now()
		w := f.post("/api/agents/event", `{"instance":"p`+string(rune('a'+i%6))+`","event":"state","state":"`+states[i%4]+`","slot":`+string(rune('1'+i%6))+`,"workspace":"/w/x","summary":"working on a thing"}`)
		d[i] = time.Since(start)
		if w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body)
		}
	}
	if got := p95(d); got > 100*time.Millisecond {
		t.Errorf("agent event p95 = %v, budget 100ms", got)
	}
}
