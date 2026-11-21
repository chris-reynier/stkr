// Package desktop is the Windows tray app (stkrw.exe). It either attaches to
// a running `stkr serve` or hosts the same server in-process, polls
// GET /api/status, keeps the tray icon, tooltip and menu up to date and runs
// every menu action through the local HTTP API. The Win32 layer is in
// *_windows.go; everything else is plain Go and tested without a desktop.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/hotkey"
	"github.com/chris-reynier/stkr/internal/server"
)

// Tray is the notification-area icon. Run blocks (on the thread that owns
// the icon) until Quit; the other methods may be called from any goroutine.
type Tray interface {
	Run(h Handler) error
	SetIcon(is IconState, tooltip string)
	Notify(n Notice)
	// SetHotkey registers the global hotkey on the tray thread and reports
	// the result (nil, or why another program holds the key) to done.
	SetHotkey(k hotkey.Key, done func(error))
	Quit()
}

// Handler receives tray events. Menu is called on the tray thread and must
// not block; the others may start work in the background.
type Handler interface {
	// Ready is called once the icon exists.
	Ready()
	// Activate is a left click on the icon: it toggles the panel.
	Activate()
	// Show is another launch of stkrw: it shows the panel.
	Show()
	// Hotkey is the global hotkey: it toggles the panel with the idea box
	// focused.
	Hotkey()
	// Menu returns the menu to show on a right click.
	Menu() []MenuItem
	// Select runs the menu item with the given id.
	Select(id string)
}

// Hosted is a server this process runs.
type Hosted interface {
	Port() int
	Done() <-chan struct{}
	Shutdown(ctx context.Context) error
}

// HostFunc starts an in-process server on port (0 = free port). It returns a
// *server.ListenError when the port cannot be opened.
type HostFunc func(ctx context.Context, port int) (Hosted, error)

// Mode says how the app reaches a server.
type Mode string

// Connection modes.
const (
	ModeNone   Mode = ""
	ModeAttach Mode = "attach" // a `stkr serve` was already running
	ModeHost   Mode = "host"   // this process runs the server
)

// QuitTimeout bounds the shutdown of the hosted server on Quit.
const QuitTimeout = 5 * time.Second

// Options configure an App.
type Options struct {
	Home          string
	Version       string
	Port          int
	Poll          time.Duration
	Notifications bool
	Tray          Tray
	Host          HostFunc
	// Open opens a URL in the browser or a folder in the file manager.
	Open   func(target string) error
	Logger *slog.Logger
	// Windows builds the panel and app windows. Nil (or WebView2 missing)
	// makes every window action open the page in the browser instead.
	Windows WindowsFactory
	// PanelOnTop is the saved "keep the panel on top" state; SavePanelOnTop
	// stores a change in the configuration file.
	PanelOnTop     bool
	SavePanelOnTop func(on bool) error
	// Hotkey is the global hotkey text (empty = none).
	Hotkey string
	// OpenPanelOnStart shows the panel once the server is reachable.
	OpenPanelOnStart bool
	// Updater runs the update steps (nil: the app cannot update itself).
	Updater UpdateOps
	// Confirm asks a yes/no question in a message box (nil counts as "no").
	Confirm func(title, text string) bool
	// Exe is the path of this program, started again after an update.
	Exe string
	// Reload reads the saved settings again for a server restart (nil keeps
	// the current ones).
	Reload func() (Settings, error)
	// JustUpdated is set for the instance started by an update: it says so in
	// a notification once it is running.
	JustUpdated bool
}

// App is the tray application.
type App struct {
	opts    Options
	log     *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	refresh chan struct{}

	mu     sync.Mutex
	mode   Mode
	client *Client
	hosted Hosted
	status *Status
	watch  Watcher

	win        Windows
	winChecked bool
	winBroken  bool
	panelOnTop bool
	startShown bool

	busy        bool
	relaunch    *relaunch
	announcedUp bool
	hotkeyOn    bool
}

