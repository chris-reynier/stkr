//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/chris-reynier/stkr/internal/hotkey"
)

const platformSupported = true

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx         = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procCreateIconIndirect       = user32.NewProc("CreateIconIndirect")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procSetDpiAwarenessContext   = user32.NewProc("SetProcessDpiAwarenessContext")
	procRegisterHotKey           = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey         = user32.NewProc("UnregisterHotKey")
	procShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	procCreateDIBSection         = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap             = gdi32.NewProc("CreateBitmap")
	procDeleteObject             = gdi32.NewProc("DeleteObject")
)

const (
	trayClassName = "STKRDesktopTray"

	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmNull        = 0x0000
	wmContextMenu = 0x007B
	wmApp         = 0x8000
	wmTrayIcon    = wmApp + 1 // icon callback
	wmTrayCall    = wmApp + 2 // run queued calls on the tray thread
	wmTrayShow    = wmApp + 3 // another launch asks to show
	wmHotKey      = 0x0312
	hotkeyID      = 1
	modNoRepeat   = 0x4000

	ninSelect    = 0x0400
	ninKeySelect = 0x0401

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage  = 0x01
	nifIcon     = 0x02
	nifTip      = 0x04
	nifInfo     = 0x10
	nifShowTip  = 0x80
	niifInfo    = 0x01
	niifError   = 0x03
	niVersion4  = 4
	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmBottomAlign = 0x0020

	smCxSmIcon = 49

	swShowNormal = 1
)

type point struct{ X, Y int32 }

type winMsg struct {
	HWnd     windows.HWND
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GUIDItem         windows.GUID
	HBalloonIcon     windows.Handle
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  windows.Handle
	HbmColor windows.Handle
}

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// winTray is the Shell_NotifyIconW tray. All Win32 calls run on the thread
// of Run; other goroutines queue calls and wake it with wmTrayCall.
type winTray struct {
	name string

	mu    sync.Mutex
	hwnd  windows.HWND
	queue []func()

	// Owned by the tray thread.
	h              Handler
	taskbarCreated uint32
	icon           windows.Handle
	iconState      IconState
	tip            string
	added          bool
	menuIDs        map[uint32]string
	hotkeyOn       bool
}

// The window procedure is created once per process (Go never frees
// callbacks) and forwards to the single tray.
var (
	activeTray  *winTray
	wndProcAddr = windows.NewCallback(func(hwnd windows.HWND, msg uint32, wp, lp uintptr) uintptr {
		if t := activeTray; t != nil {
			if r, ok := t.wndProc(hwnd, msg, wp, lp); ok {
				return r
			}
		}
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
		return r
	})
)

func newPlatformTray(name string) (Tray, error) {
	if procSetDpiAwarenessContext.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4: sharp icon and menu.
		_, _, _ = procSetDpiAwarenessContext.Call(^uintptr(3))
	}
	return &winTray{name: name}, nil
}

func u16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	if err != nil {
		p, _ = windows.UTF16PtrFromString("")
	}
	return p
}

// copyU16 copies s into dst, truncated so that the terminating NUL fits.
func copyU16(dst []uint16, s string) {
	enc := utf16.Encode([]rune(strings.ReplaceAll(s, "\x00", "")))
	if len(enc) > len(dst)-1 {
		enc = enc[:len(dst)-1]
	}
	n := copy(dst, enc)
	dst[n] = 0
}

func (t *winTray) Run(h Handler) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.h = h
	activeTray = t

	var inst windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &inst); err != nil {
		return fmt.Errorf("module handle: %w", err)
	}
	wc := wndClassEx{LpfnWndProc: wndProcAddr, HInstance: inst, LpszClassName: u16(trayClassName)}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) { //nolint:gosec // G103: Win32 call with our own struct
		return fmt.Errorf("register window class: %w", err)
	}
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated")))) //nolint:gosec // G103: Win32 string argument
	t.taskbarCreated = uint32(r)                                                               //nolint:gosec // G115: message ids are 16-bit values
	// A hidden top-level window (not message-only): only top-level windows
	// receive the TaskbarCreated broadcast after an Explorer restart.
	hwnd, _, err := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(trayClassName))), uintptr(unsafe.Pointer(u16(t.name))), //nolint:gosec // G103: Win32 string arguments
		0, 0, 0, 0, 0, 0, 0, uintptr(inst), 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %w", err)
	}
	t.mu.Lock()
	t.hwnd = windows.HWND(hwnd)
	t.mu.Unlock()
	t.addIcon()
	h.Ready()
	t.runQueue()

	var m winMsg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0) //nolint:gosec // G103: Win32 out parameter
		switch int32(r) {                                                       //nolint:gosec // G115: GetMessage returns a BOOL (-1, 0 or nonzero)
		case -1:
			return fmt.Errorf("message loop: %w", err)
		case 0:
			return nil
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m))) //nolint:gosec // G103: Win32 argument
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m))) //nolint:gosec // G103: Win32 argument
	}
}

