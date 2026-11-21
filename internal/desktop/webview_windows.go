//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// The panel and the app window show the web UI in WebView2 through the
// go-webview2 "edge" package (pure Go, no CGO). Threading: the tray (P11)
// owns the thread of the main goroutine and its message loop. A window must
// be created and used on one thread that runs its own message loop, so each
// window gets a goroutine locked to an OS thread with COM initialised as a
// single-threaded apartment (what WebView2 requires) and its own GetMessage
// loop. Other goroutines never touch a window: they queue a function and wake
// the window thread with a posted message.

var errWebView2Missing = errors.New("the Microsoft Edge WebView2 runtime is not installed")

var (
	procShowWindow           = user32.NewProc("ShowWindow")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procIsIconic             = user32.NewProc("IsIconic")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProc  = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput    = user32.NewProc("AttachThreadInput")
	procBringWindowToTop     = user32.NewProc("BringWindowToTop")
	procLoadCursorW          = user32.NewProc("LoadCursorW")
	procEnumDisplayMonitors  = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW      = user32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem      = user32.NewProc("GetDpiForSystem")
	webClassName             = "STKRWebWindow"
	webWindows               sync.Map // HWND (uintptr) -> *webWindow
	registerWebClassOnce     sync.Once
	registerWebClassErr      error
	webWndProcAddr           = windows.NewCallback(webWndProc)
	monitorEnumProcAddr      = windows.NewCallback(monitorEnumProc)
	monitorMu                sync.Mutex
	monitorAreas, monitorAll []monitorRect
)

const (
	wmSize         = 0x0005
	wmMove         = 0x0003
	wmActivate     = 0x0006
	wmGetMinMax    = 0x0024
	wmNCDestroy    = 0x0082
	wmExitSizeMove = 0x0232
	wmDpiChanged   = 0x02E0
	wmMoving       = 0x0216
	wmWinCall      = wmApp + 10 // run queued calls on a window thread
	wmForceClose   = wmApp + 11 // destroy the window (Quit)

	sizeRestored  = 0
	sizeMinimized = 1
	sizeMaximized = 2

	wsCaption          = 0x00C00000
	wsSysMenu          = 0x00080000
	wsThickFrame       = 0x00040000
	wsOverlappedWindow = 0x00CF0000
	wsExTopmost        = 0x00000008
	wsExToolWindow     = 0x00000080
	wsExAppWindow      = 0x00040000

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	swHide     = 0
	swMaximize = 3
	swShow     = 5
	swRestore  = 9

	idcArrow         = 32512
	colorWindowBrush = 6 // COLOR_WINDOW + 1

	monitorInfoPrimary = 1

	panelDefaultW, panelDefaultH = 360, 560
	panelMinW, panelMinH         = 300, 380
	appDefaultW, appDefaultH     = 1100, 780
	appMinW, appMinH             = 640, 420
)

var hwndTopmost = ^uintptr(0)   // HWND_TOPMOST (-1)
var hwndNoTopmost = ^uintptr(1) // HWND_NOTOPMOST (-2)

type winRect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

type monitorRect struct {
	work    Rect
	primary bool
}

type minMaxInfo struct {
	PtReserved, PtMaxSize, PtMaxPosition, PtMinTrackSize, PtMaxTrackSize point
}

func (r winRect) rect() Rect { return Rect{int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)} }

// ptrFrom turns an address received in a window message into a pointer.
func ptrFrom(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p)) //nolint:gosec // G103: lParam of WM_GETMINMAXINFO / WM_DPICHANGED points at a struct owned by the system
}

func monitorEnumProc(hMon, _, _, _ uintptr) uintptr {
	mi := monitorInfo{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r != 0 { //nolint:gosec // G103: Win32 out parameter
		monitorAll = append(monitorAll, monitorRect{work: mi.RcWork.rect(), primary: mi.DwFlags&monitorInfoPrimary != 0})
	}
	return 1
}

// workAreas lists the work area (screen minus taskbar) of each monitor, the
// primary monitor first.
func workAreas() []Rect {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	monitorAll = nil
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, monitorEnumProcAddr, 0)
	sort.SliceStable(monitorAll, func(i, j int) bool { return monitorAll[i].primary && !monitorAll[j].primary })
	out := make([]Rect, 0, len(monitorAll))
	for _, m := range monitorAll {
		out = append(out, m.work)
	}
	return out
}

func systemScale() float64 {
	if procGetDpiForSystem.Find() != nil {
		return 1
	}
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi < 96 {
		return 1
	}
	return float64(dpi) / 96
}