// NewApp builds an App. Call Run.
func NewApp(opts Options) *App {
	if opts.Poll <= 0 {
		opts.Poll = 5 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{opts: opts, log: opts.Logger, ctx: ctx, cancel: cancel, refresh: make(chan struct{}, 1), panelOnTop: opts.PanelOnTop}
	if opts.Windows != nil {
		a.win = opts.Windows(WindowCallbacks{Select: a.Select, Failed: a.windowsFailed})
	}
	return a
}

// Run shows the tray icon and blocks until Quit. A hosted server is then
// stopped gracefully (at most QuitTimeout).
func (a *App) Run() error {
	err := a.opts.Tray.Run(a)
	a.cancel()
	if a.win != nil {
		a.win.Close()
	}
	a.wg.Wait()
	a.mu.Lock()
	h := a.hosted
	a.hosted = nil
	a.mu.Unlock()
	if h != nil {
		ctx, cancel := context.WithTimeout(context.Background(), QuitTimeout)
		defer cancel()
		if serr := h.Shutdown(ctx); serr != nil {
			a.log.Error("hosted server shutdown", "err", serr)
		}
	}
	a.log.Info("desktop stopped")
	return err
}

// Ready starts connecting and polling.
func (a *App) Ready() {
	a.log.Info("tray ready")
	a.opts.Tray.SetIcon(IconFor(nil), "STKR - starting")
	a.setupHotkey()
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.loop()
	}()
}

func (a *App) loop() {
	for {
		a.tick()
		a.mu.Lock()
		poll := a.opts.Poll
		a.mu.Unlock()
		t := time.NewTimer(poll)
		select {
		case <-a.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-a.refresh:
			t.Stop()
		}
	}
}

// tick connects if needed and refreshes the status.
func (a *App) tick() {
	a.mu.Lock()
	busy := a.busy
	a.mu.Unlock()
	if busy {
		return // the server is down on purpose (update or restart)
	}
	a.mu.Lock()
	c, h := a.client, a.hosted
	a.mu.Unlock()
	if h != nil {
		select {
		case <-h.Done():
			a.log.Warn("hosted server stopped")
			a.setConn(ModeNone, nil, nil)
			c = nil
		default:
		}
	}
	if c == nil {
		c = a.connect()
		if c == nil {
			a.setStatus(nil)
			return
		}
	}
	st, err := c.Status(a.ctx)
	if err != nil {
		if a.ctx.Err() != nil {
			return
		}
		a.log.Warn("status failed", "err", err)
		a.mu.Lock()
		if a.mode == ModeAttach {
			// The attached server may have stopped: probe again next time.
			a.mode, a.client = ModeNone, nil
		}
		a.mu.Unlock()
		a.setStatus(nil)
		return
	}
	a.setStatus(&st)
	a.mu.Lock()
	announce := a.opts.JustUpdated && !a.announcedUp
	a.announcedUp = a.announcedUp || announce
	a.mu.Unlock()
	if announce {
		a.notify(Notice{Title: "STKR updated", Text: updatedText(a.opts.Version)})
	}
	a.mu.Lock()
	first := a.opts.OpenPanelOnStart && !a.startShown
	a.startShown = a.startShown || first
	a.mu.Unlock()
	if first {
		a.Select(ActShowPanel)
	}
}

// Decide probes the configured port: a STKR server there means attach, no
// answer means host on that port, any other answer means the port is used
// by another program.
func Decide(ctx context.Context, port int) (Mode, bool) {
	err := NewClient(port).Probe(ctx)
	switch {
	case err == nil:
		return ModeAttach, false
	case errors.Is(err, errNotSTKR):
		return ModeHost, true
	default:
		return ModeHost, false
	}
}

