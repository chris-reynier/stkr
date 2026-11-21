package desktop

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/logx"
	"github.com/chris-reynier/stkr/internal/server"
	"github.com/chris-reynier/stkr/internal/service"
	upd "github.com/chris-reynier/stkr/internal/update"
)

// UnsupportedMessage is printed by stkrw on systems without the tray.
const UnsupportedMessage = "the desktop app is Windows only, use `stkr serve`"

// Platform hooks, replaced in tests.
var (
	supported       = platformSupported
	newTray         = newPlatformTray
	acquireInstance = platformAcquireInstance
	signalInstance  = platformSignalInstance
	openTarget      = platformOpen
	newWindows      = platformWindowsFactory
	confirmBox      = platformConfirm
	executablePath  = os.Executable
	relaunchProgram = upd.StartDetached
	newUpdater      = func(h *serviceHost, exe string) UpdateOps { return serviceUpdater{h: h, exe: exe} }
)

// Main runs the desktop app and returns the process exit code: 0 after Quit
// or when another instance was asked to show itself, 1 on a start error, 2
// on systems without the tray or on bad arguments. After an update that was
// installed by replacing the program files, the new program is started once
// everything of this process (including the single-instance lock) is released.
func Main(args []string, stderr io.Writer, version string) int {
	code, rl := run(args, stderr, version)
	if rl != nil {
		if err := relaunchProgram(context.Background(), rl.exe, rl.args); err != nil {
			fmt.Fprintln(stderr, "STKR was updated but could not be started again:", err)
			return 1
		}
	}
	return code
}

func run(args []string, stderr io.Writer, version string) (int, *relaunch) {
	if !supported {
		fmt.Fprintln(stderr, UnsupportedMessage)
		return 2, nil
	}
	fs := flag.NewFlagSet("stkrw", flag.ContinueOnError)
	fs.SetOutput(stderr)
	homeFlag := fs.String("home", "", "data directory (default $STKR_HOME or ~/.stkr)")
	quit := fs.Bool("quit", false, "ask the running app for this data directory to quit, then exit")
	// --autostart is the marker the installer and Settings put in the sign-in
	// entry. It changes nothing; accepting it keeps old entries working.
	_ = fs.Bool("autostart", false, "started when the user signed in")
	afterUpdate := fs.Bool("after-update", false, "started by an update: say so once the app is running")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	home, err := config.ResolveHome(*homeFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1, nil
	}
	if *quit {
		// Nothing running is fine: the goal (no running app) is met. The data
		// folder is not created for this.
		_ = signalInstance(InstanceName(home), true)
		return 0, nil
	}
	exe, _ := executablePath()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// A previous update leaves <program>.old files on Windows.
	upd.CleanupOld(exe)
	if err := config.EnsureHome(home); err != nil {
		fmt.Fprintln(stderr, err)
		return 1, nil
	}
	logDir := filepath.Join(home, "logs")
	rf, err := logx.OpenRotating(filepath.Join(logDir, "desktop.log"), logx.MaxFileSize, logx.MaxFiles)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1, nil
	}
	defer rf.Close()
	lg := slog.New(slog.NewJSONHandler(rf, nil))

	name := InstanceName(home)
	release, already, err := acquireInstance(name)
	if err != nil {
		lg.Error("single instance check failed", "err", err)
		return 1, nil
	}
	if already {
		if err := signalInstance(name, false); err != nil {
			lg.Warn("cannot reach the running instance", "err", err)
		}
		lg.Info("already running, asked it to show")
		return 0, nil
	}
	defer release()

	cfg, warnings, err := config.Load(config.Path(home))
	if err != nil {
		lg.Error("config", "err", err)
		fmt.Fprintln(stderr, err)
		return 1, nil
	}
	for _, w := range warnings {
		lg.Warn("config", "warning", w)
	}
	srvLog, srvClose, err := logx.New(logDir, false)
	if err != nil {
		lg.Error("server log", "err", err)
		return 1, nil
	}
	defer srvClose.Close()

	tray, err := newTray(name)
	if err != nil {
		lg.Error("tray", "err", err)
		fmt.Fprintln(stderr, err)
		return 1, nil
	}
	host := &serviceHost{home: home, cfg: cfg, log: srvLog, version: version}
	defer host.close()
	s := settingsOf(cfg)
	a := NewApp(Options{
		Home: home, Version: version, Port: cfg.Port,
		Poll:          s.Poll,
		Notifications: s.Notifications,
		Tray:          tray, Host: host.start, Open: openTarget, Logger: lg,
		Windows:        newWindows(home, cfg.Desktop.PanelOnTop, lg),
		PanelOnTop:     cfg.Desktop.PanelOnTop,
		SavePanelOnTop: func(on bool) error { return savePanelOnTop(home, on) },
		Hotkey:         cfg.Desktop.Hotkey, OpenPanelOnStart: cfg.Desktop.OpenPanelOnStart,
		Updater: newUpdater(host, exe), Confirm: confirmBox, Exe: exe,
		Reload: host.reload, JustUpdated: *afterUpdate,
	})
	host.app = a
	lg.Info("desktop started", "version", version, "port", cfg.Port, "after_update", *afterUpdate)
	if err := a.Run(); err != nil {
		lg.Error("tray stopped", "err", err)
		return 1, nil
	}
	if path, rargs := a.PendingRelaunch(); path != "" {
		lg.Info("starting the updated program")
		return 0, &relaunch{exe: path, args: rargs}
	}
	return 0, nil
}