func scaled(v int, s float64) int { return int(float64(v)*s + 0.5) }

// webHost implements Windows.
type webHost struct {
	log       *slog.Logger
	cb        WindowCallbacks
	statePath string
	dataDir   string
	onTop     atomic.Bool
	closing   atomic.Bool

	stateMu sync.Mutex
	state   WindowState

	panel, app *webWindow
}

// platformWindowsFactory builds the WebView2 windows for a data folder.
func platformWindowsFactory(home string, onTop bool, lg *slog.Logger) WindowsFactory {
	return func(cb WindowCallbacks) Windows {
		h := &webHost{log: lg, cb: cb, statePath: StatePath(home), dataDir: filepath.Join(home, "desktop", "webview")}
		h.onTop.Store(onTop)
		st, err := LoadState(h.statePath)
		if err != nil {
			lg.Warn("window state ignored", "err_kind", "load")
		}
		h.state = st
		h.panel = &webWindow{h: h, kind: SurfacePanel}
		h.app = &webWindow{h: h, kind: SurfaceApp}
		return h
	}
}

func (h *webHost) Available() error {
	v, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return fmt.Errorf("%w (%v)", errWebView2Missing, err)
	}
	if v == "" {
		return errWebView2Missing
	}
	return nil
}

func (h *webHost) ShowPanel(url string, focusIdea bool) {
	h.panel.send(func() { h.panel.showPanel(url, focusIdea) })
}

func (h *webHost) TogglePanel(url string, focusIdea bool) {
	h.panel.send(func() {
		if h.panel.visible() {
			h.panel.hide()
			return
		}
		h.panel.showPanel(url, focusIdea)
	})
}

func (h *webHost) HidePanel() {
	if h.panel.isRunning() {
		h.panel.send(h.panel.hide)
	}
}

func (h *webHost) ShowApp(url string) { h.app.send(func() { h.app.showApp(url) }) }

func (h *webHost) SetPanelOnTop(on bool) {
	h.onTop.Store(on)
	if h.panel.isRunning() {
		h.panel.send(func() {
			h.panel.applyTopmost()
			if h.panel.ready {
				h.panel.chromium.Eval(fmt.Sprintf("window.stkrPanelPinned&&window.stkrPanelPinned(%t)", on))
			}
		})
	}
}

func (h *webHost) Close() {
	h.closing.Store(true)
	h.panel.stop(3 * time.Second)
	h.app.stop(3 * time.Second)
}

// remember stores the last placement of a window in window.json.
func (h *webHost) remember(kind Surface, p Placement) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	if kind == SurfacePanel {
		h.state.Panel = &p
	} else {
		h.state.App = &p
	}
	if err := SaveState(h.statePath, h.state); err != nil {
		h.log.Warn("window state not saved", "err_kind", "save")
	}
}

func (h *webHost) saved(kind Surface) *Placement {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	var p *Placement
	if kind == SurfacePanel {
		p = h.state.Panel
	} else {
		p = h.state.App
	}
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// webWindow is one WebView2 window and its thread.
type webWindow struct {
	h    *webHost
	kind Surface

	mu      sync.Mutex
	queue   []func()
	running bool
	done    chan struct{}
	hwnd    atomic.Uintptr

	// Owned by the window thread.
	chromium  *edge.Chromium
	ready     bool
	loaded    string // address last loaded
	normal    Placement
	maximized bool
	shown     bool // shown at least once
	quitting  bool // destroy as soon as WebView2 is up (Quit came during start)
}

func (w *webWindow) isRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// send queues fn for the window thread, starting the thread if needed.
func (w *webWindow) send(fn func()) {
	if w.h.closing.Load() {
		return
	}
	w.mu.Lock()
	w.queue = append(w.queue, fn)
	start := !w.running
	if start {
		w.running = true
		w.done = make(chan struct{})
	}
	hwnd := w.hwnd.Load()
	w.mu.Unlock()
	switch {
	case start:
		go w.run()
	case hwnd != 0:
		_, _, _ = procPostMessageW.Call(hwnd, wmWinCall, 0, 0)
	}
}

// stop destroys the window and waits for its thread.
func (w *webWindow) stop(timeout time.Duration) {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	done := w.done
	hwnd := w.hwnd.Load()
	w.mu.Unlock()
	if hwnd != 0 {
		_, _, _ = procPostMessageW.Call(hwnd, wmForceClose, 0, 0)
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func (w *webWindow) drain() {
	for {
		w.mu.Lock()
		q := w.queue
		w.queue = nil
		w.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			fn()
		}
	}
}

func (w *webWindow) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}
	err := w.create()
	if err != nil {
		w.h.log.Error("window not created", "window", string(w.kind), "err", err)
		w.finish(false)
		if !w.h.closing.Load() && w.h.cb.Failed != nil {
			w.h.cb.Failed(err)
		}
		return
	}
	if w.quitting || w.h.closing.Load() {
		_, _, _ = procDestroyWindow.Call(w.hwnd.Load())
	}
	w.drain()
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0) //nolint:gosec // G103: Win32 out parameter
		if int32(r) <= 0 {                                                    //nolint:gosec // G115: GetMessage returns a BOOL (-1, 0 or nonzero)
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m))) //nolint:gosec // G103: Win32 argument
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m))) //nolint:gosec // G103: Win32 argument
	}
	w.finish(true)
}

