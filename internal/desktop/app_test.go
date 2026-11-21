package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/server"
)

// fakeTray records what the app shows. Run calls Ready and blocks until Quit.
type fakeTray struct {
	mu      sync.Mutex
	icons   []IconState
	tips    []string
	notices []Notice
	quit    chan struct{}
	once    sync.Once
	ready   chan struct{}
}

func newFakeTray() *fakeTray {
	return &fakeTray{quit: make(chan struct{}), ready: make(chan struct{})}
}

func (f *fakeTray) Run(h Handler) error {
	h.Ready()
	close(f.ready)
	<-f.quit
	return nil
}

func (f *fakeTray) SetIcon(is IconState, tip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.icons = append(f.icons, is)
	f.tips = append(f.tips, tip)
}

func (f *fakeTray) Notify(n Notice) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notices = append(f.notices, n)
}

func (f *fakeTray) Quit() { f.once.Do(func() { close(f.quit) }) }

func (f *fakeTray) lastTip() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tips) == 0 {
		return ""
	}
	return f.tips[len(f.tips)-1]
}

func (f *fakeTray) noticeTexts() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, n := range f.notices {
		out = append(out, n.Text)
	}
	return strings.Join(out, "\n")
}

// waitFor polls cond for up to 10 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type opener struct {
	mu   sync.Mutex
	got  []string
	fail bool
}

func (o *opener) open(target string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.got = append(o.got, target)
	if o.fail {
		return errors.New("no browser")
	}
	return nil
}

func (o *opener) all() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.Join(o.got, " ")
}

// testHost hosts a real server on a temporary data folder.
func testHost(t *testing.T, label string) (*serviceHost, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := config.EnsureHome(home); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Update.Check = false
	h := &serviceHost{home: home, cfg: cfg, log: slog.New(slog.DiscardHandler), version: "0.2.0"}
	t.Cleanup(h.close)
	if label != "" {
		hd, err := h.start(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.svc.AddContext(context.Background(), domain.Context{ID: "secret-ctx", Label: label, Kind: domain.KindProject}); err != nil {
			t.Fatal(err)
		}
		_ = hd.Shutdown(context.Background())
	}
	return h, home
}

type runningApp struct {
	app  *App
	tray *fakeTray
	open *opener
	logs *bytes.Buffer
	done chan error
}

func (r *runningApp) quit(t *testing.T) {
	t.Helper()
	r.app.Select(ActQuit)
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("app did not quit")
	}
}

type syncBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func startApp(t *testing.T, port int, host HostFunc, home string, notifications bool) *runningApp {
	t.Helper()
	logs := &syncBuffer{}
	r := &runningApp{tray: newFakeTray(), open: &opener{}, logs: &logs.Buffer, done: make(chan error, 1)}
	r.app = NewApp(Options{
		Home: home, Version: "0.2.0", Port: port, Poll: 50 * time.Millisecond, Notifications: notifications,
		Tray: r.tray, Host: host, Open: r.open.open, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	})
	go func() { r.done <- r.app.Run() }()
	<-r.tray.ready
	return r
}

