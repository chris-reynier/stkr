package desktop

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/hotkey"
)

// fakeWindows records what the app asks the panel and app windows to do.
type fakeWindows struct {
	mu       sync.Mutex
	calls    []string
	availErr error
	cb       WindowCallbacks
}

func (f *fakeWindows) rec(format string, a ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func (f *fakeWindows) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

func (f *fakeWindows) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeWindows) Available() error                 { return f.availErr }
func (f *fakeWindows) ShowPanel(u string, focus bool)   { f.rec("show-panel %s focus=%t", u, focus) }
func (f *fakeWindows) TogglePanel(u string, focus bool) { f.rec("toggle-panel %s focus=%t", u, focus) }
func (f *fakeWindows) HidePanel()                       { f.rec("hide-panel") }
func (f *fakeWindows) ShowApp(u string)                 { f.rec("show-app %s", u) }
func (f *fakeWindows) SetPanelOnTop(on bool)            { f.rec("on-top %t", on) }
func (f *fakeWindows) Close()                           { f.rec("close") }
func (f *fakeWindows) factory() WindowsFactory {
	return func(cb WindowCallbacks) Windows { f.cb = cb; return f }
}

// startAppWith starts an app on a hosted server with the given options on
// top of the defaults of startApp.
func startAppWith(t *testing.T, tray *fakeTray, mut func(*Options)) (*runningApp, string) {
	t.Helper()
	h, home := testHost(t, "")
	logs := &syncBuffer{}
	r := &runningApp{tray: tray, open: &opener{}, logs: &logs.Buffer, done: make(chan error, 1)}
	opts := Options{
		Home: home, Version: "0.2.0", Port: 0, Poll: 50 * time.Millisecond, Notifications: true,
		Tray: r.tray, Host: h.start, Open: r.open.open, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	}
	mut(&opts)
	r.app = NewApp(opts)
	go func() { r.done <- r.app.Run() }()
	<-r.tray.ready
	waitFor(t, "status", func() bool { return r.tray.lastTip() == "STKR - Nothing running" })
	return r, home
}

func TestPanelAndWindowActions(t *testing.T) {
	fw := &fakeWindows{}
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) { o.Windows = fw.factory(); o.PanelOnTop = true })
	base := "http://127.0.0.1:" + fmt.Sprint(r.app.ServerPort())

	r.app.Activate()
	r.app.Show()
	r.app.Select(ActCaptureIdea)
	r.app.Hotkey()
	r.app.Select(ActOpenApp)
	r.app.Select(prefixOpen + "/ui/week")
	r.app.Select(ActHidePanel)
	waitFor(t, "window calls", func() bool { return len(strings.Split(fw.all(), "\n")) >= 7 })
	got := fw.all()
	for _, want := range []string{
		"toggle-panel " + base + "/ui/panel focus=false",
		"show-panel " + base + "/ui/panel focus=false",
		"show-panel " + base + "/ui/panel focus=true",
		"toggle-panel " + base + "/ui/panel focus=true",
		"show-app " + base + "/ui/today",
		"show-app " + base + "/ui/week",
		"hide-panel",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing call %q in:\n%s", want, got)
		}
	}
	if r.open.all() != "" {
		t.Errorf("the browser was used although windows work: %s", r.open.all())
	}

	// A page message becomes the same action as a menu item.
	fw.cb.Select("open:/ui/agents")
	waitFor(t, "agents page", func() bool { return strings.Contains(fw.all(), "show-app "+base+"/ui/agents") })

	// Other sites always go to the default browser, never into a window.
	fw.cb.Select("url:https://example.com/page")
	waitFor(t, "browser", func() bool { return strings.Contains(r.open.all(), "https://example.com/page") })
	if fw.count("show-app") != 3 {
		t.Errorf("a window was used for an outside address:\n%s", fw.all())
	}
	r.app.Select("url:javascript:alert(1)")
	r.app.Select("url:file:///c:/windows/notepad.exe")
	waitFor(t, "bad address", func() bool { return strings.Count(r.tray.noticeTexts(), "unknown address") == 2 })
	if strings.Contains(r.open.all(), "javascript") || strings.Contains(r.open.all(), "file:") {
		t.Errorf("unsafe address opened: %s", r.open.all())
	}

	r.quit(t)
	if fw.count("close") != 1 {
		t.Errorf("windows were not closed on quit:\n%s", fw.all())
	}
	if strings.Contains(r.logs.String(), "example.com") {
		t.Errorf("an address reached the log:\n%s", r.logs.String())
	}
}