// finish resets the window after its thread ends. With allowRestart,
// requests queued in the meantime start a new window (the user closed the app
// window and asked for it again at the same moment).
func (w *webWindow) finish(allowRestart bool) {
	w.mu.Lock()
	w.hwnd.Store(0)
	w.ready, w.chromium, w.loaded, w.shown, w.quitting = false, nil, "", false, false
	done := w.done
	restart := allowRestart && len(w.queue) > 0 && !w.h.closing.Load()
	if restart {
		w.done = make(chan struct{})
	} else {
		w.running = false
		w.queue = nil
	}
	w.mu.Unlock()
	close(done)
	if restart {
		go w.run()
	}
}

func (w *webWindow) title() string { return "STKR" }

func registerWebClass() error {
	registerWebClassOnce.Do(func() {
		var inst windows.Handle
		if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
			registerWebClassErr = fmt.Errorf("module handle: %w", err)
			return
		}
		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		icon := makeIcon(IconState{Color: colProject, Running: true})
		wc := wndClassEx{LpfnWndProc: webWndProcAddr, HInstance: inst, HCursor: windows.Handle(cursor), HIcon: icon, HIconSm: icon, HbrBackground: colorWindowBrush, LpszClassName: u16(webClassName)}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 { //nolint:gosec // G103: Win32 call with our own struct
			registerWebClassErr = fmt.Errorf("register window class: %w", err)
		}
	})
	return registerWebClassErr
}

// create makes the (hidden) window and starts WebView2 in it.
func (w *webWindow) create() error {
	if err := registerWebClass(); err != nil {
		return err
	}
	if err := os.MkdirAll(w.h.dataDir, 0o700); err != nil {
		return fmt.Errorf("browser data folder: %w", err)
	}
	// go-webview2 ends the process when WebView2 cannot create its
	// environment, so check the one thing that usually breaks it.
	probe, err := os.CreateTemp(w.h.dataDir, ".probe-*")
	if err != nil {
		return fmt.Errorf("browser data folder is not writable: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		return fmt.Errorf("module handle: %w", err)
	}
	scale := systemScale()
	areas := workAreas()
	var p Placement
	var style, ex uintptr
	if w.kind == SurfacePanel {
		style = wsCaption | wsSysMenu | wsThickFrame
		ex = wsExToolWindow
		if w.h.onTop.Load() {
			ex |= wsExTopmost
		}
		if s := w.h.saved(SurfacePanel); s != nil {
			p = Clamp(*s, scaled(panelMinW, scale), scaled(panelMinH, scale), areas)
		} else {
			var cur point
			_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&cur))) //nolint:gosec // G103: Win32 out parameter
			p = NearAnchor(scaled(panelDefaultW, scale), scaled(panelDefaultH, scale), Point{int(cur.X), int(cur.Y)}, areas)
		}
	} else {
		style = wsOverlappedWindow
		ex = wsExAppWindow
		if s := w.h.saved(SurfaceApp); s != nil {
			p = Clamp(*s, scaled(appMinW, scale), scaled(appMinH, scale), areas)
			w.maximized = s.Maximized
		} else if len(areas) > 0 {
			p = Centered(scaled(appDefaultW, scale), scaled(appDefaultH, scale), areas[0])
		} else {
			p = Placement{X: 100, Y: 100, W: appDefaultW, H: appDefaultH}
		}
	}
	w.normal = p
	hwnd, _, cerr := procCreateWindowExW.Call(ex,
		uintptr(unsafe.Pointer(u16(webClassName))), uintptr(unsafe.Pointer(u16(w.title()))), style, //nolint:gosec // G103: Win32 string arguments
		uintptr(p.X), uintptr(p.Y), uintptr(p.W), uintptr(p.H), 0, 0, uintptr(inst), 0) //nolint:gosec // G115: screen coordinates are passed as int in the Win32 ABI
	if hwnd == 0 {
		return fmt.Errorf("create window: %w", cerr)
	}
	webWindows.Store(hwnd, w)
	w.hwnd.Store(hwnd)

	c := edge.NewChromium()
	c.DataPath = w.h.dataDir
	c.MessageCallback = w.onMessage
	w.chromium = c
	if !c.Embed(hwnd) {
		_, _, _ = procDestroyWindow.Call(hwnd)
		return errors.New("WebView2 could not be started")
	}
	if s, serr := c.GetSettings(); serr == nil && s != nil {
		// No context menu, developer tools, status bar or browser shortcuts:
		// this is an application window, not a browser.
		_ = s.PutAreDefaultContextMenusEnabled(false)
		_ = s.PutAreDevToolsEnabled(false)
		_ = s.PutIsStatusBarEnabled(false)
		_ = s.PutAreBrowserAcceleratorKeysEnabled(false)
	}
	c.Resize()
	w.ready = true
	return nil
}