func TestAppHostsAndRunsActions(t *testing.T) {
	const label = "Sentinel Label Zebra"
	h, home := testHost(t, label)
	r := startApp(t, 0, h.start, home, true)
	waitFor(t, "status", func() bool { return r.tray.lastTip() == "STKR - Nothing running" })
	if r.app.Mode() != ModeHost {
		t.Fatalf("mode = %q", r.app.Mode())
	}

	r.app.Select(prefixSwitch + "secret-ctx")
	waitFor(t, "switch", func() bool { return strings.Contains(r.tray.noticeTexts(), "Switched to "+label) })
	waitFor(t, "tooltip", func() bool { return strings.HasPrefix(r.tray.lastTip(), "STKR - "+label) })
	menu := RenderMenu(r.app.Menu())
	if !strings.Contains(menu, "[x] "+label) {
		t.Fatalf("menu does not check the current context:\n%s", menu)
	}
	r.app.Select(prefixSwitch + "secret-ctx")
	waitFor(t, "debounced switch", func() bool { return strings.Contains(r.tray.noticeTexts(), "Already on "+label) })
	r.app.Select(prefixSwitch + "nope")
	waitFor(t, "unknown context", func() bool { return strings.Contains(r.tray.noticeTexts(), `unknown context "nope"`) })
	r.app.Select(ActStop)
	waitFor(t, "stop", func() bool { return strings.Contains(r.tray.noticeTexts(), "Stopped") })
	r.app.Select(ActStop)
	waitFor(t, "stop again", func() bool { return strings.Contains(r.tray.noticeTexts(), "Nothing was running") })

	r.app.Select(ActAbout)
	waitFor(t, "about", func() bool { return strings.Contains(r.tray.noticeTexts(), "STKR 0.2.0, running the server on port") })

	r.app.Activate()
	r.app.Select(prefixOpen + "/ui/week")
	r.app.Select(ActDataFolder)
	r.app.Select(ActLogsFolder)
	waitFor(t, "opens", func() bool { r.open.mu.Lock(); defer r.open.mu.Unlock(); return len(r.open.got) >= 4 })
	got := r.open.all()
	for _, want := range []string{"/ui/today", "/ui/week", home, filepath.Join(home, "logs")} {
		if !strings.Contains(got, want) {
			t.Errorf("opened %q, missing %q", got, want)
		}
	}
	r.app.Select(prefixOpen + "https://evil.example/")
	waitFor(t, "bad page", func() bool { return strings.Contains(r.tray.noticeTexts(), "unknown page") })
	r.app.Select("bogus")
	waitFor(t, "bogus", func() bool { return strings.Contains(r.tray.noticeTexts(), `unknown action "bogus"`) })

	r.quit(t)
	if strings.Contains(r.logs.String(), label) || strings.Contains(r.logs.String(), "secret-ctx") {
		t.Fatalf("desktop log contains a context label or id:\n%s", r.logs.String())
	}
	if !strings.Contains(r.logs.String(), `"action":"switch"`) {
		t.Fatalf("actions are not logged:\n%s", r.logs.String())
	}
}

func TestAppAttachesToRunningServer(t *testing.T) {
	h, home := testHost(t, "")
	hd, err := h.start(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	port := hd.Port()
	hostCalls := 0
	var mu sync.Mutex
	host := func(ctx context.Context, p int) (Hosted, error) {
		mu.Lock()
		hostCalls++
		mu.Unlock()
		return h.start(ctx, p)
	}
	r := startApp(t, port, host, home, false)
	waitFor(t, "attach", func() bool { return r.app.Mode() == ModeAttach && r.tray.lastTip() == "STKR - Nothing running" })
	r.app.Select(ActAbout)
	waitFor(t, "about", func() bool {
		return strings.Contains(r.tray.noticeTexts(), "attached to `stkr serve` on port "+strconv.Itoa(port))
	})

	// The attached server stops: the app takes over and hosts on the same port.
	if err := hd.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "take over", func() bool { return r.app.Mode() == ModeHost })
	mu.Lock()
	if hostCalls != 1 {
		t.Errorf("host calls = %d", hostCalls)
	}
	mu.Unlock()
	waitFor(t, "status after take over", func() bool { return r.tray.lastTip() == "STKR - Nothing running" })
	r.quit(t)
}

