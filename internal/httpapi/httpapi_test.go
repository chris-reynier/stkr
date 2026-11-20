package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

const testPort = 7878

type fixture struct {
	srv  *Server
	h    http.Handler
	clk  *clock.Fake
	svc  *service.Service
	logs *bytes.Buffer
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	logs := &bytes.Buffer{}
	lg := slog.New(slog.NewTextHandler(logs, nil))
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), Logger: lg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := New(svc, Options{Port: testPort, AllowedOrigins: []string{"https://deck.example"}, Logger: lg})
	return &fixture{srv: srv, h: srv.Handler(), clk: clk, svc: svc, logs: logs}
}

func (f *fixture) do(method, target string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "127.0.0.1:7878"
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			r.Host = hdr[i+1]
			continue
		}
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q body=%s", ct, w.Body)
	}
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad json %q: %v", w.Body, err)
	}
	return v
}

func TestHealthz(t *testing.T) {
	f := newFixture(t)
	w := f.do("GET", "/healthz")
	got := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || got["ok"] != true || got["version"] == "" {
		t.Fatalf("%d %v", w.Code, got)
	}
}

func TestHostGuard(t *testing.T) {
	f := newFixture(t)
	for host, want := range map[string]int{
		"127.0.0.1:7878": 200,
		"localhost:7878": 200,
		"evil.com":       421,
		"127.0.0.1:9999": 421,
		"127.0.0.1":      421,
		"evil.com:7878":  421,
		"":               421,
	} {
		w := f.do("GET", "/api/now", "Host", host)
		if w.Code != want {
			t.Errorf("host %q: code %d, want %d", host, w.Code, want)
		}
		if want != 200 {
			if e := decodeJSON[errBody](t, w); e.Error != "invalid_host" {
				t.Errorf("host %q: %+v", host, e)
			}
		}
	}
}

func TestOriginGuard(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name, method, origin string
		want                 int
	}{
		{"GET no origin", "GET", "", 200},
		{"GET foreign origin ok", "GET", "http://evil.com", 200},
		{"POST no origin", "POST", "", 200},
		{"POST loopback ip", "POST", "http://127.0.0.1:7878", 200},
		{"POST localhost", "POST", "http://localhost:7878", 200},
		{"POST configured origin", "POST", "https://deck.example", 200},
		{"POST evil", "POST", "http://evil.com", 403},
		{"POST wrong port", "POST", "http://127.0.0.1:1", 403},
		{"POST null origin", "POST", "null", 403},
	}
	for _, tt := range tests {
		path := "/api/now"
		if tt.method == "POST" {
			path = "/api/stop"
		}
		var hdr []string
		if tt.origin != "" {
			hdr = []string{"Origin", tt.origin}
		}
		w := f.do(tt.method, path, hdr...)
		if w.Code != tt.want {
			t.Errorf("%s: code %d, want %d", tt.name, w.Code, tt.want)
		}
		if tt.want == 403 {
			if e := decodeJSON[errBody](t, w); e.Error != "forbidden_origin" {
				t.Errorf("%s: %+v", tt.name, e)
			}
		}
	}
	// A rejected switch must not change state.
	w := f.do("POST", "/api/switch/other", "Origin", "http://evil.com")
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	st, _ := f.svc.Now(context.Background())
	if st.Tracking {
		t.Fatal("blocked request changed state")
	}
}

func TestBodyLimit(t *testing.T) {
	f := newFixture(t)
	f.srv.Mux().HandleFunc("POST /api/_echo", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 2<<20)
		n := 0
		var err error
		for err == nil {
			var m int
			m, err = r.Body.Read(buf[n:])
			n += m
		}
		f.srv.writeErr(w, err)
	})
	r := httptest.NewRequest("POST", "/api/_echo", bytes.NewReader(make([]byte, 2<<20)))
	r.Host = "127.0.0.1:7878"
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code %d", w.Code)
	}
}