// post queues fn for the tray thread.
func (t *winTray) post(fn func()) {
	t.mu.Lock()
	t.queue = append(t.queue, fn)
	hwnd := t.hwnd
	t.mu.Unlock()
	if hwnd != 0 {
		_, _, _ = procPostMessageW.Call(uintptr(hwnd), wmTrayCall, 0, 0)
	}
}

func (t *winTray) runQueue() {
	t.mu.Lock()
	q := t.queue
	t.queue = nil
	t.mu.Unlock()
	for _, fn := range q {
		fn()
	}
}

func (t *winTray) SetIcon(is IconState, tooltip string) {
	t.post(func() { t.applyIcon(is, tooltip) })
}

func (t *winTray) Notify(n Notice) {
	t.post(func() {
		d := t.nid()
		d.UFlags = nifInfo
		copyU16(d.SzInfo[:], n.Text)
		copyU16(d.SzInfoTitle[:], n.Title)
		d.DwInfoFlags = niifInfo
		if n.Error {
			d.DwInfoFlags = niifError
		}
		notifyIcon(nimModify, &d)
	})
}

// SetHotkey registers (or replaces) the global hotkey on the tray thread: a
// hotkey belongs to the thread that registered it and arrives as WM_HOTKEY in
// its message loop.
func (t *winTray) SetHotkey(k hotkey.Key, done func(error)) {
	t.post(func() {
		if t.hotkeyOn {
			_, _, _ = procUnregisterHotKey.Call(uintptr(t.hwnd), hotkeyID)
			t.hotkeyOn = false
		}
		r, _, err := procRegisterHotKey.Call(uintptr(t.hwnd), hotkeyID, uintptr(k.Mods|modNoRepeat), uintptr(k.VK))
		if r == 0 {
			done(fmt.Errorf("register hotkey: %w", err))
			return
		}
		t.hotkeyOn = true
		done(nil)
	})
}

func (t *winTray) Quit() {
	t.mu.Lock()
	hwnd := t.hwnd
	t.mu.Unlock()
	if hwnd != 0 {
		_, _, _ = procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	}
}

func (t *winTray) nid() notifyIconData {
	d := notifyIconData{HWnd: t.hwnd, UID: 1}
	d.CbSize = uint32(unsafe.Sizeof(d))
	return d
}

func notifyIcon(op uintptr, d *notifyIconData) bool {
	r, _, _ := procShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(d))) //nolint:gosec // G103: Win32 struct argument
	return r != 0
}

// addIcon (re)creates the icon, also after Explorer restarted.
func (t *winTray) addIcon() {
	if t.icon == 0 {
		t.icon = makeIcon(IconFor(nil))
	}
	d := t.nid()
	d.UFlags = nifMessage | nifIcon | nifTip | nifShowTip
	d.UCallbackMessage = wmTrayIcon
	d.HIcon = t.icon
	copyU16(d.SzTip[:], t.tip)
	t.added = notifyIcon(nimAdd, &d)
	d.UVersion = niVersion4
	notifyIcon(nimSetVersion, &d)
}

func (t *winTray) applyIcon(is IconState, tip string) {
	if t.icon != 0 && is == t.iconState && tip == t.tip {
		return
	}
	icon := makeIcon(is)
	if icon == 0 {
		return
	}
	old := t.icon
	t.icon, t.iconState, t.tip = icon, is, tip
	d := t.nid()
	d.UFlags = nifIcon | nifTip | nifShowTip
	d.HIcon = icon
	copyU16(d.SzTip[:], tip)
	if !notifyIcon(nimModify, &d) && !t.added {
		t.addIcon()
	}
	if old != 0 {
		_, _, _ = procDestroyIcon.Call(uintptr(old))
	}
}

func (t *winTray) wndProc(hwnd windows.HWND, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch {
	case msg == wmTrayIcon:
		switch uint32(lp & 0xFFFF) {
		case ninSelect, ninKeySelect:
			t.h.Activate()
		case wmContextMenu:
			t.showMenu(hwnd)
		}
		return 0, true
	case msg == wmTrayCall:
		t.runQueue()
		return 0, true
	case msg == wmTrayShow:
		if wp == 1 { // stkrw --quit
			t.h.Select(ActQuit)
			return 0, true
		}
		t.h.Show()
		return 0, true
	case msg == wmHotKey:
		t.h.Hotkey()
		return 0, true
	case msg == t.taskbarCreated && msg != 0:
		t.addIcon()
		t.applyIcon(t.iconState, t.tip)
		return 0, true
	case msg == wmDestroy:
		if t.hotkeyOn {
			_, _, _ = procUnregisterHotKey.Call(uintptr(hwnd), hotkeyID)
			t.hotkeyOn = false
		}
		d := t.nid()
		notifyIcon(nimDelete, &d)
		if t.icon != 0 {
			_, _, _ = procDestroyIcon.Call(uintptr(t.icon))
			t.icon = 0
		}
		t.mu.Lock()
		t.hwnd = 0
		t.mu.Unlock()
		_, _, _ = procPostQuitMessage.Call(0)
		return 0, true
	}
	return 0, false
}