func TestAppPortUsedByAnotherProgram(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not stkr</html>"))
	}))
	defer other.Close()
	port := other.Listener.Addr().(*net.TCPAddr).Port
	h, home := testHost(t, "")
	r := startApp(t, port, h.start, home, true)
	waitFor(t, "fallback", func() bool { return r.app.Mode() == ModeHost })
	waitFor(t, "notice", func() bool {
		return strings.Contains(r.tray.noticeTexts(), "Port "+strconv.Itoa(port)+" is in use, change `port` in Settings")
	})
	waitFor(t, "settings opened", func() bool { return strings.Contains(r.open.all(), "/ui/settings") })
	if strings.Contains(r.open.all(), ":"+strconv.Itoa(port)+"/") {
		t.Fatalf("settings opened on the busy port: %s", r.open.all())
	}
	r.quit(t)
}

func TestAppListenErrorFallsBack(t *testing.T) {
	h, home := testHost(t, "")
	var ports []int
	var mu sync.Mutex
	host := func(ctx context.Context, p int) (Hosted, error) {
		mu.Lock()
		ports = append(ports, p)
		mu.Unlock()
		if p != 0 {
			return nil, &server.ListenError{Port: p, Err: errors.New("in use")}
		}
		return h.start(ctx, 0)
	}
	// Port 1 does not answer HTTP, so the app tries to host on it first.
	r := startApp(t, 1, host, home, true)
	waitFor(t, "fallback", func() bool { return r.app.Mode() == ModeHost })
	mu.Lock()
	if len(ports) != 2 || ports[0] != 1 || ports[1] != 0 {
		t.Errorf("host ports = %v", ports)
	}
	mu.Unlock()
	r.quit(t)
}

func TestAppHostFailure(t *testing.T) {
	host := func(context.Context, int) (Hosted, error) { return nil, errors.New("database is locked") }
	r := startApp(t, 1, host, t.TempDir(), true)
	waitFor(t, "error notice", func() bool { return strings.Contains(r.tray.noticeTexts(), "database is locked") })
	if r.tray.lastTip() != "STKR - server not reachable" {
		t.Fatalf("tip = %q", r.tray.lastTip())
	}
	r.app.Select(ActStartDay)
	waitFor(t, "offline action", func() bool { return strings.Contains(r.tray.noticeTexts(), "not reachable") })
	if menu := RenderMenu(r.app.Menu()); !strings.Contains(menu, "STKR server not reachable") {
		t.Fatalf("menu:\n%s", menu)
	}
	r.quit(t)
}

// fakeServer answers /healthz and /api/status with a canned status and the
// POST routes with canned bodies.
type fakeServer struct {
	mu     sync.Mutex
	status Status
	posts  map[string]fakeAnswer
	srv    *httptest.Server
}

type fakeAnswer struct {
	code int
	body string
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{posts: map[string]fakeAnswer{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/healthz":
			_, _ = w.Write([]byte(`{"ok":true,"version":"0.2.0"}`))
		case r.URL.Path == "/api/status":
			_ = json.NewEncoder(w).Encode(f.status)
		case r.Method == http.MethodPost:
			a, ok := f.posts[r.URL.Path]
			if !ok {
				a = fakeAnswer{404, `{"error":"not_found","message":"no such route"}`}
			}
			w.WriteHeader(a.code)
			_, _ = w.Write([]byte(a.body))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) port() int { return f.srv.Listener.Addr().(*net.TCPAddr).Port }

func (f *fakeServer) set(fn func(*Status)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.status)
}

func noHost(context.Context, int) (Hosted, error) { return nil, errors.New("must attach") }

func TestAppNotifications(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		f := newFakeServer(t)
		r := startApp(t, f.port(), noHost, t.TempDir(), enabled)
		waitFor(t, "attach", func() bool { return r.app.Mode() == ModeAttach })
		f.set(func(s *Status) { s.CloseDue = true; s.AgentsWaiting = 1 })
		waitFor(t, "close due tooltip", func() bool { return strings.HasSuffix(r.tray.lastTip(), "close due") })
		time.Sleep(150 * time.Millisecond) // a few more polls: no repeats
		got := r.tray.noticeTexts()
		if enabled && (strings.Count(got, "close-of-day is due") != 1 || strings.Count(got, "agent is waiting") != 1) {
			t.Fatalf("notices with notifications on:\n%s", got)
		}
		if !enabled && got != "" {
			t.Fatalf("notices with notifications off:\n%s", got)
		}
		r.quit(t)
	}
}