func TestListenerIsLoopback(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a := ln.Addr().(*net.TCPAddr)
	if !a.IP.Equal(net.IPv4(127, 0, 0, 1)) || !a.IP.IsLoopback() {
		t.Fatalf("listening on %v", a)
	}
}

func TestPortInUse(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := Listen(ln.Addr().(*net.TCPAddr).Port); err == nil {
		t.Fatal("expected error for busy port")
	}
}

func TestServeAndShutdown(t *testing.T) {
	f := newFixture(t)
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := New(f.svc, Options{Port: port})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestSwitchStopNow(t *testing.T) {
	f := newFixture(t)

	w := f.do("POST", "/api/switch/other?note=hello")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	res := decodeJSON[domain.SwitchResult](t, w)
	if !res.Created || res.Event.ContextID != "other" || res.Event.Source != domain.SourceAPI || res.Event.Note != "hello" {
		t.Fatalf("%+v", res)
	}

	// Double click is idempotent.
	w = f.do("POST", "/api/switch/other")
	if res := decodeJSON[domain.SwitchResult](t, w); res.Created {
		t.Fatal("duplicate switch created an event")
	}

	f.clk.Advance(30 * time.Minute)
	w = f.do("GET", "/api/now")
	st := decodeJSON[domain.NowStatus](t, w)
	if !st.Tracking || st.Context.ID != "other" || st.Elapsed.D() != 30*time.Minute {
		t.Fatalf("%+v", st)
	}

	f.clk.Advance(time.Minute)
	w = f.do("POST", "/api/switch/meeting", "X-STKR-Source", "deck")
	if res := decodeJSON[domain.SwitchResult](t, w); res.Event.Source != domain.SourceDeck {
		t.Fatalf("%+v", res)
	}
	f.clk.Advance(time.Minute)
	w = f.do("POST", "/api/switch/planning?src=deck")
	if res := decodeJSON[domain.SwitchResult](t, w); res.Event.Source != domain.SourceDeck {
		t.Fatalf("%+v", res)
	}

	f.clk.Advance(time.Minute)
	w = f.do("POST", "/api/stop")
	res = decodeJSON[domain.SwitchResult](t, w)
	if w.Code != 200 || !res.Created || res.Event.ContextID != domain.ContextOff {
		t.Fatalf("%d %+v", w.Code, res)
	}
	f.clk.Advance(time.Minute)
	if res := decodeJSON[domain.SwitchResult](t, f.do("POST", "/api/stop")); res.Created {
		t.Fatal("second stop created an event")
	}
	if st := decodeJSON[domain.NowStatus](t, f.do("GET", "/api/now")); st.Tracking {
		t.Fatalf("%+v", st)
	}
}

func TestErrorMapping(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: "old", Kind: domain.KindProject}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ParkContext(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, path string
		code               int
		errCode            string
	}{
		{"unknown context", "POST", "/api/switch/nope", 404, "not_found"},
		{"invalid slug", "POST", "/api/switch/Bad%20Id", 400, "invalid"},
		{"parked context", "POST", "/api/switch/old", 409, "conflict"},
		{"bad kind filter", "GET", "/api/contexts?kind=zzz", 400, "invalid"},
		{"unknown route", "GET", "/api/nothing", 404, "not_found"},
	}
	for _, tt := range tests {
		w := f.do(tt.method, tt.path)
		if w.Code != tt.code {
			t.Errorf("%s: code %d, want %d (%s)", tt.name, w.Code, tt.code, w.Body)
			continue
		}
		if e := decodeJSON[errBody](t, w); e.Error != tt.errCode || e.Message == "" {
			t.Errorf("%s: %+v", tt.name, e)
		}
	}
}

func TestWriteErrMapping(t *testing.T) {
	f := newFixture(t)
	for err, want := range map[error]int{
		domain.New(domain.ErrUnavailable, "no tool"): 503,
		context.DeadlineExceeded:                     500,
	} {
		w := httptest.NewRecorder()
		f.srv.writeErr(w, err)
		if w.Code != want {
			t.Errorf("%v: %d, want %d", err, w.Code, want)
		}
		if e := decodeJSON[errBody](t, w); want == 500 && e.Message != "internal error" {
			t.Errorf("internal error leaked: %+v", e)
		}
	}
}

