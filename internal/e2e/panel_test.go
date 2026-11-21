package e2e

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeP12Panel checks that the panel works in a plain browser (no
// desktop app needed): the page and its assets load, a switch from it is
// recorded, an idea is captured and the hotkey setting is validated.
func TestSmokeP12Panel(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if _, _, code := run(t, home, "ctx", "add", "acme", "--label", "ACME portal", "--kind", "project"); code != 0 {
		t.Fatal("ctx add failed")
	}
	base, stop := startServe(t, home)
	defer func() { _ = stop() }()

	code, b := call(t, "GET", base+"/ui/panel")
	page := string(b)
	if code != 200 {
		t.Fatalf("panel: %d", code)
	}
	for _, want := range []string{"Nothing running", "ACME portal", `value="acme"`, `id="idea"`, "Agents waiting: 0", "Open STKR", "/static/panel.css", "/static/desktop.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("panel page does not contain %q", want)
		}
	}
	for _, asset := range []string{"/static/panel.css", "/static/desktop.js", "/static/htmx.min.js"} {
		if code, b := call(t, "GET", base+asset); code != 200 || len(b) == 0 {
			t.Errorf("%s: %d", asset, code)
		}
	}
	if code, b := call(t, "GET", base+"/ui/panel/live"); code != 200 || strings.Contains(string(b), "<html") {
		t.Errorf("live fragment: %d", code)
	}

	// Switch from the panel: the form posts and goes back to the panel.
	resp := postForm(t, base, "/ui/switch", url.Values{"next": {"/ui/panel"}, "ctx": {"acme"}})
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/ui/panel" {
		t.Fatalf("switch: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, b = call(t, "GET", base+"/ui/panel")
	if page = string(b); !strings.Contains(page, "<strong>ACME portal</strong>") || !strings.Contains(page, "Stop</button>") {
		t.Errorf("panel does not show the running context after the switch")
	}
	if out, _, code := run(t, home, "now"); code != 0 || !strings.Contains(out, "ACME portal") {
		t.Errorf("now: %d %q", code, out)
	}

	// Capture an idea and stop from the panel.
	resp = postForm(t, base, "/ui/ideas/add", url.Values{"next": {"/ui/panel"}, "text": {"idea from the panel"}})
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/ui/panel" {
		t.Fatalf("idea: %d", resp.StatusCode)
	}
	if out, _, code := run(t, home, "ideas"); code != 0 || !strings.Contains(out, "idea from the panel") {
		t.Errorf("ideas: %d %q", code, out)
	}
	if resp = postForm(t, base, "/ui/stop", url.Values{"next": {"/ui/panel"}}); resp.StatusCode != 303 {
		t.Fatalf("stop: %d", resp.StatusCode)
	}
	if code, _ := call(t, "POST", base+"/ui/import/clipboard", "Origin", "https://evil.example"); code != 403 {
		t.Errorf("import from a foreign origin: %d", code)
	}

	// The new configuration keys.
	if _, errOut, code := run(t, home, "config", "set", "desktop.hotkey", "Ctrl+Shift+K"); code != 0 {
		t.Fatalf("set hotkey: %d %s", code, errOut)
	}
	if _, _, code := run(t, home, "config", "set", "desktop.hotkey", "Space"); code == 0 {
		t.Error("a hotkey without a modifier was accepted")
	}
	if _, _, code := run(t, home, "config", "set", "desktop.hotkey", ""); code != 0 {
		t.Error("an empty hotkey (off) was rejected")
	}
	if out, _, code := run(t, home, "config", "show"); code != 0 || !strings.Contains(out, "panel_on_top: true") || !strings.Contains(out, "open_panel_on_start: false") {
		t.Errorf("config show: %d %q", code, out)
	}
}