// onMessage receives a message from the page (see desktop.js).
func (w *webWindow) onMessage(msg string) {
	b, ok := ParseBridge(msg)
	if !ok {
		return
	}
	if b.Action == ActHidePanel {
		if w.kind == SurfacePanel {
			w.hide()
		}
		return
	}
	if w.h.cb.Select != nil {
		w.h.cb.Select(b.Action)
	}
}

func (w *webWindow) visible() bool {
	hwnd := w.hwnd.Load()
	if hwnd == 0 {
		return false
	}
	v, _, _ := procIsWindowVisible.Call(hwnd)
	i, _, _ := procIsIconic.Call(hwnd)
	return v != 0 && i == 0
}

func (w *webWindow) rect() (Placement, bool) {
	var r winRect
	if ok, _, _ := procGetWindowRect.Call(w.hwnd.Load(), uintptr(unsafe.Pointer(&r))); ok == 0 { //nolint:gosec // G103: Win32 out parameter
		return Placement{}, false
	}
	return Placement{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}, true
}

// captureNormal records the window rectangle while it is neither maximized
// nor minimized (what is saved and restored).
func (w *webWindow) captureNormal() {
	if w.maximized {
		return
	}
	if i, _, _ := procIsIconic.Call(w.hwnd.Load()); i != 0 {
		return
	}
	if p, ok := w.rect(); ok && p.W > 0 && p.H > 0 {
		w.normal = p
	}
}

func (w *webWindow) save() {
	p := w.normal
	if p.W <= 0 || p.H <= 0 {
		return
	}
	p.Maximized = w.kind == SurfaceApp && w.maximized
	w.h.remember(w.kind, p)
}

func (w *webWindow) mins() (int, int) {
	s := 1.0
	if procGetDpiForWindow.Find() == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(w.hwnd.Load()); dpi >= 96 {
			s = float64(dpi) / 96
		}
	}
	if w.kind == SurfacePanel {
		return scaled(panelMinW, s), scaled(panelMinH, s)
	}
	return scaled(appMinW, s), scaled(appMinH, s)
}

// clampToScreen brings a window that is partly or wholly off every screen
// back into view.
func (w *webWindow) clampToScreen() {
	if w.maximized {
		return
	}
	cur, ok := w.rect()
	if !ok {
		return
	}
	minW, minH := w.mins()
	want := Clamp(cur, minW, minH, workAreas())
	if want == cur {
		return
	}
	_, _, _ = procSetWindowPos.Call(w.hwnd.Load(), 0, uintptr(want.X), uintptr(want.Y), uintptr(want.W), uintptr(want.H), swpNoZOrder|swpNoActivate) //nolint:gosec // G115: screen coordinates
	w.normal = want
}

