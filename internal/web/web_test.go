package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/httpapi"
	"github.com/chris-reynier/stkr/internal/service"
)

const port = 7878

type fixture struct {
	h       http.Handler
	svc     *service.Service
	clk     *clock.Fake
	cfgPath string
}

func utc(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(utc("2026-11-16 09:00"))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), ConfigPath: cfgPath, Logger: lg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := httpapi.New(svc, httpapi.Options{Port: port, Logger: lg})
	if _, err := Mount(srv.Mux(), svc, Options{Logger: lg}); err != nil {
		t.Fatal(err)
	}
	return &fixture{h: srv.Handler(), svc: svc, clk: clk, cfgPath: cfgPath}
}

func (f *fixture) get(target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = "127.0.0.1:7878"
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *fixture) post(target string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	r.Host = "127.0.0.1:7878"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://127.0.0.1:7878")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

// at records a switch at the given clock time.
func (f *fixture) at(t *testing.T, ts, ctxID, note string) domain.Event {
	t.Helper()
	f.clk.Set(utc(ts))
	res, err := f.svc.Switch(context.Background(), ctxID, domain.SourceDeck, note)
	if err != nil {
		t.Fatal(err)
	}
	return res.Event
}

// seed: acme 09:00, planning 10:00, off 11:00 on 2026-11-16 (clock ends 12:00).
func (f *fixture) seed(t *testing.T) (a, p, off domain.Event) {
	t.Helper()
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: "acme", Label: "ACME", TempoTicket: "ACME-1"}); err != nil {
		t.Fatal(err)
	}
	a = f.at(t, "2026-11-16 09:00", "acme", "kickoff")
	p = f.at(t, "2026-11-16 10:00", "planning", "")
	off = f.at(t, "2026-11-16 11:00", "off", "")
	f.clk.Set(utc("2026-11-16 12:00"))
	return a, p, off
}

func body(t *testing.T, w *httptest.ResponseRecorder, want int) string {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, want, w.Body)
	}
	return w.Body.String()
}

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("page does not contain %q", sub)
		}
	}
}

func TestPages(t *testing.T) {
	f := newFixture(t)
	f.seed(t)
	today := body(t, f.get("/ui/today"), 200)
	mustContain(t, today, "Monday 2026-11-16", "ACME", "kickoff", "id=\"import\"", "id=\"top3\"", "id=\"items\"", "1h00m",
		"/ui/week", "/ui/settings", "id=\"update-banner\"", "href=\"/static/app.css\"", "/static/htmx.min.js")
	for _, absent := range []string{">Ideas<", ">Links<"} {
		if strings.Contains(today, absent) {
			t.Errorf("nav shows unregistered page %s", absent)
		}
	}
	week := body(t, f.get("/ui/week?w=2026-W47"), 200)
	mustContain(t, week, "2026-W47", "ACME-1", "Unmapped contexts", "Planning", "/api/week/2026-W47/tempo.csv", "Download Tempo CSV")
	settings := body(t, f.get("/ui/settings"), 200)
	mustContain(t, settings, "Add a context", "ACME-1", "claude_code.bin", "ai.provider", "ollama.url", "whisper.model", "onedrive_dir", "phone_inbox")
	if strings.Contains(settings, "allowed_origins") || strings.Contains(settings, "name=\"port\"") {
		t.Error("port and allowed_origins must not be editable")
	}
}

func TestDefaultToday(t *testing.T) {
	f := newFixture(t)
	body(t, f.get("/ui/today"), 200)
	for _, p := range []string{"/", "/ui/"} {
		w := f.get(p)
		if w.Code != 303 || w.Header().Get("Location") != "/ui/today" {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Location"))
		}
	}
	if w := f.get("/ui/today?d=garbage"); w.Code != 400 {
		t.Errorf("bad date: %d", w.Code)
	}
	if w := f.get("/ui/week?w=garbage"); w.Code != 400 {
		t.Errorf("bad week: %d", w.Code)
	}
	body(t, f.get("/ui/today?d=2026-11-15"), 200)
}