func (t *winTray) showMenu(hwnd windows.HWND) {
	t.menuIDs = map[uint32]string{}
	next := uint32(1)
	menu := t.buildMenu(t.h.Menu(), &next)
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu) //nolint:errcheck // best effort cleanup
	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))) //nolint:gosec // G103: Win32 out parameter
	// Required so the menu closes when the user clicks elsewhere.
	_, _, _ = procSetForegroundWindow.Call(uintptr(hwnd))
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, tpmRightButton|tpmReturnCmd|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), uintptr(hwnd), 0) //nolint:gosec // G115: screen coordinates are passed as int in the Win32 ABI
	_, _, _ = procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
	if id, ok := t.menuIDs[uint32(cmd)]; ok && cmd != 0 { //nolint:gosec // G115: menu ids are small numbers we assigned
		t.h.Select(id)
	}
}

func (t *winTray) buildMenu(items []MenuItem, next *uint32) uintptr {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return 0
	}
	for _, it := range items {
		if it.Separator {
			_, _, _ = procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		label := uintptr(unsafe.Pointer(u16(strings.ReplaceAll(it.Label, "&", "&&")))) //nolint:gosec // G103: Win32 string argument
		flags := uintptr(mfString)
		if it.Disabled {
			flags |= mfGrayed
		}
		if it.Checked {
			flags |= mfChecked
		}
		if len(it.Children) > 0 {
			sub := t.buildMenu(it.Children, next)
			_, _, _ = procAppendMenuW.Call(menu, flags|mfPopup, sub, label)
			continue
		}
		id := *next
		*next++
		if it.ID != "" {
			t.menuIDs[id] = it.ID
		}
		_, _, _ = procAppendMenuW.Call(menu, flags, uintptr(id), label)
	}
	return menu
}

// makeIcon converts the drawn icon to an HICON (32-bit colour with alpha and
// an empty mask) at the system small-icon size.
func makeIcon(is IconState) windows.Handle {
	size, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	if size < 16 || size > 256 {
		size = 16
	}
	img := DrawIcon(is, int(size))
	n := int(size)
	bi := bitmapInfoHeader{BiWidth: int32(n), BiHeight: -int32(n), BiPlanes: 1, BiBitCount: 32}
	bi.BiSize = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	hbm, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0) //nolint:gosec // G103: Win32 out parameter
	if hbm == 0 || bits == nil {
		return 0
	}
	defer procDeleteObject.Call(hbm)         //nolint:errcheck // the icon keeps its own copy
	px := unsafe.Slice((*byte)(bits), n*n*4) //nolint:gosec // G103: the DIB section has exactly n*n 32-bit pixels
	copyBGRA(px, img)
	maskBits := make([]byte, ((n+15)/16)*2*n)
	mask, _, _ := procCreateBitmap.Call(uintptr(n), uintptr(n), 1, 1, uintptr(unsafe.Pointer(&maskBits[0]))) //nolint:gosec // G103: Win32 buffer argument
	if mask == 0 {
		return 0
	}
	defer procDeleteObject.Call(mask) //nolint:errcheck // the icon keeps its own copy
	ii := iconInfo{FIcon: 1, HbmMask: windows.Handle(mask), HbmColor: windows.Handle(hbm)}
	h, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii))) //nolint:gosec // G103: Win32 struct argument
	return windows.Handle(h)
}

// copyBGRA writes img as straight-alpha BGRA rows.
func copyBGRA(dst []byte, img *image.RGBA) {
	b := img.Bounds()
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.RGBAAt(x, y)).(color.NRGBA)
			dst[i], dst[i+1], dst[i+2], dst[i+3] = c.B, c.G, c.R, c.A
			i += 4
		}
	}
}

func platformAcquireInstance(name string) (func(), bool, error) {
	h, err := windows.CreateMutex(nil, false, u16(MutexPrefix+name))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(h)
		return func() {}, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("create mutex: %w", err)
	}
	return func() { _ = windows.CloseHandle(h) }, false, nil
}

func platformSignalInstance(name string, quit bool) error {
	hwnd, _, err := procFindWindowW.Call(uintptr(unsafe.Pointer(u16(trayClassName))), uintptr(unsafe.Pointer(u16(name)))) //nolint:gosec // G103: Win32 string arguments
	if hwnd == 0 {
		return fmt.Errorf("find running instance: %w", err)
	}
	var wp uintptr
	if quit {
		wp = 1
	}
	// Let the running instance bring the browser or panel to the front.
	_, _, _ = procAllowSetForegroundWindow.Call(uintptr(^uint32(0)))
	_, _, _ = procPostMessageW.Call(hwnd, wmTrayShow, wp, 0)
	return nil
}

func platformOpen(target string) error {
	return windows.ShellExecute(0, u16("open"), u16(target), nil, nil, swShowNormal)
}
