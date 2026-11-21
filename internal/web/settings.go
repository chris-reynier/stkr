package web

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/update"
)

func init() {
	Register(Page{ID: "settings", Title: "Settings", Path: "/ui/settings", Order: 90, Mount: mountSettings})
}

type field struct {
	Key, Label, Help string
	Type             string // text, number, bool, select
	Options          []string
	// Value is the current value; always empty for secrets.
	Value  string
	Secret bool
	// Set tells whether a secret has a value.
	Set bool
}

type section struct {
	Title  string
	Fields []field
	// DesktopOnly sections are shown (and saved) only when the desktop app
	// hosts the server.
	DesktopOnly bool
}

type settingsData struct {
	Contexts []domain.Context
	Kinds    []domain.ContextKind
	Sections []section
	// Desktop is true when the desktop app hosts the server.
	Desktop bool
	Update  updateView
}

// specs lists the settings the page edits. Only these keys can be changed
// through the form; port and allowed_origins are deliberately left out.
func specs() []section {
	agent := func(prefix, title string) section {
		return section{Title: title, Fields: []field{
			{Key: prefix + ".enabled", Label: "Enabled", Type: "bool"},
			{Key: prefix + ".bin", Label: "Program", Type: "text"},
			{Key: prefix + ".args", Label: "Arguments", Type: "text", Help: "Comma-separated."},
			{Key: prefix + ".window_title", Label: "Window title contains", Type: "text"},
		}}
	}
	return []section{
		{Title: "Time", Fields: []field{
			{Key: "timezone", Label: "Time zone", Type: "text", Help: "IANA name such as Europe/Paris. Empty uses this computer's zone."},
			{Key: "day_start", Label: "Working day starts", Type: "text", Help: "HH:MM"},
			{Key: "day_end", Label: "Working day ends", Type: "text", Help: "HH:MM"},
			{Key: "close_reminder", Label: "Close-of-day reminder", Type: "text", Help: "HH:MM"},
			{Key: "idle_gap_minutes", Label: "Idle gap (minutes)", Type: "number", Help: "Longer blocks are idle-suspect until confirmed."},
			{Key: "rounding_hours", Label: "Export rounding (hours)", Type: "number", Help: "0.25 rounds exported hours to quarter hours."},
			{Key: "open_notebook_on_switch", Label: "Open the notebook when switching", Type: "bool"},
		}},
		{Title: "Paths", Fields: []field{
			{Key: "onedrive_dir", Label: "Export folder", Type: "text", Help: "Empty turns the daily export off."},
			{Key: "phone_inbox", Label: "Phone inbox folder", Type: "text", Help: "Empty turns the inbox off."},
			{Key: "ffmpeg", Label: "ffmpeg program", Type: "text"},
			{Key: "mic_device", Label: "Microphone device", Type: "text"},
			{Key: "whisper.bin", Label: "Whisper program", Type: "text"},
			{Key: "whisper.model", Label: "Whisper model file", Type: "text"},
		}},
		{Title: "Bookmarks", Fields: []field{
			{Key: "bookmarks.fetch_title", Label: "Fetch page titles", Type: "bool", Help: "Downloads the page (2 s limit) to read its title."},
		}},
		{Title: "AI", Fields: []field{
			{Key: "ai.provider", Label: "Provider", Type: "select", Options: config.Providers},
			{Key: "ai.timeout_seconds", Label: "Timeout (seconds)", Type: "number"},
			{Key: "ai.batch_minutes", Label: "Idea sorting interval (minutes)", Type: "number"},
			{Key: "ai.plan_start_day", Label: "Add an AI plan to Start Day", Type: "bool", Help: "Waits up to 5 seconds for the provider."},
		}},
		agent("claude_code", "Claude Code"),
		agent("github_copilot", "GitHub Copilot"),
		{Title: "Ollama", Fields: []field{
			{Key: "ollama.enabled", Label: "Enabled", Type: "bool"},
			{Key: "ollama.url", Label: "URL", Type: "text"},
			{Key: "ollama.model", Label: "Model", Type: "text"},
		}},
		{Title: "Desktop app (Windows)", DesktopOnly: true, Fields: []field{
			{Key: "desktop.autostart", Label: "Start STKR when I sign in", Type: "bool", Help: "Adds or removes the sign-in entry for the current Windows user."},
			{Key: "desktop.notifications", Label: "Show notifications", Type: "bool", Help: "Close reminder, update found, agent waiting."},
			{Key: "desktop.poll_seconds", Label: "Refresh every (seconds)", Type: "number", Help: "2 to 60."},
			{Key: "desktop.panel_on_top", Label: "Keep the panel on top", Type: "bool", Help: "Also the pin in the panel and the tray menu."},
			{Key: "desktop.hotkey", Label: "Panel hotkey", Type: "text", Help: "For example Ctrl+Alt+Space. Empty turns it off."},
			{Key: "desktop.open_panel_on_start", Label: "Show the panel at start", Type: "bool"},
		}},
		{Title: "Update checks", Fields: []field{
			{Key: "update.check", Label: "Check for updates", Type: "bool", Help: "One anonymous request to the GitHub API at start and then every interval. Off means no request unless you press Check now."},
			{Key: "update.channel", Label: "Channel", Type: "select", Options: []string{"stable", "beta"}, Help: "Beta also offers prereleases."},
			{Key: "update.interval_hours", Label: "Check every (hours)", Type: "number"},
		}},
		{Title: "MCP", Fields: []field{
			{Key: "mcp.enabled", Label: "Enabled", Type: "bool"},
		}},
	}
}