func TestSecurityHeadersAndStatic(t *testing.T) {
	f := newFixture(t)
	w := f.get("/ui/today")
	if csp := w.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "default-src 'self'") {
		t.Errorf("csp = %q", csp)
	}
	if strings.Contains(w.Body.String(), "<script>") || strings.Contains(w.Body.String(), " style=") {
		t.Error("page must not use inline script or style")
	}
	css := f.get("/static/app.css")
	if css.Code != 200 || !strings.Contains(css.Header().Get("Content-Type"), "css") || css.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("css: %d %v", css.Code, css.Header())
	}
	if js := f.get("/static/htmx.min.js"); js.Code != 200 || js.Body.Len() < 10000 {
		t.Errorf("htmx: %d %d", js.Code, js.Body.Len())
	}
	if l := f.get("/static/htmx.LICENSE.txt"); l.Code != 200 || !strings.Contains(l.Body.String(), "Zero-Clause BSD") {
		t.Errorf("license: %d", l.Code)
	}
	if bad := f.get("/ui/nope"); bad.Code != 404 {
		t.Errorf("unknown route: %d", bad.Code)
	}
	// Host and Origin guards still apply to UI routes.
	r := httptest.NewRequest(http.MethodGet, "/ui/today", nil)
	r.Host = "evil.example"
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("host guard: %d", rec.Code)
	}
	if w := f.post("/ui/stop", url.Values{}, "Origin", "https://evil.example"); w.Code != 403 {
		t.Errorf("origin guard: %d", w.Code)
	}
}

func eventsOf(t *testing.T, f *fixture) []domain.Event {
	t.Helper()
	rep, err := f.svc.Day(context.Background(), "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	return rep.Events
}

func TestEventActions(t *testing.T) {
	f := newFixture(t)
	a, p, off := f.seed(t)
	self := "/ui/today?d=2026-11-16"
	id := func(e domain.Event) string { return itoa(e.ID) }

	// Edit: time, context, note.
	w := f.post("/ui/event/"+id(p)+"/edit", url.Values{
		"next": {self}, "date": {"2026-11-16"}, "orig_time": {"10:00"}, "time": {"10:30"}, "context": {"meeting"}, "note": {"standup"},
	})
	if w.Code != 303 || w.Header().Get("Location") != self {
		t.Fatalf("edit: %d %q", w.Code, w.Header().Get("Location"))
	}
	e, _ := f.svc.GetEvent(context.Background(), p.ID)
	if e.ContextID != "meeting" || e.Note != "standup" || e.TS.Format("15:04") != "10:30" {
		t.Fatalf("event after edit: %+v", e)
	}
	// Note-only edit keeps the exact timestamp.
	exact := e.TS
	f.post("/ui/event/"+id(p)+"/edit", url.Values{"next": {self}, "date": {"2026-11-16"}, "orig_time": {"10:30"}, "time": {"10:30"}, "context": {"meeting"}, "note": {"x"}})
	if e2, _ := f.svc.GetEvent(context.Background(), p.ID); !e2.TS.Equal(exact) || e2.Note != "x" {
		t.Fatalf("note-only edit: %+v", e2)
	}

	// Split the acme block at 09:30.
	f.post("/ui/event/"+id(a)+"/split", url.Values{"next": {self}, "date": {"2026-11-16"}, "time": {"09:30"}})
	evs := eventsOf(t, f)
	if len(evs) != 4 || evs[1].TS.Format("15:04") != "09:30" || evs[1].ContextID != "acme" {
		t.Fatalf("after split: %+v", evs)
	}

	// Merge acme with its split half.
	f.post("/ui/event/"+id(a)+"/merge", url.Values{"next": {self}, "other": {id(evs[1])}})
	if evs = eventsOf(t, f); len(evs) != 3 {
		t.Fatalf("after merge: %+v", evs)
	}

	// Delete the off event.
	f.post("/ui/event/"+id(off)+"/delete", url.Values{"next": {self}})
	if evs = eventsOf(t, f); len(evs) != 2 {
		t.Fatalf("after delete: %+v", evs)
	}
}

func TestConfirmIdleSuspect(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: "acme"}); err != nil {
		t.Fatal(err)
	}
	a := f.at(t, "2026-11-16 09:00", "acme", "")
	f.clk.Set(utc("2026-11-16 15:00")) // 6 h block, longer than the idle gap
	page := body(t, f.get("/ui/today"), 200)
	mustContain(t, page, "idle-suspect, not counted", "Confirm worked")
	f.post("/ui/event/"+itoa(a.ID)+"/confirm", url.Values{"next": {"/ui/today"}, "confirmed": {"1"}})
	if e, _ := f.svc.GetEvent(context.Background(), a.ID); !e.Confirmed {
		t.Fatal("event not confirmed")
	}
	page = body(t, f.get("/ui/today"), 200)
	mustContain(t, page, "confirmed", "Unconfirm")
	f.post("/ui/event/"+itoa(a.ID)+"/confirm", url.Values{"next": {"/ui/today"}, "confirmed": {"0"}})
	if e, _ := f.svc.GetEvent(context.Background(), a.ID); e.Confirmed {
		t.Fatal("event still confirmed")
	}
}

