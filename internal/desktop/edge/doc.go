//go:build windows

// Package edge is a modified copy of github.com/jchv/go-webview2/pkg/edge
// (MIT, see LICENSE), the Go wrapper around the WebView2 COM interfaces.
//
// Changes made for STKR:
//   - A failed WebView2 environment or controller no longer ends the process
//     with log.Fatalf: Embed returns false and Err says why. A window that
//     cannot be created must never take the tray and the hosted server down.
//   - Embed waits at most EmbedTimeout (30 s) for WebView2 instead of for ever.
//   - Other log.Fatal calls (Eval, WebResourceRequested) return instead.
//   - NavigationStartingCallback can cancel a navigation, and every request for
//     a new window is suppressed and reported to NewWindowCallback (navigation.go).
//
// The WebView2 loader (webviewloader) and the internal w32 helpers are used
// as upstream has them; only this package was copied because the changes
// needed to touch it. To update, diff against the upstream version named in
// go.mod and re-apply the changes above.
package edge
