//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/chris-reynier/stkr/internal/domain"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procKeybdEvent               = user32.NewProc("keybd_event")
	procGetCurrentThreadID       = kernel32.NewProc("GetCurrentThreadId")
)

const (
	swRestore      = 9
	vkMenu         = 0x12 // ALT
	keyeventfKeyUp = 0x2
)

// The EnumWindows callback is created once (Go never frees callbacks), so
// enumeration state is package-level and guarded by enumMu.
var (
	enumMu       sync.Mutex
	enumNeedle   string
	enumMatches  []windows.HWND
	enumCallback = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		if visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); visible == 0 {
			return 1
		}
		if title := windowTitle(hwnd); title != "" && strings.Contains(strings.ToLower(title), enumNeedle) {
			enumMatches = append(enumMatches, hwnd)
		}
		return 1
	})
)

func windowTitle(hwnd windows.HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if n == 0 || n > 1<<12 {
		return ""
	}
	buf := make([]uint16, n+1)
	got, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) //nolint:gosec // G103: pointer to our own buffer for the Win32 call
	return string(utf16.Decode(buf[:got]))
}

func findWindows(needle string) []windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumNeedle = strings.ToLower(needle)
	enumMatches = nil
	_, _, _ = procEnumWindows.Call(enumCallback, 0)
	out := enumMatches
	enumMatches = nil
	return out
}

type winFocus struct{}

// NewWindows returns the Windows implementation (EnumWindows and
// SetForegroundWindow).
func NewWindows() Windows { return winFocus{} }

func (winFocus) Focus(ctx context.Context, titleContains string, wait time.Duration) error {
	if strings.TrimSpace(titleContains) == "" {
		return domain.New(domain.ErrInvalid, "empty window title")
	}
	deadline := time.Now().Add(wait) // poll budget in real time, not domain time
	for {
		if hs := findWindows(titleContains); len(hs) > 0 {
			return bringToFront(hs[0])
		}
		if wait <= 0 || time.Now().After(deadline) {
			return domain.New(domain.ErrNotFound, fmt.Sprintf("no window title contains %q", titleContains))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// bringToFront restores and activates hwnd. Windows refuses foreground
// changes from background processes, so this attaches to the foreground
// window's input queue and taps ALT, as a best effort.
func bringToFront(hwnd windows.HWND) error {
	if iconic, _, _ := procIsIconic.Call(uintptr(hwnd)); iconic != 0 {
		_, _, _ = procShowWindow.Call(uintptr(hwnd), swRestore)
	}
	cur, _, _ := procGetCurrentThreadID.Call()
	var attached bool
	var fgThread uintptr
	if fg, _, _ := procGetForegroundWindow.Call(); fg != 0 {
		fgThread, _, _ = procGetWindowThreadProcessID.Call(fg, 0)
		if fgThread != 0 && fgThread != cur {
			r, _, _ := procAttachThreadInput.Call(cur, fgThread, 1)
			attached = r != 0
		}
	}
	_, _, _ = procKeybdEvent.Call(vkMenu, 0, 0, 0)
	_, _, _ = procKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
	_, _, _ = procBringWindowToTop.Call(uintptr(hwnd))
	r, _, callErr := procSetForegroundWindow.Call(uintptr(hwnd))
	if attached {
		_, _, _ = procAttachThreadInput.Call(cur, fgThread, 0)
	}
	if r == 0 {
		var errno syscall.Errno
		if errors.As(callErr, &errno) && errno != 0 {
			return fmt.Errorf("SetForegroundWindow: %w", errno)
		}
		return errors.New("SetForegroundWindow was refused")
	}
	return nil
}