// connect attaches to a running server or starts the in-process one.
func (a *App) connect() *Client {
	a.mu.Lock()
	port := a.opts.Port
	a.mu.Unlock()
	mode, portBusy := Decide(a.ctx, port)
	if mode == ModeAttach {
		c := NewClient(port)
		a.setConn(ModeAttach, c, nil)
		a.log.Info("attached to running server", "port", port)
		return c
	}
	var h Hosted
	var err error
	if !portBusy {
		h, err = a.opts.Host(a.ctx, port)
		var le *server.ListenError
		if errors.As(err, &le) {
			portBusy = true
		} else if err != nil {
			a.log.Error("cannot start server", "err", err)
			a.notify(Notice{Title: "STKR cannot start", Text: err.Error(), Error: true})
			return nil
		}
	}
	if portBusy {
		if h, err = a.opts.Host(a.ctx, 0); err != nil {
			a.log.Error("cannot start server", "err", err)
			a.notify(Notice{Title: "STKR cannot start", Text: err.Error(), Error: true})
			return nil
		}
		a.log.Warn("port in use, server on a free port", "port", port, "fallback", h.Port())
		a.notify(Notice{Title: "STKR", Error: true, Text: fmt.Sprintf(
			"Port %d is in use, change `port` in Settings. STKR runs on port %d for now; Stream Deck keys will not reach it.", port, h.Port())})
	}
	c := NewClient(h.Port())
	a.setConn(ModeHost, c, h)
	a.log.Info("hosting server", "port", h.Port())
	if portBusy {
		if err := a.open(c.URL(pageSettings)); err != nil {
			a.log.Warn("cannot open settings", "err_kind", errKind(err))
		}
	}
	return c
}

func (a *App) setConn(m Mode, c *Client, h Hosted) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode, a.client, a.hosted = m, c, h
}

func (a *App) setStatus(st *Status) {
	a.mu.Lock()
	a.status = st
	var notes []Notice
	notify := a.opts.Notifications
	if st != nil {
		notes = a.watch.Observe(*st)
	}
	a.mu.Unlock()
	a.opts.Tray.SetIcon(IconFor(st), Tooltip(st))
	if notify {
		for _, n := range notes {
			a.notify(n)
		}
	}
}

func (a *App) notify(n Notice) { a.opts.Tray.Notify(n) }

// Mode reports how the app reaches its server.
func (a *App) Mode() Mode {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mode
}

// Activate toggles the panel (left click on the icon).
func (a *App) Activate() { a.Select(ActOpenPanel) }

// Show shows the panel (a second launch of stkrw).
func (a *App) Show() { a.Select(ActShowPanel) }

// Hotkey toggles the panel with the idea box focused.
func (a *App) Hotkey() { a.Select(ActHotkey) }

// Menu builds the menu from the last status.
func (a *App) Menu() []MenuItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	return BuildMenu(a.status, MenuOptions{Version: a.opts.Version, PanelOnTop: a.panelOnTop})
}

// Select runs a menu action in the background and reports its result in a
// notification.
func (a *App) Select(id string) {
	if id == ActQuit {
		a.log.Info("quit requested")
		a.opts.Tray.Quit()
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		msg, err := a.do(id)
		kind, _, _ := strings.Cut(id, ":")
		if err != nil {
			a.log.Warn("action failed", "action", kind, "err_kind", errKind(err))
			a.notify(Notice{Title: "STKR", Text: err.Error(), Error: true})
			return
		}
		a.log.Info("action", "action", kind)
		if msg != "" {
			a.notify(Notice{Title: "STKR", Text: msg})
		}
		select {
		case a.refresh <- struct{}{}:
		default:
		}
	}()
}

