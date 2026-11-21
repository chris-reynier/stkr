//go:build windows

package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procMessageBoxW = user32.NewProc("MessageBoxW")

const (
	mbYesNo         = 0x00000004
	mbIconQuestion  = 0x00000020
	mbDefButton2    = 0x00000100
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
	idYes           = 6
)

// platformConfirm asks a yes/no question in a message box above other
// windows. "No" is the default button.
func platformConfirm(title, text string) bool {
	t, err1 := windows.UTF16PtrFromString(title)
	m, err2 := windows.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return false
	}
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), //nolint:gosec // G103: Win32 string arguments
		mbYesNo|mbIconQuestion|mbDefButton2|mbSetForeground|mbTopMost)
	return r == idYes
}
