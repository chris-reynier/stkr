package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/testutil"
	"github.com/chris-reynier/stkr/internal/update"
)

// releaseAPI serves a release list with the given tags.
func releaseAPI(t *testing.T, tags ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var out []map[string]any
		for _, tag := range tags {
			out = append(out, map[string]any{"tag_name": tag, "html_url": "https://example.test/" + tag})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withRelease(srv *httptest.Server, version string) func(*service.Deps) {
	return func(d *service.Deps) {
		d.Version = version
		d.Updater = &update.Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client()}
	}
}

// flash returns the one-time message set by a form action.
func flash(t *testing.T, w *httptest.ResponseRecorder) (isErr bool, text string) {
	t.Helper()
	ck := w.Result().Cookies()
	if len(ck) == 0 || ck[0].Name != flashCookie {
		t.Fatalf("no flash cookie: %v", ck)
	}
	v, err := url.QueryUnescape(ck[0].Value)
	if err != nil || len(v) < 2 {
		t.Fatalf("bad flash %q", ck[0].Value)
	}
	return v[0] == 'e', v[1:]
}

// getWithFlash loads path with the flash cookie of a previous response.
func (f *fixture) getWithFlash(path string, prev *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = "127.0.0.1:7878"
	for _, c := range prev.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

type hookCalls struct {
	update, saved, folder atomic.Int32
	updateErr             error
}

func (h *hookCalls) hooks() Hooks {
	return Hooks{
		RequestUpdate:  func(context.Context) error { h.update.Add(1); return h.updateErr },
		SettingsSaved:  func() { h.saved.Add(1) },
		OpenDataFolder: func() error { h.folder.Add(1); return nil },
	}
}

func checkUpdate(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.svc.UpdateTick(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPlainServeCannotInstallUpdates(t *testing.T) {
	srv := releaseAPI(t, "v0.2.0")
	calls := &hookCalls{}
	// Hooks are handed over but the server is not hosted by the desktop app.
	f := newFixtureWith(t, Options{HostedBy: "serve", Hooks: calls.hooks()}, withRelease(srv, "v0.1.0"))
	checkUpdate(t, f)

	for _, page := range []string{"/ui/today", "/ui/settings"} {
		body := body(t, f.get(page), 200)
		if strings.Contains(body, "Install update") || strings.Contains(body, "/ui/update/apply") {
			t.Errorf("%s offers the install button under plain serve", page)
		}
		if !strings.Contains(body, "stkr update") || !strings.Contains(body, "restart `stkr serve`") {
			t.Errorf("%s has no hint to run stkr update", page)
		}
	}
	w := f.post("/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if w.Code != 303 {
		t.Fatalf("code = %d", w.Code)
	}
	isErr, text := flash(t, w)
	if !isErr || !strings.Contains(text, "stkr update") {
		t.Errorf("flash = %v %q", isErr, text)
	}
	if calls.update.Load() != 0 {
		t.Error("the update hook ran under plain serve")
	}
}

func TestDesktopHostedUpdateButtons(t *testing.T) {
	srv := releaseAPI(t, "v0.2.0")
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()}, withRelease(srv, "v0.1.0"))

	// Nothing checked yet: no banner, apply is refused.
	if strings.Contains(body(t, f.get("/ui/today"), 200), "Install update") {
		t.Error("button before any check")
	}
	w := f.post("/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if isErr, text := flash(t, w); !isErr || !strings.Contains(text, "Check now") {
		t.Errorf("without a pending update: %v %q", isErr, text)
	}
	if calls.update.Load() != 0 {
		t.Fatal("hook ran without a pending update")
	}

	// "Check now" finds it.
	w = f.post("/ui/update/check", url.Values{"next": {"/ui/settings"}})
	if isErr, text := flash(t, w); isErr || !strings.Contains(text, "v0.2.0 is available") {
		t.Errorf("check: %v %q", isErr, text)
	}
	page := body(t, f.get("/ui/settings"), 200)
	for _, want := range []string{`id="updates"`, "Running STKR <strong>v0.1.0</strong>", "Latest release: <strong>v0.2.0</strong>",
		`href="https://example.test/v0.2.0"`, `action="/ui/update/apply"`, "Install update", "Check now"} {
		if !strings.Contains(page, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
	if !strings.Contains(body(t, f.get("/ui/today"), 200), `action="/ui/update/apply"`) {
		t.Error("banner has no Install update button")
	}

	// Install asks the desktop host exactly once.
	w = f.post("/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if isErr, text := flash(t, w); isErr || !strings.Contains(text, "Installing STKR v0.2.0") {
		t.Errorf("apply: %v %q", isErr, text)
	}
	if calls.update.Load() != 1 {
		t.Errorf("hook calls = %d", calls.update.Load())
	}

	// A host that cannot start the update reports it.
	calls.updateErr = domain.New(domain.ErrConflict, "an update is already running")
	w = f.post("/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if isErr, text := flash(t, w); !isErr || !strings.Contains(text, "already running") {
		t.Errorf("hook error: %v %q", isErr, text)
	}
}

func TestUpToDateAndDevBuilds(t *testing.T) {
	srv := releaseAPI(t, "v0.1.0")
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()}, withRelease(srv, "v0.1.0"))
	w := f.post("/ui/update/check", url.Values{"next": {"/ui/settings"}})
	if isErr, text := flash(t, w); isErr || !strings.Contains(text, "up to date") {
		t.Errorf("up to date: %v %q", isErr, text)
	}
	if page := body(t, f.get("/ui/settings"), 200); strings.Contains(page, "Install update") || !strings.Contains(page, "STKR is up to date") {
		t.Error("up to date page is wrong")
	}

	dev := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()}, withRelease(srv, "dev"))
	dev.post("/ui/update/check", url.Values{"next": {"/ui/settings"}})
	page := body(t, dev.get("/ui/settings"), 200)
	if strings.Contains(page, "Install update") || !strings.Contains(page, "development build") {
		t.Error("a development build must not offer an update")
	}
	w = dev.post("/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if isErr, _ := flash(t, w); !isErr || calls.update.Load() != 0 {
		t.Error("a development build was updated")
	}
}