func TestSwitchStopAndGaps(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: "acme", Label: "ACME"}); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(utc("2026-11-16 10:00"))
	page := body(t, f.get("/ui/today"), 200)
	mustContain(t, page, "Not tracking", "value=\"acme\"", "No events on this day")
	if w := f.post("/ui/switch", url.Values{"next": {"/ui/today"}, "ctx": {"acme"}}); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if st, _ := f.svc.Now(context.Background()); !st.Tracking || st.Context.ID != "acme" {
		t.Fatalf("now = %+v", st)
	}
	page = body(t, f.get("/ui/today"), 200)
	mustContain(t, page, "since 10:00", ">Stop<")
	f.clk.Advance(20 * time.Minute)
	f.post("/ui/stop", url.Values{"next": {"/ui/today"}})
	if st, _ := f.svc.Now(context.Background()); st.Tracking {
		t.Fatalf("still tracking: %+v", st)
	}
	evs := eventsOf(t, f)
	if len(evs) != 2 || evs[0].Source != domain.SourceUI {
		t.Fatalf("events: %+v", evs)
	}
}

func TestActionErrorsAreFlashed(t *testing.T) {
	f := newFixture(t)
	a, _, _ := f.seed(t)
	w := f.post("/ui/event/"+itoa(a.ID)+"/split", url.Values{"next": {"/ui/today"}, "date": {"2026-11-16"}, "time": {"13:00"}})
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	ck := w.Result().Cookies()
	if len(ck) == 0 || ck[0].Name != flashCookie || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies: %+v", ck)
	}
	r := httptest.NewRequest(http.MethodGet, "/ui/today", nil)
	r.Host = "127.0.0.1:7878"
	r.AddCookie(ck[0])
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	mustContain(t, rec.Body.String(), `class="flash error"`, "split time must be")
	if cleared := rec.Result().Cookies(); len(cleared) == 0 || cleared[0].MaxAge >= 0 {
		t.Errorf("flash not cleared: %+v", cleared)
	}
	// Plain bad input.
	for _, tc := range []struct{ path string }{{"/ui/event/abc/delete"}, {"/ui/event/999/delete"}, {"/ui/event/1/merge"}, {"/ui/event/1/split"}} {
		w := f.post(tc.path, url.Values{"next": {"/ui/today"}})
		if w.Code != 303 || len(w.Result().Cookies()) == 0 {
			t.Errorf("%s: %d %v", tc.path, w.Code, w.Result().Cookies())
		}
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/ui/week?w=1": "/ui/week?w=1", "": "/ui/today", "https://evil.example/": "/ui/today",
		"//evil.example": "/ui/today", "/ui/\\evil": "/ui/today", "/api/stop": "/ui/today", "/ui/x\r\nSet-Cookie: a=b": "/ui/today",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
	f := newFixture(t)
	w := f.post("/ui/stop", url.Values{"next": {"https://evil.example"}})
	if w.Header().Get("Location") != "/ui/today" {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
}

func TestSettingsContexts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	w := f.post("/ui/settings/ctx", url.Values{"next": {"/ui/settings"}, "id": {"beta"}, "label": {"Beta"}, "kind": {"project"}, "tempo_ticket": {"B-1"}})
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	c, err := f.svc.GetContext(ctx, "beta")
	if err != nil || c.TempoTicket != "B-1" || !c.Active {
		t.Fatalf("%v %+v", err, c)
	}
	f.post("/ui/settings/ctx/beta/edit", url.Values{"next": {"/ui/settings"}, "label": {"Beta 2"}, "kind": {"mode"}, "tempo_ticket": {"B-2"}, "notebook_url": {"https://n.example"}, "workdir": {"/w"}})
	c, _ = f.svc.GetContext(ctx, "beta")
	if c.Label != "Beta 2" || c.Kind != domain.KindMode || c.TempoTicket != "B-2" || c.NotebookURL != "https://n.example" || c.Workdir != "/w" {
		t.Fatalf("%+v", c)
	}
	f.post("/ui/settings/ctx/beta/park", url.Values{"next": {"/ui/settings"}})
	if c, _ = f.svc.GetContext(ctx, "beta"); c.Active {
		t.Fatal("not parked")
	}
	mustContain(t, body(t, f.get("/ui/settings"), 200), "parked", "Activate")
	f.post("/ui/settings/ctx/beta/activate", url.Values{"next": {"/ui/settings"}})
	if c, _ = f.svc.GetContext(ctx, "beta"); !c.Active {
		t.Fatal("not active")
	}
	// Errors: duplicate id, invalid id, off cannot be parked.
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/ui/settings/ctx", url.Values{"id": {"beta"}}},
		{"/ui/settings/ctx", url.Values{"id": {"Bad Id"}}},
		{"/ui/settings/ctx/off/park", url.Values{}},
	} {
		tc.form.Set("next", "/ui/settings")
		w := f.post(tc.path, tc.form)
		if w.Code != 303 || len(w.Result().Cookies()) == 0 || !strings.HasPrefix(w.Result().Cookies()[0].Value, "e") {
			t.Errorf("%s: want error flash, got %v", tc.path, w.Result().Cookies())
		}
	}
}

