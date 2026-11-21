package desktop

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/logx"
	"github.com/chris-reynier/stkr/internal/server"
	"github.com/chris-reynier/stkr/internal/service"
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
)

// Main runs the desktop app and returns the process exit code: 0 after Quit
// or when another instance was asked to show itself, 1 on a start error, 2
// on systems without the tray or on bad arguments.
func Main(args []string, stderr io.Writer, version string) int {
	if !supported {
		fmt.Fprintln(stderr, UnsupportedMessage)
		return 2
	}
	fs := flag.NewFlagSet("stkrw", flag.ContinueOnError)
	fs.SetOutput(stderr)
	homeFlag := fs.String("home", "", "data directory (default $STKR_HOME or ~/.stkr)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	home, err := config.ResolveHome(*homeFlag)
	if err == nil {
		err = config.EnsureHome(home)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	logDir := filepath.Join(home, "logs")
	rf, err := logx.OpenRotating(filepath.Join(logDir, "desktop.log"), logx.MaxFileSize, logx.MaxFiles)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer rf.Close()
	lg := slog.New(slog.NewJSONHandler(rf, nil))

	name := InstanceName(home)
	release, already, err := acquireInstance(name)
	if err != nil {
		lg.Error("single instance check failed", "err", err)
		return 1
	}
	if already {
		if err := signalInstance(name); err != nil {
			lg.Warn("cannot reach the running instance", "err", err)
		}
		lg.Info("already running, asked it to show")
		return 0
	}
	defer release()

	cfg, warnings, err := config.Load(config.Path(home))
	if err != nil {
		lg.Error("config", "err", err)
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, w := range warnings {
		lg.Warn("config", "warning", w)
	}
	srvLog, srvClose, err := logx.New(logDir, false)
	if err != nil {
		lg.Error("server log", "err", err)
		return 1
	}
	defer srvClose.Close()

	tray, err := newTray(name)
	if err != nil {
		lg.Error("tray", "err", err)
		fmt.Fprintln(stderr, err)
		return 1
	}
	host := &serviceHost{home: home, cfg: cfg, log: srvLog, version: version}
	defer host.close()
	a := NewApp(Options{
		Home: home, Version: version, Port: cfg.Port,
		Poll:          time.Duration(cfg.Desktop.PollSeconds) * time.Second,
		Notifications: cfg.Desktop.Notifications,
		Tray:          tray, Host: host.start, Open: openTarget, Logger: lg,
	})
	lg.Info("desktop started", "version", version, "port", cfg.Port)
	if err := a.Run(); err != nil {
		lg.Error("tray stopped", "err", err)
		return 1
	}
	return 0
}

// serviceHost opens the database once and runs server.Start on it.
type serviceHost struct {
	home    string
	cfg     *config.Config
	log     *slog.Logger
	version string

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
	hd, err := server.Start(ctx, h.svc, server.Options{Port: port, Logger: h.log, HostedBy: server.HostedByDesktop})
	if err != nil {
		return nil, err
	}
	return hd, nil
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