func TestAppDailyActions(t *testing.T) {
	f := newFakeServer(t)
	f.posts["/api/day/start"] = fakeAnswer{200, `{"ok":true,"chars":900,"clipboard":true,"warnings":["digest cut"]}`}
	f.posts["/api/close/prompt"] = fakeAnswer{200, `{"ok":true,"chars":900,"clipboard":true}`}
	f.posts["/api/import/clipboard"] = fakeAnswer{200, `{"ok":true,"items_created":2,"items_done":1,"tops_saved":3,"warnings":[]}`}
	f.posts["/api/bookmark/from-clipboard"] = fakeAnswer{201, `{"bookmark":{"title":"Example page"}}`}
	r := startApp(t, f.port(), noHost, t.TempDir(), true)
	waitFor(t, "attach", func() bool { return r.app.Mode() == ModeAttach })
	for _, a := range []string{ActStartDay, ActCloseDay, ActImportClip, ActBookmarkClip} {
		r.app.Select(a)
	}
	for _, want := range []string{
		"Start Day prompt copied to the clipboard. Paste it into your AI assistant. (1 warning(s), see Today)",
		"Close Day prompt copied",
		"Imported: 2 new item(s), 1 done, 3 top priorities.",
		"Bookmark saved. Example page",
	} {
		waitFor(t, want, func() bool { return strings.Contains(r.tray.noticeTexts(), want) })
	}
	f.mu.Lock()
	f.posts["/api/import/clipboard"] = fakeAnswer{422, `{"ok":false,"error":"no_valid_block","message":"no stkr-brief or stkr-close block with valid lines was found; nothing was imported"}`}
	f.posts["/api/bookmark/from-clipboard"] = fakeAnswer{503, `{"error":"unavailable","message":"no clipboard"}`}
	f.mu.Unlock()
	r.app.Select(ActImportClip)
	r.app.Select(ActBookmarkClip)
	waitFor(t, "import error", func() bool { return strings.Contains(r.tray.noticeTexts(), "nothing was imported") })
	waitFor(t, "clipboard error", func() bool { return strings.Contains(r.tray.noticeTexts(), "no clipboard") })
	r.open.mu.Lock()
	r.open.fail = true
	r.open.mu.Unlock()
	r.app.Select(prefixOpen + pageToday)
	waitFor(t, "open error", func() bool { return strings.Contains(r.tray.noticeTexts(), "cannot open") })
	r.quit(t)
	if !strings.Contains(r.logs.String(), "http 422 no_valid_block") {
		t.Fatalf("error kind not logged:\n%s", r.logs.String())
	}
}

func TestDecide(t *testing.T) {
	f := newFakeServer(t)
	if m, busy := Decide(context.Background(), f.port()); m != ModeAttach || busy {
		t.Fatalf("stkr: %q %v", m, busy)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) }))
	defer other.Close()
	if m, busy := Decide(context.Background(), other.Listener.Addr().(*net.TCPAddr).Port); m != ModeHost || !busy {
		t.Fatalf("other: %q %v", m, busy)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if m, busy := Decide(context.Background(), free); m != ModeHost || busy {
		t.Fatalf("free: %q %v", m, busy)
	}
	// Something that accepts but never answers is "in use" after the timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(2 * ProbeTimeout) }))
	defer slow.Close()
	start := time.Now()
	if m, _ := Decide(context.Background(), slow.Listener.Addr().(*net.TCPAddr).Port); m != ModeHost {
		t.Fatalf("slow: %q", m)
	}
	if time.Since(start) > 3*ProbeTimeout {
		t.Fatalf("probe took %v", time.Since(start))
	}
}