func TestSettingsConfigSave(t *testing.T) {
	f := newFixture(t)
	form := url.Values{
		"next": {"/ui/settings"}, "day_end": {"18:00"}, "rounding_hours": {"0.5"}, "ollama.model": {"m2"},
		"ollama.enabled": {"false", "true"}, "ai.provider": {"ollama"}, "onedrive_dir": {"/export"},
		"port": {"1"}, "allowed_origins": {"https://evil.example"}, // not editable through the form
	}
	if w := f.post("/ui/settings/config", form); w.Code != 303 {
		t.Fatal(w.Code)
	}
	cfg, _, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DayEnd != "18:00" || cfg.RoundingHours != 0.5 || cfg.Ollama.Model != "m2" || !cfg.Ollama.Enabled || cfg.AI.Provider != "ollama" || cfg.OneDriveDir != "/export" {
		t.Fatalf("saved config: %+v", cfg)
	}
	if cfg.Port != 7878 || len(cfg.AllowedOrigins) != 0 {
		t.Errorf("non-editable keys changed: port=%d origins=%v", cfg.Port, cfg.AllowedOrigins)
	}
	mustContain(t, body(t, f.get("/ui/settings"), 200), `value="18:00"`, `value="/export"`)
	// Invalid values are rejected and leave the file alone.
	w := f.post("/ui/settings/config", url.Values{"next": {"/ui/settings"}, "day_end": {"07:00"}})
	if ck := w.Result().Cookies(); len(ck) == 0 || !strings.HasPrefix(ck[0].Value, "e") {
		t.Fatalf("want error flash: %v", ck)
	}
	if cfg, _, _ = config.Load(f.cfgPath); cfg.DayEnd != "18:00" {
		t.Errorf("config changed: %s", cfg.DayEnd)
	}
}

func isSecretWhisperModel(key string) bool { return key == "whisper.model" }

func TestSettingsDoNotLeakSecrets(t *testing.T) {
	f := newFixture(t)
	cfg := config.Default()
	cfg.Whisper.Model = "hunter2-secret"
	secs := buildSections(cfg, isSecretWhisperModel)
	var found bool
	for _, s := range secs {
		for _, fl := range s.Fields {
			if fl.Key == "whisper.model" {
				found = true
				if !fl.Secret || !fl.Set || fl.Value != "" {
					t.Fatalf("secret field = %+v", fl)
				}
			}
		}
	}
	if !found {
		t.Fatal("field missing")
	}
	ui := muxUI(t, f)
	var buf bytes.Buffer
	if err := ui.tmpl["settings"].ExecuteTemplate(&buf, "layout", View{Title: "Settings", Data: settingsData{Sections: secs}}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatal("secret rendered in HTML")
	}
	mustContain(t, out, `type="password"`, "leave empty to keep")

	// An empty secret field keeps the stored value; a filled one replaces it.
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.PostForm = url.Values{"whisper.model": {""}, "whisper.bin": {"w"}}
	if p := formPairs(r, isSecretWhisperModel); len(p) != 1 || p["whisper.bin"] != "w" {
		t.Errorf("pairs = %v", p)
	}
	r.PostForm = url.Values{"whisper.model": {"new"}}
	if p := formPairs(r, isSecretWhisperModel); p["whisper.model"] != "new" {
		t.Errorf("pairs = %v", p)
	}
}

