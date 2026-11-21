package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/testutil"
	"github.com/chris-reynier/stkr/internal/update"
)

func TestPanelNothingRunning(t *testing.T) {
	f := newFixture(t)
	page := body(t, f.get("/ui/panel"), 200)
	mustContain(t, page,
		`data-surface="panel"`, `/static/panel.css`, `/static/desktop.js`, `/static/htmx.min.js`,
		"Nothing running", `id="live"`, `hx-trigger="every 5s, refresh"`, `hx-get="/ui/panel/live"`,
		`action="/ui/switch"`, `name="ctx" value="planning"`, `name="ctx" value="meeting"`,
		`id="idea"`, `data-enter-submit`, `action="/ui/ideas/add"`,
		`action="/ui/day/start"`, `action="/ui/close"`, `action="/ui/import/clipboard"`,
		"Agents waiting: 0", `href="/ui/agents"`, "Open STKR", `href="/ui/settings"`, `data-desktop="pin"`,
		`name="next" value="/ui/panel"`)
	for _, absent := range []string{"Stop</button>", "Time to close the day", "is available", "Record voice", "Filter", "<nav", `class="top"`, `name="ctx" value="off"`} {
		if strings.Contains(page, absent) {
			t.Errorf("panel contains %q", absent)
		}
	}
	if !strings.Contains(page, `aria-pressed="true"`) {
		t.Error("the pin is on by default")
	}
	if strings.Contains(page, "autofocus") {
		t.Error("idea box is focused without asking")
	}
	if !strings.Contains(body(t, f.get("/ui/panel?focus=idea"), 200), " autofocus") {
		t.Error("focus=idea does not focus the idea box")
	}
	// The panel is not in the navigation of the other pages.
	if nav := body(t, f.get("/ui/today"), 200); strings.Contains(nav, `href="/ui/panel"`) {
		t.Error("panel is listed in the navigation")
	}
}

func TestPanelRunningCloseDueAndUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v0.2.0", "html_url": "https://example.test/v0.2.0"}})
	}))
	defer srv.Close()
	f := newFixture(t, func(d *service.Deps) {
		d.Version = "v0.1.0"
		d.Updater = &update.Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client()}
	})
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: "acme", Label: "ACME <b>portal</b>"}); err != nil {
		t.Fatal(err)
	}
	f.at(t, "2026-11-16 09:00", "acme", "")
	f.clk.Set(utc("2026-11-16 17:00"))
	if err := f.svc.UpdateTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	page := body(t, f.get("/ui/panel"), 200)
	mustContain(t, page, "ACME &lt;b&gt;portal&lt;/b&gt;", "8h00", "Stop</button>",
		"Time to close the day.", "STKR v0.2.0 is available", `class="secondary due"`, `class="current" aria-current="true"`)
	if strings.Contains(page, "<b>portal</b>") {
		t.Fatal("label is not escaped")
	}
	// The fragment is bare and carries the same state.
	live := body(t, f.get("/ui/panel/live"), 200)
	mustContain(t, live, "ACME &lt;b&gt;portal&lt;/b&gt;", "Time to close the day.")
	if strings.Contains(live, "<html") || strings.Contains(live, `id="idea"`) {
		t.Fatalf("live fragment is not bare: %s", live)
	}
	if got := f.get("/ui/panel/live").Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("CSP = %q", got)
	}
}

func TestPanelFilter(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("proj-%d", i)
		if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: id, Label: "Project " + strings.Repeat("x", i)}); err != nil {
			t.Fatal(err)
		}
	}
	// 4 seeded (off is not offered) + 8 added: above the limit.
	page := body(t, f.get("/ui/panel"), 200)
	mustContain(t, page, `id="q"`, "Filter 12 contexts", `hx-preserve="true"`, `value="proj-7"`)
	live := body(t, f.get("/ui/panel/live?q=XXXXXXX"), 200)
	mustContain(t, live, `value="proj-7"`, `value="XXXXXXX"`)
	if strings.Contains(live, `value="proj-0"`) || strings.Contains(live, `value="planning"`) {
		t.Fatalf("filter did not narrow the list: %s", live)
	}
	if none := body(t, f.get("/ui/panel/live?q=zzzzzz"), 200); !strings.Contains(none, "No context matches.") {
		t.Errorf("no empty message: %s", none)
	}
}