// buildSections fills the field values from cfg. Secret fields never carry
// their value, only whether one is set.
func buildSections(cfg *config.Config, isSecret func(key string) bool) []section {
	out := specs()
	for i := range out {
		for j := range out[i].Fields {
			f := &out[i].Fields[j]
			v, _ := cfg.Get(f.Key)
			if isSecret(f.Key) {
				f.Secret, f.Set, f.Value = true, v != "", ""
				continue
			}
			f.Value = v
		}
	}
	return out
}

// formPairs collects the posted values of the known settings. An empty secret
// field keeps the stored secret.
func formPairs(r *http.Request, isSecret func(key string) bool) map[string]string {
	pairs := map[string]string{}
	for _, s := range specs() {
		for _, f := range s.Fields {
			vals, ok := r.PostForm[f.Key]
			if !ok || len(vals) == 0 {
				continue
			}
			v := vals[len(vals)-1] // checkboxes post a hidden "false" first
			if isSecret(f.Key) && v == "" {
				continue
			}
			pairs[f.Key] = strings.TrimSpace(v)
		}
	}
	return pairs
}

func mountSettings(ui *UI) {
	ui.Get("GET /ui/settings", ui.settings)
	ui.Post("POST /ui/settings/config", func(r *http.Request, _ string) (string, error) {
		pairs := formPairs(r, config.IsSecret)
		if !ui.desktop() {
			// The desktop settings are hidden here and are not accepted either.
			for k := range pairs {
				if strings.HasPrefix(k, "desktop.") {
					delete(pairs, k)
				}
			}
		}
		if err := ui.Svc.UpdateConfig(r.Context(), pairs); err != nil {
			return "", err
		}
		if ui.desktop() && ui.hooks.SettingsSaved != nil {
			ui.hooks.SettingsSaved()
			ui.restarting.Store(true)
			return "Settings saved. STKR is restarting to apply them.", nil
		}
		return "Settings saved. Restart `stkr serve` for them to take effect.", nil
	})
	ui.Post("POST /ui/update/check", func(r *http.Request, _ string) (string, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		res, err := ui.Svc.UpdateCheck(ctx)
		if err != nil {
			return "", err
		}
		if res.Available {
			return fmt.Sprintf("STKR %s is available.", res.Latest), nil
		}
		return fmt.Sprintf("STKR is up to date (latest release: %s).", res.Latest), nil
	})
	ui.Post("POST /ui/update/apply", func(r *http.Request, _ string) (string, error) {
		if !ui.canInstallUpdate() {
			return "", domain.New(domain.ErrConflict, "This page can only install an update when the STKR desktop app runs the server. "+updateHint)
		}
		info, ok := ui.Svc.UpdatePending(r.Context())
		if !ok {
			return "", domain.New(domain.ErrConflict, "No update is waiting. Press Check now first.")
		}
		if err := ui.hooks.RequestUpdate(r.Context()); err != nil {
			return "", err
		}
		return fmt.Sprintf("Installing STKR %s. STKR closes and starts again by itself, this takes about a minute.", info.Latest), nil
	})
	ui.Post("POST /ui/settings/backup", func(r *http.Request, _ string) (string, error) {
		p, err := ui.Svc.Backup(r.Context())
		if err != nil {
			return "", err
		}
		return "Backup saved: " + filepath.Base(p), nil
	})
	ui.Post("POST /ui/settings/open-data", func(_ *http.Request, _ string) (string, error) {
		if !ui.desktop() || ui.hooks.OpenDataFolder == nil {
			return "", domain.New(domain.ErrConflict, "The data folder can only be opened from the desktop app. Use the path shown on this page.")
		}
		if err := ui.hooks.OpenDataFolder(); err != nil {
			return "", err
		}
		return "Opened the data folder.", nil
	})
	ui.Post("POST /ui/settings/ai-test", func(r *http.Request, _ string) (string, error) {
		st, err := ui.Svc.AIStatus(r.Context())
		if err != nil {
			return "", err
		}
		res, err := ui.Svc.AITest(r.Context())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Provider %s works (version %s, answered in %d ms).", res.Provider, versionOrUnknown(st.Version), res.Millis), nil
	})
	ui.Post("POST /ui/settings/ctx", func(r *http.Request, _ string) (string, error) {
		_, err := ui.Svc.AddContext(r.Context(), domain.Context{
			ID:          strings.TrimSpace(r.PostFormValue("id")),
			Label:       strings.TrimSpace(r.PostFormValue("label")),
			Kind:        domain.ContextKind(r.PostFormValue("kind")),
			TempoTicket: strings.TrimSpace(r.PostFormValue("tempo_ticket")),
			NotebookURL: strings.TrimSpace(r.PostFormValue("notebook_url")),
			Workdir:     strings.TrimSpace(r.PostFormValue("workdir")),
		})
		if err != nil {
			return "", err
		}
		return "Context added.", nil
	})
	ui.Post("POST /ui/settings/ctx/{id}/edit", func(r *http.Request, _ string) (string, error) {
		str := func(name string) *string { v := r.PostFormValue(name); return &v }
		p := service.ContextPatch{
			Label: str("label"), TempoTicket: str("tempo_ticket"),
			NotebookURL: str("notebook_url"), Workdir: str("workdir"),
		}
		if k := r.PostFormValue("kind"); k != "" {
			kind := domain.ContextKind(k)
			p.Kind = &kind
		}
		if _, err := ui.Svc.UpdateContext(r.Context(), r.PathValue("id"), p); err != nil {
			return "", err
		}
		return "Context saved.", nil
	})
	ui.Post("POST /ui/settings/ctx/{id}/park", func(r *http.Request, _ string) (string, error) {
		if err := ui.Svc.ParkContext(r.Context(), r.PathValue("id")); err != nil {
			return "", err
		}
		return "Context parked.", nil
	})
	ui.Post("POST /ui/settings/ctx/{id}/activate", func(r *http.Request, _ string) (string, error) {
		if err := ui.Svc.ActivateContext(r.Context(), r.PathValue("id")); err != nil {
			return "", err
		}
		return "Context activated.", nil
	})
}

func versionOrUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// updateView is the "Updates" box of the settings page.
type updateView struct {
	Current string
	// Dev is true for development builds, which are never updated.
	Dev bool
	// Known is true when a check result is cached.
	Known     bool
	Latest    string
	Available bool
	URL       string
	Checked   string
	// CanInstall shows the Install update button; otherwise Hint says how to
	// update.
	CanInstall bool
	Hint       string
}

func (u *UI) updateView(r *http.Request) updateView {
	v := updateView{Current: u.Svc.CurrentVersion(), CanInstall: u.canInstallUpdate()}
	v.Dev = update.IsDev(v.Current)
	if info, ok := u.Svc.UpdateInfo(r.Context()); ok {
		v.Known, v.Latest, v.URL = true, info.Latest, info.URL
		v.Available = info.Available && !v.Dev
		v.Checked = info.Checked.In(u.Svc.Location()).Format("2006-01-02 15:04")
	}
	if !v.CanInstall {
		v.Hint = updateHint
	}
	return v
}

func (u *UI) settings(w http.ResponseWriter, r *http.Request) {
	ctxs, err := u.Svc.ListContexts(r.Context(), service.ContextFilter{IncludeParked: true})
	if err != nil {
		u.Fail(w, err)
		return
	}
	cfg, err := u.Svc.ConfigFile()
	if err != nil {
		u.Fail(w, err)
		return
	}
	var secs []section
	for _, s := range buildSections(cfg, config.IsSecret) {
		if s.DesktopOnly && !u.desktop() {
			continue
		}
		// The sign-in entry itself, not the config file, tells the truth: the
		// installer writes it too.
		if s.DesktopOnly {
			if on, err := u.Svc.AutostartEnabled(); err == nil {
				for i := range s.Fields {
					if s.Fields[i].Key == service.AutostartKey {
						s.Fields[i].Value = strconv.FormatBool(on)
					}
				}
			}
		}
		secs = append(secs, s)
	}
	u.Render(w, r, "settings", "Settings", settingsData{
		Contexts: ctxs,
		Kinds:    domain.ContextKinds,
		Sections: secs,
		Desktop:  u.desktop(),
		Update:   u.updateView(r),
	})
}
