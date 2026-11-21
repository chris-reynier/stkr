//go:build windows

package edge

// Added for STKR (not in upstream go-webview2): handlers for NavigationStarting
// and NewWindowRequested, so the application can refuse navigation to other
// origins and keep pop-up windows out of the process. The vtable layouts
// follow WebView2.h (ICoreWebView2NavigationStartingEventArgs and
// ICoreWebView2NewWindowRequestedEventArgs).

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- NavigationStarting ----

type _ICoreWebView2NavigationStartingEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2NavigationStartingEventHandler struct {
	vtbl *_ICoreWebView2NavigationStartingEventHandlerVtbl
	impl iCoreWebView2NavigationStartingEventHandlerImpl
}

type iCoreWebView2NavigationStartingEventHandlerImpl interface {
	_IUnknownImpl
	NavigationStarting(sender *ICoreWebView2, args *iCoreWebView2NavigationStartingEventArgs) uintptr
}

func navStartQueryInterface(this *iCoreWebView2NavigationStartingEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}

func navStartAddRef(this *iCoreWebView2NavigationStartingEventHandler) uintptr {
	return this.impl.AddRef()
}

func navStartRelease(this *iCoreWebView2NavigationStartingEventHandler) uintptr {
	return this.impl.Release()
}

func navStartInvoke(this *iCoreWebView2NavigationStartingEventHandler, sender *ICoreWebView2, args *iCoreWebView2NavigationStartingEventArgs) uintptr {
	return this.impl.NavigationStarting(sender, args)
}

var navStartFn = _ICoreWebView2NavigationStartingEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(navStartQueryInterface),
		NewComProc(navStartAddRef),
		NewComProc(navStartRelease),
	},
	NewComProc(navStartInvoke),
}

func newICoreWebView2NavigationStartingEventHandler(impl iCoreWebView2NavigationStartingEventHandlerImpl) *iCoreWebView2NavigationStartingEventHandler {
	return &iCoreWebView2NavigationStartingEventHandler{vtbl: &navStartFn, impl: impl}
}

type _ICoreWebView2NavigationStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetCancel          ComProc
	PutCancel          ComProc
	GetNavigationId    ComProc
}

type iCoreWebView2NavigationStartingEventArgs struct {
	vtbl *_ICoreWebView2NavigationStartingEventArgsVtbl
}

func (i *iCoreWebView2NavigationStartingEventArgs) uri() string {
	var p *uint16
	r, _, _ := i.vtbl.GetUri.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&p)))
	if int32(r) < 0 || p == nil { //nolint:gosec // G115: HRESULT
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func (i *iCoreWebView2NavigationStartingEventArgs) cancel() {
	_, _, _ = i.vtbl.PutCancel.Call(uintptr(unsafe.Pointer(i)), 1)
}

// NavigationStarting is called before every navigation. A callback that
// returns true cancels it.
func (e *Chromium) NavigationStarting(_ *ICoreWebView2, args *iCoreWebView2NavigationStartingEventArgs) uintptr {
	if e.NavigationStartingCallback != nil && e.NavigationStartingCallback(args.uri()) {
		args.cancel()
	}
	return 0
}

// ---- NewWindowRequested ----

type _ICoreWebView2NewWindowRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type iCoreWebView2NewWindowRequestedEventHandler struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventHandlerVtbl
	impl iCoreWebView2NewWindowRequestedEventHandlerImpl
}

type iCoreWebView2NewWindowRequestedEventHandlerImpl interface {
	_IUnknownImpl
	NewWindowRequested(sender *ICoreWebView2, args *iCoreWebView2NewWindowRequestedEventArgs) uintptr
}

func newWinQueryInterface(this *iCoreWebView2NewWindowRequestedEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}

func newWinAddRef(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return this.impl.AddRef()
}

func newWinRelease(this *iCoreWebView2NewWindowRequestedEventHandler) uintptr {
	return this.impl.Release()
}

func newWinInvoke(this *iCoreWebView2NewWindowRequestedEventHandler, sender *ICoreWebView2, args *iCoreWebView2NewWindowRequestedEventArgs) uintptr {
	return this.impl.NewWindowRequested(sender, args)
}

var newWinFn = _ICoreWebView2NewWindowRequestedEventHandlerVtbl{
	_IUnknownVtbl{
		NewComProc(newWinQueryInterface),
		NewComProc(newWinAddRef),
		NewComProc(newWinRelease),
	},
	NewComProc(newWinInvoke),
}

func newICoreWebView2NewWindowRequestedEventHandler(impl iCoreWebView2NewWindowRequestedEventHandlerImpl) *iCoreWebView2NewWindowRequestedEventHandler {
	return &iCoreWebView2NewWindowRequestedEventHandler{vtbl: &newWinFn, impl: impl}
}

type _ICoreWebView2NewWindowRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	PutNewWindow       ComProc
	GetNewWindow       ComProc
	PutHandled         ComProc
	GetHandled         ComProc
	GetIsUserInitiated ComProc
	GetDeferral        ComProc
	GetWindowFeatures  ComProc
}

type iCoreWebView2NewWindowRequestedEventArgs struct {
	vtbl *_ICoreWebView2NewWindowRequestedEventArgsVtbl
}

func (i *iCoreWebView2NewWindowRequestedEventArgs) uri() string {
	var p *uint16
	r, _, _ := i.vtbl.GetUri.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&p)))
	if int32(r) < 0 || p == nil { //nolint:gosec // G115: HRESULT
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func (i *iCoreWebView2NewWindowRequestedEventArgs) handled() {
	_, _, _ = i.vtbl.PutHandled.Call(uintptr(unsafe.Pointer(i)), 1)
}

// NewWindowRequested is called when a page wants a new window (window.open,
// target=_blank). The request is always marked handled, so WebView2 opens no
// window of its own; the callback decides what to do with the address.
func (e *Chromium) NewWindowRequested(_ *ICoreWebView2, args *iCoreWebView2NewWindowRequestedEventArgs) uintptr {
	args.handled()
	if e.NewWindowCallback != nil {
		e.NewWindowCallback(args.uri())
	}
	return 0
}