// settingsOf extracts what the app applies when the server restarts.
func settingsOf(cfg *config.Config) Settings {
	return Settings{
		Port: cfg.Port, Poll: time.Duration(cfg.Desktop.PollSeconds) * time.Second,
		Notifications: cfg.Desktop.Notifications, PanelOnTop: cfg.Desktop.PanelOnTop,
		Hotkey: cfg.Desktop.Hotkey, OpenPanelOnStart: cfg.Desktop.OpenPanelOnStart,
	}
}

// serviceHost opens the database once per configuration and runs
// server.Start on it.
type serviceHost struct {
	home    string
	cfg     *config.Config
	log     *slog.Logger
	version string
	// app receives the web UI's requests (set before Run).
	app *App

	mu      sync.Mutex
	svc     *service.Service
	closeFn func() error
}

func (h *serviceHost) start(ctx context.Context, port int) (Hosted, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.svc == nil {
		svc, closeFn, err := app.Open(ctx, h.home, h.cfg, app.Options{Logger: h.log, Version: h.version})
		if err != nil {
			return nil, err
		}
		h.svc, h.closeFn = svc, closeFn
	}
	hd, err := server.Start(ctx, h.svc, server.Options{
		Port: port, Logger: h.log, HostedBy: server.HostedByDesktop,
		OnUpdateRequest:  func(context.Context) error { return h.app.RequestUpdate() },
		OnSettingsSaved:  func() { h.app.SettingsSaved() },
		OnOpenDataFolder: func() error { return h.app.OpenDataFolder() },
	})
	if err != nil {
		return nil, err
	}
	return hd, nil
}

// reload reads the saved configuration and drops the service, so the next
// start opens it with the new values. The server must be stopped first.
func (h *serviceHost) reload() (Settings, error) {
	cfg, warnings, err := config.Load(config.Path(h.home))
	if err != nil {
		return Settings{}, err
	}
	for _, w := range warnings {
		h.log.Warn("config", "warning", w)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg = cfg
	if h.closeFn != nil {
		if err := h.closeFn(); err != nil && !errors.Is(err, context.Canceled) {
			h.log.Error("close database", "err", err)
		}
		h.svc, h.closeFn = nil, nil
	}
	return settingsOf(cfg), nil
}

func (h *serviceHost) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closeFn != nil {
		if err := h.closeFn(); err != nil && !errors.Is(err, context.Canceled) {
			h.log.Error("close database", "err", err)
		}
		h.svc, h.closeFn = nil, nil
	}
}

// serviceUpdater runs the update steps with the hosted service.
type serviceUpdater struct {
	h   *serviceHost
	exe string
}

func (u serviceUpdater) service() (*service.Service, error) {
	u.h.mu.Lock()
	defer u.h.mu.Unlock()
	if u.h.svc == nil {
		return nil, errOffline
	}
	return u.h.svc, nil
}

func (u serviceUpdater) Check(ctx context.Context) (UpdateInfo, error) {
	svc, err := u.service()
	if err != nil {
		return UpdateInfo{}, err
	}
	res, err := svc.UpdateCheck(ctx)
	if err != nil {
		return UpdateInfo{}, err
	}
	return UpdateInfo{Current: res.Current, Latest: res.Latest, Available: res.Available}, nil
}

func (u serviceUpdater) Stage(ctx context.Context) (StagedUpdate, error) {
	svc, err := u.service()
	if err != nil {
		return nil, err
	}
	// Relaunch: when the setup does the install it starts stkrw again.
	st, err := svc.UpdateStage(ctx, u.exe, upd.FetchOptions{Relaunch: true})
	if err != nil {
		return nil, err
	}
	return stagedUpdate{st}, nil
}

// stagedUpdate adapts *update.Staged to StagedUpdate.
type stagedUpdate struct{ s *upd.Staged }

func (s stagedUpdate) Version() string                  { return s.s.Tag }
func (s stagedUpdate) Relaunches() bool                 { return s.s.Method == upd.MethodSetup }
func (s stagedUpdate) Commit(ctx context.Context) error { return s.s.Commit(ctx) }
func (s stagedUpdate) Discard()                         { s.s.Discard() }

// savePanelOnTop stores the "keep the panel on top" state in config.yaml,
// keeping every other setting as the file has it.
func savePanelOnTop(home string, on bool) error {
	path := config.Path(home)
	cfg, _, err := config.Load(path)
	if err != nil {
		return err
	}
	if err := cfg.Set("desktop.panel_on_top", strconv.FormatBool(on)); err != nil {
		return err
	}
	return config.Save(path, cfg)
}