func (w *webWindow) applyTopmost() {
	after := hwndNoTopmost
	if w.h.onTop.Load() {
		after = hwndTopmost
	}
	_, _, _ = procSetWindowPos.Call(w.hwnd.Load(), after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

// foreground gives the window the keyboard focus even when another program
// owns the foreground, by briefly sharing its input queue.
func (w *webWindow) foreground() {
	hwnd := w.hwnd.Load()
	fg, _, _ := procGetForegroundWindow.Call()
	var fgThread uintptr
	if fg != 0 {
		fgThread, _, _ = procGetWindowThreadProc.Call(fg, 0)
	}
	cur := uintptr(windows.GetCurrentThreadId())
	attached := false
	if fgThread != 0 && fgThread != cur {
		r, _, _ := procAttachThreadInput.Call(cur, fgThread, 1)
		attached = r != 0
	}
	_, _, _ = procBringWindowToTop.Call(hwnd)
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	if attached {
		_, _, _ = procAttachThreadInput.Call(cur, fgThread, 0)
	}
	if w.ready {
		w.chromium.Focus()
	}
}

func (w *webWindow) hide() {
	if hwnd := w.hwnd.Load(); hwnd != 0 {
		w.captureNormal()
		w.save()
		_, _, _ = procShowWindow.Call(hwnd, swHide)
		if w.ready {
			_ = w.chromium.Hide()
		}
	}
}

func (w *webWindow) showPanel(url string, focusIdea bool) {
	if !w.ready {
		return
	}
	if w.loaded != url {
		target := url
		if focusIdea {
			target += "?focus=idea"
		}
		w.chromium.Navigate(target)
		w.loaded = url
	} else {
		w.chromium.Eval(fmt.Sprintf("window.stkrPanelShown&&window.stkrPanelShown(%t)", focusIdea))
	}
	w.clampToScreen()
	w.applyTopmost()
	_, _, _ = procShowWindow.Call(w.hwnd.Load(), swShowNormal)
	_ = w.chromium.Show()
	w.applyTopmost()
	w.foreground()
}

func (w *webWindow) showApp(url string) {
	if !w.ready {
		return
	}
	w.chromium.Navigate(url)
	w.loaded = url
	w.clampToScreen()
	cmd := uintptr(swShow)
	if i, _, _ := procIsIconic.Call(w.hwnd.Load()); i != 0 {
		cmd = swRestore
	} else if !w.shown && w.maximized {
		cmd = swMaximize
	}
	w.shown = true
	_, _, _ = procShowWindow.Call(w.hwnd.Load(), cmd)
	_ = w.chromium.Show()
	w.foreground()
}

// handle processes a window message on the window thread. handled=false
// passes it to DefWindowProc.
func (w *webWindow) handle(hwnd windows.HWND, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case wmWinCall:
		if w.ready {
			w.drain()
		}
		return 0, true
	case wmSize:
		switch wp {
		case sizeMaximized:
			w.maximized = true
		case sizeRestored:
			w.maximized = false
		}
		if w.chromium != nil {
			w.chromium.Resize()
		}
		if wp != sizeMinimized {
			w.captureNormal()
		}
		return 0, true
	case wmMove, wmMoving:
		if w.chromium != nil {
			_ = w.chromium.NotifyParentWindowPositionChanged()
		}
		w.captureNormal()
		return 0, true
	case wmGetMinMax:
		mm := (*minMaxInfo)(ptrFrom(lp))
		minW, minH := w.mins()
		mm.PtMinTrackSize = point{X: int32(minW), Y: int32(minH)} //nolint:gosec // G115: small window sizes
		return 0, true
	case wmExitSizeMove:
		w.captureNormal()
		w.save()
		return 0, true
	case wmActivate:
		if wp&0xFFFF != 0 && w.ready {
			w.chromium.Focus()
		}
		return 0, false
	case wmDpiChanged:
		r := (*winRect)(ptrFrom(lp))
		_, _, _ = procSetWindowPos.Call(uintptr(hwnd), 0, uintptr(r.Left), uintptr(r.Top), //nolint:gosec // G115: screen coordinates
			uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate) //nolint:gosec // G115: screen coordinates
		return 0, true
	case wmClose:
		if w.kind == SurfacePanel {
			w.hide() // closing the panel hides it; Quit is in the tray menu
			return 0, true
		}
		w.captureNormal()
		w.save()
		return 0, false // DefWindowProc destroys the app window
	case wmForceClose:
		if !w.ready {
			// WebView2 is still starting in a nested message loop; destroying
			// the window now would break it. run() destroys it right after.
			w.quitting = true
			return 0, true
		}
		_, _, _ = procDestroyWindow.Call(uintptr(hwnd))
		return 0, true
	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0, true
	case wmNCDestroy:
		webWindows.Delete(uintptr(hwnd))
		return 0, false
	}
	return 0, false
}

func webWndProc(hwnd windows.HWND, msg uint32, wp, lp uintptr) (ret uintptr) {
	if v, ok := webWindows.Load(uintptr(hwnd)); ok {
		w := v.(*webWindow)
		defer func() {
			if r := recover(); r != nil {
				w.h.log.Error("window message failed", "window", string(w.kind), "panic", fmt.Sprint(r))
				ret = 0
			}
		}()
		if r, handled := w.handle(hwnd, msg, wp, lp); handled {
			return r
		}
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return r
}