func TestContexts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme", Label: "Acme", Kind: domain.KindProject}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "old", Kind: domain.KindProject}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ParkContext(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	ids := func(target string) []string {
		w := f.do("GET", target)
		var out struct {
			Contexts []domain.Context `json:"contexts"`
		}
		if w.Code != 200 {
			t.Fatalf("%s: %d", target, w.Code)
		}
		out = decodeJSON[struct {
			Contexts []domain.Context `json:"contexts"`
		}](t, w)
		var s []string
		for _, c := range out.Contexts {
			s = append(s, c.ID)
		}
		sort.Strings(s)
		return s
	}
	if got := strings.Join(ids("/api/contexts"), ","); got != "acme,break,meeting,off,other,planning" {
		t.Errorf("default: %s", got)
	}
	if got := strings.Join(ids("/api/contexts?parked=1"), ","); got != "acme,break,meeting,off,old,other,planning" {
		t.Errorf("parked: %s", got)
	}
	if got := strings.Join(ids("/api/contexts?kind=personal"), ","); got != "break" {
		t.Errorf("kind: %s", got)
	}
}

func TestNowImage(t *testing.T) {
	f := newFixture(t)
	check := func(label string, wantR, wantG, wantB uint8) {
		t.Helper()
		w := f.do("GET", "/img/now.png")
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %v", label, w.Code, w.Header())
		}
		img, err := png.Decode(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 144 || b.Dy() != 144 {
			t.Fatalf("%s: size %v", label, b)
		}
		r, g, b, _ := img.At(1, 1).RGBA()
		if uint8(r>>8) != wantR || uint8(g>>8) != wantG || uint8(b>>8) != wantB {
			t.Errorf("%s: pixel (1,1) = %d,%d,%d want %d,%d,%d", label, r>>8, g>>8, b>>8, wantR, wantG, wantB)
		}
	}
	check("off", colOff.R, colOff.G, colOff.B)
	f.do("POST", "/api/switch/other")
	check("project", colProject.R, colProject.G, colProject.B)
	f.clk.Advance(time.Second)
	f.do("POST", "/api/switch/meeting")
	check("mode", colMode.R, colMode.G, colMode.B)
	f.clk.Advance(time.Second)
	f.do("POST", "/api/switch/break")
	check("personal", colPersonal.R, colPersonal.G, colPersonal.B)
	f.clk.Advance(3 * time.Hour) // longer than idle_gap_minutes
	check("idle", colIdle.R, colIdle.G, colIdle.B)
}

func TestRequestLogHasNoQueryOrBody(t *testing.T) {
	f := newFixture(t)
	f.do("POST", "/api/switch/other?note=SECRET-NOTE-TEXT")
	out := f.logs.String()
	if !strings.Contains(out, "path=/api/switch/other") || !strings.Contains(out, "status=200") {
		t.Fatalf("request not logged: %s", out)
	}
	if strings.Contains(out, "SECRET-NOTE-TEXT") {
		t.Fatalf("note leaked into log: %s", out)
	}
}

func TestPanicRecovered(t *testing.T) {
	f := newFixture(t)
	f.srv.Mux().HandleFunc("GET /api/_panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	w := f.do("GET", "/api/_panic")
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	if e := decodeJSON[errBody](t, w); e.Error != "internal" {
		t.Fatalf("%+v", e)
	}
}

func TestGETDoesNotMutate(t *testing.T) {
	f := newFixture(t)
	// Mutations are POST-only: a GET must not switch (cross-site GET safety).
	w := f.do("GET", "/api/switch/other")
	if w.Code != http.StatusNotFound { // falls through to the JSON catch-all
		t.Fatalf("code %d", w.Code)
	}
	if st, _ := f.svc.Now(context.Background()); st.Tracking {
		t.Fatal("GET changed state")
	}
}
