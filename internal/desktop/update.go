package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Action ids of the update and restart flows.
const (
	ActCheckUpdate   = "check-update"       // tray: ask GitHub, show the answer
	ActInstallUpdate = "install-update"     // tray: confirm, then install
	ActInstallNow    = "install-update-now" // from the web UI button: the click was the confirmation
	ActRestartServer = "restart-server"     // from Settings: apply saved settings
)

// Timing of the update flow. Tests shorten RestartDelay.
var (
	// UpdateTimeout bounds download plus install.
	UpdateTimeout = 10 * time.Minute
	// RestartDelay lets the page that saved the settings finish loading
	// before the server it comes from is restarted.
	RestartDelay = 400 * time.Millisecond
)

// AfterUpdateFlag is the program argument of the instance started after an
// update (contract install-update.md).
const AfterUpdateFlag = "--after-update"

// UpdateInfo is the answer of an explicit update check.
type UpdateInfo struct {
	Current   string
	Latest    string
	Available bool
}

// StagedUpdate is a downloaded and verified update that has not been
// installed yet.
type StagedUpdate interface {
	// Version is the release being installed ("v0.2.0").
	Version() string
	// Relaunches is true when the installer starts the app again itself
	// (the setup path); otherwise the app starts the new program.
	Relaunches() bool
	// Commit installs the update. For the setup path it starts the setup and
	// returns at once.
	Commit(ctx context.Context) error
	// Discard drops what was downloaded after a failed or cancelled install.
	Discard()
}

// UpdateOps are the update steps of the hosting process.
type UpdateOps interface {
	// Check asks GitHub for the latest release, whatever update.check says.
	Check(ctx context.Context) (UpdateInfo, error)
	// Stage downloads and verifies the update without changing anything.
	Stage(ctx context.Context) (StagedUpdate, error)
}

// Settings are the saved settings the app applies without a restart of the
// process when the server restarts.
type Settings struct {
	Port             int
	Poll             time.Duration
	Notifications    bool
	PanelOnTop       bool
	Hotkey           string
	OpenPanelOnStart bool
}

// relaunch is a program to start after the app has exited.
type relaunch struct {
	exe  string
	args []string
}

var errUpdateBusy = errors.New("an update or restart is already in progress")

// attachHint is shown when the server belongs to `stkr serve`.
const attachHint = "STKR is attached to `stkr serve`, which this app does not control. Run `stkr update` in a terminal, then restart `stkr serve`"

// beginBusy marks the start of a flow that takes the server down. While it
// is set the poll loop does not reconnect.
func (a *App) beginBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return false
	}
	a.busy = true
	return true
}

func (a *App) endBusy() {
	a.mu.Lock()
	a.busy = false
	a.mu.Unlock()
	a.poke()
}

func (a *App) poke() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

// PendingRelaunch returns the program to start once Run has returned and the
// single-instance lock is released ("" when none).
func (a *App) PendingRelaunch() (exe string, args []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.relaunch == nil {
		return "", nil
	}
	return a.relaunch.exe, a.relaunch.args
}

// RequestUpdate is the web UI's Install update button: it starts the update
// in the background (no confirmation, the click is it) and returns at once,
// because the server that served the click is about to stop.
func (a *App) RequestUpdate() error {
	a.mu.Lock()
	mode, busy := a.mode, a.busy
	a.mu.Unlock()
	switch {
	case a.opts.Updater == nil:
		return errors.New("this build cannot update itself")
	case mode == ModeAttach:
		return errors.New(attachHint)
	case busy:
		return errUpdateBusy
	}
	a.Select(ActInstallNow)
	return nil
}

// SettingsSaved restarts the hosted server so saved settings apply.
func (a *App) SettingsSaved() { a.Select(ActRestartServer) }

// OpenDataFolder opens the data folder in the file manager.
func (a *App) OpenDataFolder() error { return a.open(a.opts.Home) }

