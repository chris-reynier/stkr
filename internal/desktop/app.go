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

	"github.com/chris-reynier/stkr/internal/server"
)

// Tray is the notification-area icon. Run blocks (on the thread that owns
// the icon) until Quit; the other methods may be called from any goroutine.
type Tray interface {
	Run(h Handler) error
	SetIcon(is IconState, tooltip string)
	Notify(n Notice)
	Quit()
}

// Handler receives tray events. Menu is called on the tray thread and must
// not block; the others may start work in the background.
type Handler interface {
	// Ready is called once the icon exists.
	Ready()
	// Activate is a left click on the icon, or another launch of stkrw.
	Activate()
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
	return &App{opts: opts, log: opts.Logger, ctx: ctx, cancel: cancel, refresh: make(chan struct{}, 1)}
}

// Run shows the tray icon and blocks until Quit. A hosted server is then
// stopped gracefully (at most QuitTimeout).
func (a *App) Run() error {
	err := a.opts.Tray.Run(a)
	a.cancel()
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
	a.opts.Tray.SetIcon(IconFor(nil), "STKR - starting")
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.loop()
	}()
}

func (a *App) loop() {
	t := time.NewTicker(a.opts.Poll)
	defer t.Stop()
	for {
		a.tick()
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		case <-a.refresh:
		}
	}
}

// tick connects if needed and refreshes the status.
func (a *App) tick() {
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
	port := a.opts.Port
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
	if st != nil {
		notes = a.watch.Observe(*st)
	}
	a.mu.Unlock()
	a.opts.Tray.SetIcon(IconFor(st), Tooltip(st))
	if a.opts.Notifications {
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

// Activate opens the Today page (the panel replaces it later).
func (a *App) Activate() { a.Select(ActOpenPanel) }

// Menu builds the menu from the last status.
func (a *App) Menu() []MenuItem {
	a.mu.Lock()
	defer a.mu.Unlock()
	return BuildMenu(a.status, a.opts.Version)
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
	if id == ActOpenPanel {
		return "", a.open(c.URL(pageToday))
	}
	if path, ok := strings.CutPrefix(id, prefixOpen); ok {
		if !strings.HasPrefix(path, "/ui/") {
			return "", fmt.Errorf("unknown page %q", path)
		}
		return "", a.open(c.URL(path))
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
