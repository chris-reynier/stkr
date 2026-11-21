package desktop

import (
	"net/url"
	"strings"
)

// Surface is one of the two windows that show the web UI.
type Surface string

// The windows.
const (
	SurfacePanel Surface = "panel" // compact, always on top, no taskbar button
	SurfaceApp   Surface = "app"   // normal resizable window with the full UI
)

// Windows shows the panel and the app window. The real implementation runs
// each window in WebView2 on its own thread (webview_windows.go); tests use a
// fake. All methods may be called from any goroutine and never block on the
// windows.
type Windows interface {
	// Available reports why windows cannot be shown (WebView2 is not
	// installed), or nil.
	Available() error
	// ShowPanel shows the panel, loading url the first time. With focusIdea
	// the idea box gets the keyboard focus.
	ShowPanel(url string, focusIdea bool)
	// TogglePanel hides the panel when it is visible, else shows it.
	TogglePanel(url string, focusIdea bool)
	// HidePanel hides the panel.
	HidePanel()
	// ShowApp opens the app window (or reuses it) and loads url in it.
	ShowApp(url string)
	// SetPanelOnTop turns the always-on-top state of the panel on or off.
	SetPanelOnTop(on bool)
	// Close destroys both windows.
	Close()
}

// WindowCallbacks are what the windows call back into the app with.
type WindowCallbacks struct {
	// Select runs an action id (see Bridge.Action), for example a link
	// clicked in the panel. It does not block.
	Select func(id string)
	// Failed reports that a window could not be created (WebView2 broke).
	Failed func(err error)
}

// WindowsFactory builds the windows once the app exists.
type WindowsFactory func(WindowCallbacks) Windows

// Bridge is a message the web page sends to the app through
// window.chrome.webview.postMessage (see internal/web/static/desktop.js).
type Bridge struct {
	// Action is the action id to run (see App.Select).
	Action string
}

const (
	maxBridgeLen = 2048
	prefixURL    = "url:"
	prefixApp    = "app:"
)

// ParseBridge validates a page message. Messages are untrusted: a page the
// user opened in the window could send anything, so only a fixed set is
// accepted: "hide", "pin", "app:/ui/<path>" (open a page of the UI in the app
// window) and "url:<http(s) address>" (open another site in the default
// browser). Anything else is ignored.
func ParseBridge(msg string) (Bridge, bool) {
	if len(msg) == 0 || len(msg) > maxBridgeLen || strings.ContainsAny(msg, "\x00\r\n") {
		return Bridge{}, false
	}
	switch msg {
	case "hide":
		return Bridge{Action: ActHidePanel}, true
	case "pin":
		return Bridge{Action: ActPinPanel}, true
	}
	if path, ok := strings.CutPrefix(msg, prefixApp); ok {
		if !strings.HasPrefix(path, "/ui/") || strings.ContainsAny(path, "\\ ") || strings.HasPrefix(path, "/ui//") {
			return Bridge{}, false
		}
		return Bridge{Action: prefixOpen + path}, true
	}
	if raw, ok := strings.CutPrefix(msg, prefixURL); ok {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return Bridge{}, false
		}
		return Bridge{Action: prefixURL + u.String()}, true
	}
	return Bridge{}, false
}