// errKind describes an error for the log without its text, which may echo
// user content (a context label or a bookmark URL).
func errKind(err error) string {
	var ae *APIError
	if errors.As(err, &ae) {
		return fmt.Sprintf("http %d %s", ae.Status, ae.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "error"
}

var errOffline = errors.New("the STKR server is not reachable; it is restarted automatically, try again in a few seconds")

// do runs one action and returns the text for the notification ("" = none).
func (a *App) do(id string) (string, error) {
	a.mu.Lock()
	c, mode, st := a.client, a.mode, a.status
	a.mu.Unlock()
	switch id {
	case ActCheckUpdate:
		return a.checkUpdate()
	case ActInstallUpdate:
		return a.installUpdate(true)
	case ActInstallNow:
		return a.installUpdate(false)
	case ActRestartServer:
		return a.restartServer()
	case ActPinPanel:
		return "", a.togglePin()
	case ActHidePanel:
		if a.win != nil {
			a.win.HidePanel()
		}
		return "", nil
	case ActDataFolder:
		return "", a.open(a.opts.Home)
	case ActLogsFolder:
		return "", a.open(filepath.Join(a.opts.Home, "logs"))
	case ActAbout:
		where := "not connected to a server"
		if c != nil {
			where = fmt.Sprintf("running the server on port %d", c.Port())
			if mode == ModeAttach {
				where = fmt.Sprintf("attached to `stkr serve` on port %d", c.Port())
			}
		}
		return fmt.Sprintf("STKR %s, %s. Data folder: %s", a.opts.Version, where, a.opts.Home), nil
	}
	if c == nil {
		return "", errOffline
	}
	switch id {
	case ActOpenPanel:
		return "", a.panel(c, true, false)
	case ActShowPanel:
		return "", a.panel(c, false, false)
	case ActCaptureIdea:
		return "", a.panel(c, false, true)
	case ActHotkey:
		return "", a.panel(c, true, true)
	case ActOpenApp:
		return "", a.app(c, pageToday)
	}
	if path, ok := strings.CutPrefix(id, prefixOpen); ok {
		if !strings.HasPrefix(path, "/ui/") {
			return "", fmt.Errorf("unknown page %q", path)
		}
		return "", a.app(c, path)
	}
	if _, ok := strings.CutPrefix(id, prefixURL); ok {
		// Another site always opens in the default browser, never in a window.
		b, ok := ParseBridge(id)
		if !ok {
			return "", errors.New("unknown address")
		}
		return "", a.open(strings.TrimPrefix(b.Action, prefixURL))
	}
	if ctxID, ok := strings.CutPrefix(id, prefixSwitch); ok {
		res, err := c.Post(a.ctx, "/api/switch/"+url.PathEscape(ctxID))
		if err != nil {
			return "", err
		}
		label := ctxID
		if st != nil {
			for _, x := range st.Contexts {
				if x.ID == ctxID {
					label = labelOf(x.Label, x.ID)
				}
			}
		}
		if created, _ := res["created"].(bool); !created {
			return "Already on " + label, nil
		}
		return "Switched to " + label, nil
	}
	switch id {
	case ActStop:
		res, err := c.Post(a.ctx, "/api/stop")
		if err != nil {
			return "", err
		}
		if created, _ := res["created"].(bool); !created {
			return "Nothing was running", nil
		}
		return "Stopped", nil
	case ActBookmarkClip:
		res, err := c.Post(a.ctx, "/api/bookmark/from-clipboard")
		if err != nil {
			return "", err
		}
		title := ""
		if b, ok := res["bookmark"].(map[string]any); ok {
			title, _ = b["title"].(string)
		}
		return strings.TrimSpace("Bookmark saved. " + shorten(title, 80)), nil
	case ActStartDay:
		res, err := c.Post(a.ctx, "/api/day/start")
		if err != nil {
			return "", err
		}
		return withWarnings("Start Day prompt copied to the clipboard. Paste it into your AI assistant.", res), nil
	case ActCloseDay:
		res, err := c.Post(a.ctx, "/api/close/prompt")
		if err != nil {
			return "", err
		}
		return withWarnings("Close Day prompt copied to the clipboard. Paste it into your AI assistant, then import the answer.", res), nil
	case ActImportClip:
		res, err := c.Post(a.ctx, "/api/import/clipboard")
		if err != nil {
			return "", err
		}
		return importText(res), nil
	}
	return "", fmt.Errorf("unknown action %q", id)
}

func (a *App) open(target string) error {
	if a.opts.Open == nil {
		return errors.New("opening files and pages is not supported here")
	}
	if err := a.opts.Open(target); err != nil {
		return fmt.Errorf("cannot open %s: %w", target, err)
	}
	return nil
}

func withWarnings(msg string, res map[string]any) string {
	if w, ok := res["warnings"].([]any); ok && len(w) > 0 {
		return fmt.Sprintf("%s (%d warning(s), see Today)", msg, len(w))
	}
	return msg
}

func importText(res map[string]any) string {
	num := func(k string) int { n, _ := res[k].(float64); return int(n) }
	msg := fmt.Sprintf("Imported: %d new item(s), %d done, %d top priorities.", num("items_created"), num("items_done"), num("tops_saved"))
	return withWarnings(msg, res)
}

// pagePanel is the compact page shown in the panel window.
const pagePanel = "/ui/panel"

// missingRuntimeText is the notification shown once when WebView2 is absent.
const missingRuntimeText = "Microsoft Edge WebView2 is not installed, so the panel cannot open. STKR opens its pages in your browser instead. Install the WebView2 Runtime from Microsoft to use the panel."

// windowFailedText is shown once when WebView2 is installed but a window still
// could not be created (for example the browser data folder is not usable).
const windowFailedText = "The STKR window could not be opened, so STKR opens its pages in your browser instead. Restart STKR to try again; if it keeps happening, see Troubleshooting in the desktop app guide."

// windowsUsable reports whether the windows can be shown. The first call
// asks the windows whether WebView2 exists and notifies once if it does not.
func (a *App) windowsUsable() bool {
	a.mu.Lock()
	if a.win == nil || a.winBroken {
		a.mu.Unlock()
		return false
	}
	check := !a.winChecked
	a.winChecked = true
	a.mu.Unlock()
	if !check {
		return true
	}
	if err := a.win.Available(); err != nil {
		a.markBroken(err, missingRuntimeText)
		return false
	}
	return true
}

// markBroken switches every window action to the browser and says so once.
func (a *App) markBroken(err error, text string) {
	a.mu.Lock()
	first := !a.winBroken
	a.winBroken = true
	a.mu.Unlock()
	if first {
		a.log.Warn("windows unavailable, using the browser", "err_kind", errKind(err))
		a.notify(Notice{Title: "STKR", Text: text, Error: true})
	}
}

// windowsFailed is called when a window could not be created after
// WebView2 reported itself as present. The page the user asked for opens in
// the browser instead.
func (a *App) windowsFailed(err error) {
	a.markBroken(err, windowFailedText)
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c != nil {
		if oerr := a.open(c.URL(pageToday)); oerr != nil {
			a.log.Warn("cannot open the page", "err_kind", errKind(oerr))
		}
	}
}

// panel shows or toggles the panel; without windows it opens Today (or the
// Ideas page for the capture actions) in the browser.
func (a *App) panel(c *Client, toggle, focusIdea bool) error {
	if !a.windowsUsable() {
		if focusIdea {
			return a.open(c.URL(pageIdeas))
		}
		return a.open(c.URL(pageToday))
	}
	if toggle {
		a.win.TogglePanel(c.URL(pagePanel), focusIdea)
	} else {
		a.win.ShowPanel(c.URL(pagePanel), focusIdea)
	}
	return nil
}

// app opens a page of the UI in the app window, or in the browser.
func (a *App) app(c *Client, path string) error {
	if !a.windowsUsable() {
		return a.open(c.URL(path))
	}
	a.win.ShowApp(c.URL(path))
	return nil
}

// togglePin flips "keep the panel on top", applies it and saves it.
func (a *App) togglePin() error {
	a.mu.Lock()
	on := !a.panelOnTop
	a.panelOnTop = on
	a.mu.Unlock()
	if a.win != nil {
		a.win.SetPanelOnTop(on)
	}
	if a.opts.SavePanelOnTop != nil {
		if err := a.opts.SavePanelOnTop(on); err != nil {
			return fmt.Errorf("the setting could not be saved: %w", err)
		}
	}
	return nil
}

// setupHotkey registers the global hotkey; a key that is already taken by
// another program is reported in a notification.
func (a *App) setupHotkey() {
	a.mu.Lock()
	text := a.opts.Hotkey
	a.mu.Unlock()
	k, err := hotkey.Parse(text)
	if err != nil {
		a.log.Warn("invalid hotkey in the configuration")
		a.notify(Notice{Title: "STKR", Error: true, Text: err.Error() + ". Fix desktop.hotkey in Settings."})
		return
	}
	a.mu.Lock()
	was := a.hotkeyOn
	a.hotkeyOn = !k.IsZero()
	a.mu.Unlock()
	if k.IsZero() {
		if was {
			a.opts.Tray.SetHotkey(k, func(error) {}) // turn the old key off
		}
		return
	}
	a.opts.Tray.SetHotkey(k, func(err error) {
		if err == nil {
			return
		}
		a.log.Warn("hotkey not registered", "err_kind", "register")
		a.notify(Notice{Title: "STKR", Error: true, Text: fmt.Sprintf(
			"The hotkey %s could not be registered, another program probably uses it. Change desktop.hotkey in Settings.", k)})
	})
}
