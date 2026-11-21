//go:build windows

package desktop

import (
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/desktop/edge"
)

// TestWebViewFailureIsReportedNotFatal is the regression test for the
// upstream go-webview2 behaviour (log.Fatalf when the WebView2 environment or
// controller cannot be created), which would have ended the tray and the
// hosted server. The completion callbacks that WebView2 calls are driven
// directly with failure results; they must record the failure, release the
// waiting Embed loop and return normally (a log.Fatalf would end this test
// binary). No window is created and nothing is shown.
func TestWebViewFailureIsReportedNotFatal(t *testing.T) {
	cases := []struct {
		name string
		call func(c *edge.Chromium)
		want string
	}{
		// 0x8007xxxx values are positive as 64-bit numbers: the upstream check
		// (int64(res) < 0) never saw them as failures.
		{"environment, access denied", func(c *edge.Chromium) { c.EnvironmentCompleted(0x80070005, nil) }, "environment"},
		{"environment, no environment", func(c *edge.Chromium) { c.EnvironmentCompleted(0, nil) }, "environment"},
		{"controller, server failed", func(c *edge.Chromium) { c.CreateCoreWebView2ControllerCompleted(0x80080005, nil) }, "controller"},
		{"controller, no controller", func(c *edge.Chromium) { c.CreateCoreWebView2ControllerCompleted(0, nil) }, "controller"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := edge.NewChromium()
			if c.Err() != nil {
				t.Fatal("a new wrapper already has an error")
			}
			tc.call(c)
			err := c.Err()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Err() = %v, want a %s failure", err, tc.want)
			}
		})
	}
}