func TestPanelRecentFirstAndParkedHidden(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, id := range []string{"aaa", "zzz", "parked"} {
		if _, err := f.svc.AddContext(ctx, domain.Context{ID: id, Label: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.ParkContext(ctx, "parked"); err != nil {
		t.Fatal(err)
	}
	f.at(t, "2026-11-16 09:00", "zzz", "")
	live := body(t, f.get("/ui/panel/live"), 200)
	if strings.Contains(live, `value="parked"`) {
		t.Error("a parked context is offered")
	}
	if iz, ia := strings.Index(live, `value="zzz"`), strings.Index(live, `value="aaa"`); iz < 0 || ia < 0 || iz > ia {
		t.Errorf("the most recently used context is not first (zzz at %d, aaa at %d)", iz, ia)
	}
}

func TestPanelPinFollowsSavedConfig(t *testing.T) {
	f := newFixture(t)
	cfg, _, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Desktop.PanelOnTop = false
	if err := config.Save(f.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	page := body(t, f.get("/ui/panel"), 200)
	mustContain(t, page, `aria-pressed="false"`, ">Pin</button>")
	if strings.Contains(page, ">Pinned</button>") {
		t.Error("shows pinned after the saved config turned it off")
	}
}

func TestPanelActionsRoundTrip(t *testing.T) {
	clip := &testutil.FakeClipboard{}
	f := newFixture(t, func(d *service.Deps) { d.Clipboard = clip })
	ctx := context.Background()
	next := url.Values{"next": {"/ui/panel"}}

	// Switch, then Stop.
	w := f.post("/ui/switch", url.Values{"next": {"/ui/panel"}, "ctx": {"meeting"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/panel" {
		t.Fatalf("switch: %d %s", w.Code, w.Header().Get("Location"))
	}
	if now, _ := f.svc.Now(ctx); !now.Tracking || now.Context.ID != "meeting" {
		t.Fatalf("not tracking meeting: %+v", now)
	}
	if page := body(t, f.get("/ui/panel"), 200); !strings.Contains(page, "Meeting") || !strings.Contains(page, "Stop</button>") {
		t.Errorf("panel does not show the running context")
	}
	if w := f.post("/ui/stop", next); w.Code != http.StatusSeeOther {
		t.Fatalf("stop: %d", w.Code)
	}
	if now, _ := f.svc.Now(ctx); now.Tracking {
		t.Fatal("still tracking after Stop")
	}

	// Capture an idea; the flash shows on the panel afterwards.
	w = f.post("/ui/ideas/add", url.Values{"next": {"/ui/panel"}, "text": {"panel idea sentinel"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/panel" {
		t.Fatalf("idea: %d %s", w.Code, w.Header().Get("Location"))
	}
	ideas, err := f.svc.ListIdeas(ctx, service.IdeaFilter{})
	if err != nil || len(ideas) != 1 || ideas[0].RawText != "panel idea sentinel" {
		t.Fatalf("ideas = %+v, %v", ideas, err)
	}
	r := httptest.NewRequest(http.MethodGet, "/ui/panel", nil)
	r.Host = "127.0.0.1:7878"
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	mustContain(t, rec.Body.String(), "Idea saved.", `class="flash ok"`)

	// Start day and Close day copy their prompt.
	if w := f.post("/ui/day/start", next); w.Code != http.StatusSeeOther {
		t.Fatalf("start day: %d", w.Code)
	}
	if clip.Text() == "" {
		t.Error("Start day did not copy the prompt")
	}
	if w := f.post("/ui/close", next); w.Code != http.StatusSeeOther {
		t.Fatalf("close day: %d", w.Code)
	}

	// Import reads the clipboard.
	block := "```stkr-brief v1\ntop | 1 | Ship the panel\nitem | action | other | Check the panel | 2026-11-20\n```"
	if err := clip.WriteText(ctx, block); err != nil {
		t.Fatal(err)
	}
	if w := f.post("/ui/import/clipboard", next); w.Code != http.StatusSeeOther {
		t.Fatalf("import: %d", w.Code)
	}
	if items, err := f.svc.ListItems(ctx, service.ItemFilter{}); err != nil || len(items) != 1 || items[0].Summary != "Check the panel" {
		t.Fatalf("items = %+v, %v", items, err)
	}

	// Nothing importable on the clipboard: an error message, never a crash.
	_ = clip.WriteText(ctx, "just some words")
	w = f.post("/ui/import/clipboard", next)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("failed import: %d", w.Code)
	}
	var flash string
	for _, c := range w.Result().Cookies() {
		if c.Name == flashCookie {
			flash, _ = url.QueryUnescape(c.Value)
		}
	}
	if !strings.HasPrefix(flash, "e") {
		t.Errorf("flash = %q, want an error", flash)
	}

	// A foreign next is ignored, and a foreign Origin is refused.
	if w := f.post("/ui/import/clipboard", url.Values{"next": {"https://evil.example/"}}); w.Header().Get("Location") != "/ui/today" {
		t.Errorf("foreign next: %s", w.Header().Get("Location"))
	}
	if w := f.post("/ui/import/clipboard", next, "Origin", "https://evil.example"); w.Code != http.StatusForbidden {
		t.Errorf("foreign origin: %d", w.Code)
	}
}

func TestPanelRecordButtonOnlyWhenAvailable(t *testing.T) {
	f := newFixture(t)
	if page := body(t, f.get("/ui/panel"), 200); strings.Contains(page, "Record voice") {
		t.Error("record button without ffmpeg and whisper")
	}
	bin := testutil.BuildFakebin(t)
	f2 := newFixture(t)
	cfg := f2.svc.Config()
	cfg.FFmpeg, cfg.Whisper.Bin = bin, bin
	if page := body(t, f2.get("/ui/panel"), 200); !strings.Contains(page, "Record voice") {
		t.Error("record button missing when both tools exist")
	}
}

func TestDesktopAssets(t *testing.T) {
	f := newFixture(t)
	js := body(t, f.get("/static/desktop.js"), 200)
	mustContain(t, js, "data-enter-submit", "window.chrome.webview", "stkrPanelShown", "stkrPanelPinned", "'url:'", "'app:'", "'hide'")
	mustContain(t, body(t, f.get("/static/panel.css"), 200), "body.compact")
	// No page loads anything from another origin or uses inline code, which
	// the Content-Security-Policy would block.
	for _, p := range []string{"/ui/panel", "/ui/today"} {
		page := body(t, f.get(p), 200)
		if strings.Contains(page, "http://") || strings.Contains(page, "https://") {
			t.Errorf("%s references another origin", p)
		}
		if strings.Contains(page, "<script>") || strings.Contains(page, " style=") {
			t.Errorf("%s has inline script or style", p)
		}
	}
}