func muxUI(t *testing.T, f *fixture) *UI {
	t.Helper()
	ui, err := Mount(http.NewServeMux(), f.svc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return ui
}

func TestRegisterAddsPageToNav(t *testing.T) {
	regMu.Lock()
	saved := append([]Page(nil), registry...)
	regMu.Unlock()
	t.Cleanup(func() { regMu.Lock(); registry = saved; regMu.Unlock() })

	fsys := fstest.MapFS{"ideas.html": {Data: []byte(`{{define "content"}}<h1>Ideas page</h1>{{.Data}}{{end}}`)}}
	Register(Page{ID: "ideas", Title: "Ideas", Path: "/ui/ideas", Order: 40, Templates: fsys, Mount: func(ui *UI) {
		ui.Get("GET /ui/ideas", func(w http.ResponseWriter, r *http.Request) { ui.Render(w, r, "ideas", "Ideas", "hello") })
	}})
	f := newFixture(t)
	page := body(t, f.get("/ui/ideas"), 200)
	mustContain(t, page, "Ideas page", "hello", `class="active"`)
	today := body(t, f.get("/ui/today"), 200)
	if strings.Index(today, ">Week<") > strings.Index(today, ">Ideas<") || strings.Index(today, ">Ideas<") > strings.Index(today, ">Settings<") {
		t.Error("nav order wrong")
	}
	// A broken page template fails Mount instead of serving broken pages.
	Register(Page{ID: "broken", Title: "Broken", Path: "/ui/broken", Templates: fstest.MapFS{"broken.html": {Data: []byte(`{{define "content"}}{{`)}}})
	if _, err := Mount(http.NewServeMux(), f.svc, Options{}); err == nil {
		t.Error("want error for broken template")
	}
}

func TestEscapesUserText(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: "xss", Label: "<script>alert(1)</script>"}); err != nil {
		t.Fatal(err)
	}
	f.at(t, "2026-11-16 09:00", "xss", `"><img src=x onerror=alert(1)>`)
	f.clk.Set(utc("2026-11-16 09:30"))
	for _, p := range []string{"/ui/today", "/ui/settings", "/ui/week"} {
		out := body(t, f.get(p), 200)
		if strings.Contains(out, "<script>alert") || strings.Contains(out, "<img src=x") {
			t.Errorf("%s: unescaped user text", p)
		}
	}
}

func TestWeekNavigationAndEmpty(t *testing.T) {
	f := newFixture(t)
	page := body(t, f.get("/ui/week"), 200)
	mustContain(t, page, "2026-W47", "No time recorded this week", "No hours mapped to tickets", "w=2026-W46", "w=2026-W48")
	body(t, f.get("/ui/week?w=2026-W01"), 200)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAgentsPage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, ev := range []service.AgentEvent{
		{Instance: "i1", Event: "state", State: "action_required", Slot: ptr(2), Workspace: `C:\Code\Alpha`, Summary: "needs <b>approval</b>"},
		{Instance: "i2", Event: "state", State: "working"},
	} {
		if _, err := f.svc.AgentEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	page := body(t, f.get("/ui/agents"), 200)
	mustContain(t, page, "action_required", "Alpha", "unmapped", `C:\Code\Alpha`, "/ui/agents/go", "/ui/agents/queue", "needs &lt;b&gt;approval&lt;/b&gt;")
	if strings.Contains(page, "<b>approval</b>") {
		t.Error("summary not escaped")
	}

	// map the workspace to a context
	w := f.post("/ui/agents/map", url.Values{"workspace": {`C:\Code\Alpha`}, "context": {"meeting"}, "next": {"/ui/agents"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("map = %d", w.Code)
	}
	page = body(t, f.get("/ui/agents"), 200)
	mustContain(t, page, "Meeting", "Every workspace maps to a context.")

	// queue, go, ack
	for _, p := range []string{"/ui/agents/queue", "/ui/agents/go"} {
		if w := f.post(p, url.Values{"id": {"i2"}, "next": {"/ui/agents"}}); w.Code != http.StatusSeeOther {
			t.Errorf("%s = %d", p, w.Code)
		}
	}
	if w := f.post("/ui/agents/ack-done", url.Values{"next": {"/ui/agents"}}); w.Code != http.StatusSeeOther {
		t.Errorf("ack = %d", w.Code)
	}
	if w := f.post("/ui/agents/go-next", url.Values{"next": {"/ui/agents"}}); w.Code != http.StatusSeeOther {
		t.Errorf("go-next = %d", w.Code)
	}
}

func ptr(i int) *int { return &i }