func TestPinPanel(t *testing.T) {
	fw := &fakeWindows{}
	var saved []bool
	var saveErr error
	var mu sync.Mutex
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) {
		o.Windows, o.PanelOnTop = fw.factory(), true
		o.SavePanelOnTop = func(on bool) error {
			mu.Lock()
			defer mu.Unlock()
			saved = append(saved, on)
			return saveErr
		}
	})
	if menu := RenderMenu(r.app.Menu()); !strings.Contains(menu, "[x] Keep panel on top") {
		t.Fatalf("menu:\n%s", menu)
	}
	r.app.Select(ActPinPanel)
	waitFor(t, "unpinned", func() bool { return strings.Contains(fw.all(), "on-top false") })
	if menu := RenderMenu(r.app.Menu()); strings.Contains(menu, "[x] Keep panel on top") || !strings.Contains(menu, "Keep panel on top") {
		t.Fatalf("menu after unpin:\n%s", menu)
	}
	// The pin button in the panel sends "pin", which flips it back.
	fw.cb.Select(ActPinPanel)
	waitFor(t, "pinned again", func() bool { return strings.Contains(fw.all(), "on-top true") })
	mu.Lock()
	if fmt.Sprint(saved) != "[false true]" {
		t.Errorf("saved = %v", saved)
	}
	saveErr = errors.New("disk full")
	mu.Unlock()
	r.app.Select(ActPinPanel)
	waitFor(t, "save error shown", func() bool { return strings.Contains(r.tray.noticeTexts(), "could not be saved: disk full") })
	r.quit(t)
}

func TestPinWorksWithoutWindows(t *testing.T) {
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) { o.PanelOnTop = false })
	r.app.Select(ActPinPanel)
	waitFor(t, "menu checked", func() bool { return strings.Contains(RenderMenu(r.app.Menu()), "[x] Keep panel on top") })
	r.quit(t)
}

func TestMissingWebView2FallsBackToTheBrowser(t *testing.T) {
	fw := &fakeWindows{availErr: errors.New("no runtime")}
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) { o.Windows = fw.factory() })
	r.app.Activate()
	waitFor(t, "today in the browser", func() bool { return strings.Contains(r.open.all(), "/ui/today") })
	waitFor(t, "notice", func() bool { return strings.Contains(r.tray.noticeTexts(), "WebView2 is not installed") })
	r.app.Select(ActCaptureIdea)
	r.app.Select(prefixOpen + "/ui/week")
	waitFor(t, "ideas and week in the browser", func() bool {
		return strings.Contains(r.open.all(), "/ui/ideas") && strings.Contains(r.open.all(), "/ui/week")
	})
	if n := strings.Count(r.tray.noticeTexts(), "WebView2 is not installed"); n != 1 {
		t.Errorf("the missing runtime was announced %d times", n)
	}
	if got := fw.all(); got != "" {
		t.Errorf("windows were used although unavailable:\n%s", got)
	}
	r.quit(t)
}

func TestWindowCreationFailureFallsBackToTheBrowser(t *testing.T) {
	fw := &fakeWindows{}
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) { o.Windows = fw.factory() })
	fw.cb.Failed(errors.New("boom"))
	waitFor(t, "today in the browser", func() bool { return strings.Contains(r.open.all(), "/ui/today") })
	waitFor(t, "notice", func() bool { return strings.Contains(r.tray.noticeTexts(), "WebView2 is not installed") })
	r.app.Activate()
	waitFor(t, "later actions use the browser", func() bool { return strings.Count(r.open.all(), "/ui/today") == 2 })
	if got := fw.all(); got != "" {
		t.Errorf("windows were used after a failure:\n%s", got)
	}
	r.quit(t)
}