func TestCheckNowWorksWithChecksOff(t *testing.T) {
	srv := releaseAPI(t, "v0.2.0")
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()}, withRelease(srv, "v0.1.0"))
	if err := f.svc.UpdateConfig(context.Background(), map[string]string{"update.check": "false"}); err != nil {
		t.Fatal(err)
	}
	// The running service keeps its start-up config, so turn the flag off there too.
	f.svc.Config().Update.Check = false
	f.post("/ui/update/check", url.Values{"next": {"/ui/settings"}})
	if strings.Contains(body(t, f.get("/ui/today"), 200), `id="update-banner" class="banner" hidden`) == false {
		t.Error("banner shown although update.check is off")
	}
	// The explicit check is still honoured on the Settings page and by Install.
	if !strings.Contains(body(t, f.get("/ui/settings"), 200), "Install update") {
		t.Error("explicit check result not offered")
	}
	f.post("/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if calls.update.Load() != 1 {
		t.Error("explicit check did not allow the install")
	}
}

func TestSettingsSaveRestartsDesktopServer(t *testing.T) {
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()})
	w := f.post("/ui/settings/config", url.Values{"next": {"/ui/settings"}, "idle_gap_minutes": {"45"}, "desktop.poll_seconds": {"9"}})
	if isErr, text := flash(t, w); isErr || !strings.Contains(text, "restarting") {
		t.Fatalf("flash = %v %q", isErr, text)
	}
	if calls.saved.Load() != 1 {
		t.Errorf("restart hook calls = %d", calls.saved.Load())
	}
	cfg, _ := f.svc.ConfigFile()
	if cfg.IdleGapMinutes != 45 || cfg.Desktop.PollSeconds != 9 {
		t.Errorf("not saved: %d %d", cfg.IdleGapMinutes, cfg.Desktop.PollSeconds)
	}
	// The next page waits for the restart; only that one page.
	if page := body(t, f.getWithFlash("/ui/settings", w), 200); !strings.Contains(page, "data-wait-restart") {
		t.Error("page does not wait for the restart")
	}
	if strings.Contains(body(t, f.get("/ui/settings"), 200), "data-wait-restart") {
		t.Error("marker is shown again")
	}

	// A rejected change does not restart anything.
	w = f.post("/ui/settings/config", url.Values{"next": {"/ui/settings"}, "desktop.poll_seconds": {"1"}})
	if isErr, _ := flash(t, w); !isErr || calls.saved.Load() != 1 {
		t.Errorf("invalid value: restart hook calls = %d", calls.saved.Load())
	}
	if strings.Contains(body(t, f.getWithFlash("/ui/settings", w), 200), "data-wait-restart") {
		t.Error("error page waits for a restart")
	}
}

