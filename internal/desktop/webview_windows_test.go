//go:build windows

package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/chris-reynier/stkr/internal/desktop/edge"
)

// TestEmbedFailureDoesNotEndTheProcess is the regression test for the
// upstream go-webview2 behaviour (log.Fatalf when the WebView2 environment
// cannot be created), which would have killed the tray and the hosted server.
// The data folder is an existing file, so the environment cannot be created
// whether or not the WebView2 runtime is installed; Embed must report the
// failure. The window is never shown.
func TestEmbedFailureDoesNotEndTheProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
			defer windows.CoUninitialize()
		}
		// A hidden window of a system class: never visible, no class to register.
		hwnd, _, cerr := procCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(u16("STATIC"))), uintptr(unsafe.Pointer(u16("stkr test"))), //nolint:gosec // G103: Win32 string arguments
			0, 0, 0, 10, 10, 0, 0, 0, 0)
		if hwnd == 0 {
			done <- result{err: cerr}
			return
		}
		defer procDestroyWindow.Call(hwnd) //nolint:errcheck // test cleanup
		c := edge.NewChromium()
		c.DataPath = file
		c.EmbedTimeout = 10 * time.Second
		ok := c.Embed(hwnd)
		done <- result{ok: ok, err: c.Err()}
	}()
	select {
	case r := <-done:
		if r.ok {
			t.Fatal("Embed succeeded with a file as the data folder")
		}
		if r.err == nil {
			t.Fatal("Embed failed without a reason")
		}
		t.Logf("Embed failed as expected: %v", r.err)
	case <-time.After(60 * time.Second):
		t.Fatal("Embed did not return")
	}
}