func TestOpenPanelOnStart(t *testing.T) {
	fw := &fakeWindows{}
	r, _ := startAppWith(t, newFakeTray(), func(o *Options) { o.Windows, o.OpenPanelOnStart = fw.factory(), true })
	waitFor(t, "panel shown", func() bool { return fw.count("show-panel") == 1 })
	// Only once, however long the app runs.
	time.Sleep(200 * time.Millisecond)
	if n := fw.count("show-panel"); n != 1 {
		t.Errorf("panel shown %d times at start", n)
	}
	r.quit(t)

	fw2 := &fakeWindows{}
	r2, _ := startAppWith(t, newFakeTray(), func(o *Options) { o.Windows = fw2.factory() })
	time.Sleep(200 * time.Millisecond)
	if fw2.all() != "" {
		t.Errorf("panel shown at start although it is off:\n%s", fw2.all())
	}
	r2.quit(t)
}

func TestHotkeyRegistration(t *testing.T) {
	want, err := hotkey.Parse("Ctrl+Alt+Space")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("registered", func(t *testing.T) {
		tray := newFakeTray()
		r, _ := startAppWith(t, tray, func(o *Options) { o.Hotkey = "Ctrl+Alt+Space" })
		tray.mu.Lock()
		got := append([]hotkey.Key(nil), tray.hotkeys...)
		tray.mu.Unlock()
		if len(got) != 1 || got[0] != want {
			t.Errorf("registered %v, want %v", got, want)
		}
		if strings.Contains(tray.noticeTexts(), "hotkey") {
			t.Errorf("notice without a problem: %s", tray.noticeTexts())
		}
		r.quit(t)
	})
	t.Run("taken by another program", func(t *testing.T) {
		tray := newFakeTray()
		tray.hotkeyErr = errors.New("already registered")
		r, _ := startAppWith(t, tray, func(o *Options) { o.Hotkey = "Ctrl+Alt+Space" })
		waitFor(t, "notice", func() bool {
			return strings.Contains(tray.noticeTexts(), "The hotkey Ctrl+Alt+Space could not be registered")
		})
		if !strings.Contains(tray.noticeTexts(), "desktop.hotkey") {
			t.Errorf("the notice does not say where to change it: %s", tray.noticeTexts())
		}
		r.quit(t)
	})
	t.Run("off", func(t *testing.T) {
		tray := newFakeTray()
		r, _ := startAppWith(t, tray, func(o *Options) { o.Hotkey = "" })
		tray.mu.Lock()
		n := len(tray.hotkeys)
		tray.mu.Unlock()
		if n != 0 {
			t.Errorf("registered %d hotkeys although it is off", n)
		}
		r.quit(t)
	})
	t.Run("invalid text", func(t *testing.T) {
		tray := newFakeTray()
		r, _ := startAppWith(t, tray, func(o *Options) { o.Hotkey = "Space" })
		waitFor(t, "notice", func() bool { return strings.Contains(tray.noticeTexts(), "Fix desktop.hotkey") })
		tray.mu.Lock()
		n := len(tray.hotkeys)
		tray.mu.Unlock()
		if n != 0 {
			t.Errorf("registered an invalid hotkey")
		}
		r.quit(t)
	})
}

func TestWindowActionsOffline(t *testing.T) {
	// Without a server the panel cannot load anything: the action says so
	// instead of opening an empty window, and the pin still works.
	fw := &fakeWindows{}
	tray := newFakeTray()
	logs := &bytes.Buffer{}
	a := NewApp(Options{Tray: tray, Windows: fw.factory(), Logger: slog.New(slog.NewJSONHandler(logs, nil))})
	for _, id := range []string{ActOpenPanel, ActShowPanel, ActCaptureIdea, ActHotkey, ActOpenApp, prefixOpen + "/ui/week"} {
		if _, err := a.do(id); err == nil || !strings.Contains(err.Error(), "not reachable") {
			t.Errorf("%s: err = %v", id, err)
		}
	}
	if _, err := a.do(ActPinPanel); err != nil {
		t.Errorf("pin: %v", err)
	}
	if got := fw.all(); got != "on-top true" {
		t.Errorf("calls = %q", got)
	}
}