func TestSettingsSavePlainServeNeedsManualRestart(t *testing.T) {
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: "serve", Hooks: calls.hooks()})
	// Desktop settings are neither shown nor accepted.
	page := body(t, f.get("/ui/settings"), 200)
	for _, hidden := range []string{"Desktop app", "desktop.hotkey", "desktop.autostart", "Open data folder"} {
		if strings.Contains(page, hidden) {
			t.Errorf("plain serve shows %q", hidden)
		}
	}
	w := f.post("/ui/settings/config", url.Values{"next": {"/ui/settings"}, "idle_gap_minutes": {"45"},
		"desktop.hotkey": {"Ctrl+Alt+K"}, "desktop.autostart": {"true"}})
	if isErr, text := flash(t, w); isErr || !strings.Contains(text, "Restart `stkr serve`") {
		t.Fatalf("flash = %v %q", isErr, text)
	}
	if calls.saved.Load() != 0 {
		t.Error("restart hook ran under plain serve")
	}
	cfg, _ := f.svc.ConfigFile()
	if cfg.IdleGapMinutes != 45 || cfg.Desktop.Hotkey == "Ctrl+Alt+K" || cfg.Desktop.Autostart {
		t.Errorf("config = %d %q %v", cfg.IdleGapMinutes, cfg.Desktop.Hotkey, cfg.Desktop.Autostart)
	}
	if strings.Contains(body(t, f.getWithFlash("/ui/settings", w), 200), "data-wait-restart") {
		t.Error("plain serve waits for a restart")
	}
	if isErr, _ := flash(t, f.post("/ui/settings/open-data", url.Values{"next": {"/ui/settings"}})); !isErr || calls.folder.Load() != 0 {
		t.Error("open-data worked under plain serve")
	}
}

func TestDesktopSettingsSectionAndAutostart(t *testing.T) {
	calls := &hookCalls{}
	fake := &testutil.FakeAutostart{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()}, func(d *service.Deps) { d.Autostart = fake })
	page := body(t, f.get("/ui/settings"), 200)
	for _, want := range []string{"Desktop app", "Start STKR when I sign in", "desktop.hotkey", "desktop.notifications",
		"desktop.panel_on_top", "desktop.open_panel_on_start", "desktop.poll_seconds", "Open data folder", "Back up now",
		"update.channel", "update.check", "update.interval_hours"} {
		if !strings.Contains(page, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
	if strings.Contains(page, `name="desktop.autostart" value="true" checked`) {
		t.Error("autostart shown as on")
	}
	f.post("/ui/settings/config", url.Values{"next": {"/ui/settings"}, "desktop.autostart": {"false", "true"}})
	if on, _ := fake.Enabled(); !on {
		t.Fatal("the sign-in entry was not written")
	}
	// The entry, not the file, decides what the page shows.
	if !strings.Contains(body(t, f.get("/ui/settings"), 200), `name="desktop.autostart" value="true" checked`) {
		t.Error("autostart not shown as on")
	}
	_ = fake.Set(false)
	if strings.Contains(body(t, f.get("/ui/settings"), 200), `name="desktop.autostart" value="true" checked`) {
		t.Error("page follows the config file instead of the entry")
	}
	// A refused write is an error and changes nothing.
	fake.Err = errors.New("denied")
	w := f.post("/ui/settings/config", url.Values{"next": {"/ui/settings"}, "desktop.autostart": {"false", "true"}, "idle_gap_minutes": {"33"}})
	if isErr, _ := flash(t, w); !isErr {
		t.Error("registry error not shown")
	}
	if cfg, _ := f.svc.ConfigFile(); cfg.IdleGapMinutes == 33 {
		t.Error("config saved despite the failed sign-in entry")
	}
}

func TestBackupAndOpenDataFolder(t *testing.T) {
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()})
	w := f.post("/ui/settings/backup", url.Values{"next": {"/ui/settings"}})
	isErr, text := flash(t, w)
	if isErr || !strings.HasPrefix(text, "Backup saved: stkr-") {
		t.Fatalf("flash = %v %q", isErr, text)
	}
	entries, err := os.ReadDir(filepath.Join(f.dir, "backups"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no backup file: %v %v", entries, err)
	}
	if strings.Contains(text, f.dir) {
		t.Error("the flash shows the full path")
	}
	w = f.post("/ui/settings/open-data", url.Values{"next": {"/ui/settings"}})
	if isErr, _ := flash(t, w); isErr || calls.folder.Load() != 1 {
		t.Errorf("open data folder: %v, calls %d", isErr, calls.folder.Load())
	}
}

func TestUpdateRoutesRejectForeignOrigin(t *testing.T) {
	calls := &hookCalls{}
	f := newFixtureWith(t, Options{HostedBy: HostedByDesktop, Hooks: calls.hooks()})
	for _, p := range []string{"/ui/update/apply", "/ui/update/check", "/ui/settings/backup", "/ui/settings/open-data", "/ui/settings/config"} {
		if w := f.post(p, url.Values{"next": {"/ui/settings"}}, "Origin", "http://evil.example"); w.Code != http.StatusForbidden {
			t.Errorf("%s with a foreign origin: %d", p, w.Code)
		}
		// GET is not an action.
		if w := f.get(p); w.Code == 200 || w.Code == 303 {
			t.Errorf("GET %s answered %d", p, w.Code)
		}
	}
	if calls.update.Load() != 0 || calls.saved.Load() != 0 || calls.folder.Load() != 0 {
		t.Error("a rejected request reached a hook")
	}
}
