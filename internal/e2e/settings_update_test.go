package e2e

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeP14SettingsAndUpdate drives a plain `stkr serve` (not hosted by the
// desktop app): the Updates box and the update settings are there, the
// desktop-only parts are not, the install action is refused, saved settings
// say that a restart is needed, and "Back up now" writes a backup.
func TestSmokeP14SettingsAndUpdate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	// No network: the update check is off and nothing is cached.
	if _, _, code := run(t, home, "config", "set", "update.check", "false"); code != 0 {
		t.Fatal("config set failed")
	}
	base, stop := startServe(t, home)
	defer func() { _ = stop() }()

	_, b := call(t, "GET", base+"/ui/settings")
	page := string(b)
	for _, want := range []string{`id="updates"`, "Check now", `action="/ui/update/check"`, "Back up now", `name="update.channel"`,
		`name="update.check"`, `name="update.interval_hours"`, "Restart <code>stkr serve</code>"} {
		if !strings.Contains(page, want) {
			t.Errorf("settings page does not contain %q", want)
		}
	}
	for _, hidden := range []string{"Install update", "Open data folder", "desktop.autostart", "desktop.hotkey"} {
		if strings.Contains(page, hidden) {
			t.Errorf("plain serve shows %q", hidden)
		}
	}

	// Installing from the page is impossible under plain serve.
	resp := postForm(t, base, "/ui/update/apply", url.Values{"next": {"/ui/settings"}})
	if resp.StatusCode != 303 {
		t.Fatalf("apply: %d", resp.StatusCode)
	}
	var flash string
	for _, c := range resp.Cookies() {
		if c.Name == "stkr_flash" {
			flash, _ = url.QueryUnescape(c.Value)
		}
	}
	if !strings.HasPrefix(flash, "e") || !strings.Contains(flash, "stkr update") {
		t.Errorf("apply flash = %q", flash)
	}
	if code, _ := call(t, "POST", base+"/ui/update/apply", "Origin", "https://evil.example"); code != 403 {
		t.Errorf("apply from a foreign origin: %d", code)
	}

	// A saved setting is kept in the file and needs a restart of serve.
	if resp := postForm(t, base, "/ui/settings/config", url.Values{"next": {"/ui/settings"}, "update.channel": {"beta"}, "update.interval_hours": {"12"}}); resp.StatusCode != 303 {
		t.Fatalf("save: %d", resp.StatusCode)
	}
	if out, _, code := run(t, home, "config", "show"); code != 0 || !strings.Contains(out, "channel: beta") || !strings.Contains(out, "interval_hours: 12") {
		t.Errorf("config show: %d %q", code, out)
	}

	// Back up now writes a backup file.
	if resp := postForm(t, base, "/ui/settings/backup", url.Values{"next": {"/ui/settings"}}); resp.StatusCode != 303 {
		t.Fatalf("backup: %d", resp.StatusCode)
	}
	files, err := filepath.Glob(filepath.Join(home, "backups", "stkr-*.db"))
	if err != nil || len(files) == 0 {
		t.Errorf("no backup written: %v %v", files, err)
	}
	// The desktop folder action is refused here.
	if resp := postForm(t, base, "/ui/settings/open-data", url.Values{"next": {"/ui/settings"}}); resp.StatusCode != 303 {
		t.Fatalf("open-data: %d", resp.StatusCode)
	}

	// The new config key exists and does not touch Windows from the CLI.
	if _, _, code := run(t, home, "config", "set", "desktop.autostart", "false"); code != 0 {
		t.Error("desktop.autostart rejected")
	}
	if out, _, code := run(t, home, "config", "show"); code != 0 || !strings.Contains(out, "autostart: false") {
		t.Errorf("config show: %q", out)
	}
	if _, err := os.Stat(filepath.Join(home, "config.yaml")); err != nil {
		t.Error(err)
	}
}