// checkUpdate runs the "Check for updates" menu item.
func (a *App) checkUpdate() (string, error) {
	a.mu.Lock()
	c, mode := a.client, a.mode
	a.mu.Unlock()
	if mode == ModeAttach || a.opts.Updater == nil {
		// The attached `stkr serve` does the check; its Settings page has the
		// Check now button.
		if c == nil {
			return "", errOffline
		}
		return "", a.app(c, pageSettings+"#updates")
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	info, err := a.opts.Updater.Check(ctx)
	if err != nil {
		return "", fmt.Errorf("could not check for updates: %w", err)
	}
	if info.Available {
		return fmt.Sprintf("STKR %s is available (you have %s). Choose Install update in the STKR menu.", info.Latest, info.Current), nil
	}
	return fmt.Sprintf("STKR is up to date (%s).", info.Current), nil
}

// installUpdate is the update flow: download and verify with the server still
// running, then stop the server, install and start again. A failure before the
// install leaves everything running; a failure of the install restarts the
// server.
func (a *App) installUpdate(confirm bool) (string, error) {
	a.mu.Lock()
	mode, st := a.mode, a.status
	a.mu.Unlock()
	switch {
	case a.opts.Updater == nil:
		return "", errors.New("this build cannot update itself")
	case mode == ModeAttach:
		return "", errors.New(attachHint)
	case mode != ModeHost:
		return "", errOffline
	}
	if !a.beginBusy() {
		return "", errUpdateBusy
	}
	// busy stays set when the update succeeds: the app is about to exit.
	keepBusy := false
	defer func() {
		if !keepBusy {
			a.endBusy()
		}
	}()

	if confirm {
		version := "the new version"
		if st != nil && st.Update != nil && st.Update.Available && st.Update.Version != "" {
			version = st.Update.Version
		}
		if a.opts.Confirm == nil || !a.opts.Confirm("Install STKR update",
			fmt.Sprintf("STKR %s is available. STKR will close, install the update and start again by itself. Your data is kept.\n\nInstall it now?", version)) {
			return "", nil
		}
	}
	a.notify(Notice{Title: "STKR", Text: "Downloading the update..."})
	ctx, cancel := context.WithTimeout(a.ctx, UpdateTimeout)
	defer cancel()
	staged, err := a.opts.Updater.Stage(ctx)
	if err != nil {
		return "", fmt.Errorf("the update could not be downloaded: %w", err)
	}

	// From here the server is down until the update is installed or failed.
	a.stopHosted()
	if err := staged.Commit(ctx); err != nil {
		staged.Discard()
		a.log.Warn("update failed", "err_kind", errKind(err))
		return "", fmt.Errorf("the update failed and was not installed (%w); STKR keeps running the current version", err)
	}
	a.log.Info("update installed", "relaunch_by_setup", staged.Relaunches())
	keepBusy = true
	if !staged.Relaunches() {
		a.mu.Lock()
		a.relaunch = &relaunch{exe: a.opts.Exe, args: []string{AfterUpdateFlag}}
		a.mu.Unlock()
	}
	a.opts.Tray.Quit()
	return "", nil
}

// stopHosted shuts the hosted server down and forgets the connection, so the
// poll loop (blocked by busy) does not reconnect.
func (a *App) stopHosted() {
	a.mu.Lock()
	h := a.hosted
	a.mode, a.client, a.hosted = ModeNone, nil, nil
	a.mu.Unlock()
	if h == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), QuitTimeout)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		a.log.Error("hosted server shutdown", "err", err)
	}
}

// restartServer applies saved settings: the hosted server is stopped, the
// configuration is read again, and the poll loop starts the server again on
// the same port (unless the saved port changed).
func (a *App) restartServer() (string, error) {
	select {
	case <-time.After(RestartDelay):
	case <-a.ctx.Done():
		return "", nil
	}
	a.mu.Lock()
	mode, c := a.mode, a.client
	a.mu.Unlock()
	if mode != ModeHost || c == nil {
		return "", nil // attached: the settings take effect when `stkr serve` restarts
	}
	if !a.beginBusy() {
		return "", errUpdateBusy
	}
	defer a.endBusy()
	port := c.Port()
	a.stopHosted()
	if a.opts.Reload == nil {
		return "", nil
	}
	s, err := a.opts.Reload()
	if err != nil {
		a.log.Warn("settings reload failed", "err_kind", errKind(err))
		return "", fmt.Errorf("the saved settings could not be applied: %w", err)
	}
	a.applySettings(s, port)
	return "", nil
}

// applySettings takes the saved settings into use. The server keeps its
// current port unless the saved port differs from the one it was started for.
func (a *App) applySettings(s Settings, currentPort int) {
	a.mu.Lock()
	if s.Port == a.opts.Port {
		a.opts.Port = currentPort
	} else {
		a.opts.Port = s.Port
	}
	if s.Poll > 0 {
		a.opts.Poll = s.Poll
	}
	a.opts.Notifications = s.Notifications
	pinChanged := s.PanelOnTop != a.panelOnTop
	a.panelOnTop = s.PanelOnTop
	hotkeyChanged := s.Hotkey != a.opts.Hotkey
	a.opts.Hotkey = s.Hotkey
	a.mu.Unlock()
	if pinChanged && a.win != nil {
		a.win.SetPanelOnTop(s.PanelOnTop)
	}
	if hotkeyChanged {
		a.setupHotkey()
	}
}

// updatedText is the notification of the instance started after an update.
func updatedText(version string) string {
	return "STKR updated to " + strings.TrimPrefix(version, "v") + "."
}

// ServerPort is the port of the server the app is connected to (0 when it is
// not connected). It is safe to call from any goroutine.
func (a *App) ServerPort() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return 0
	}
	return a.client.Port()
}
