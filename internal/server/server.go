// Package server runs the STKR HTTP server: the loopback listener, the API
// and web UI routes and the background jobs. `stkr serve` and the desktop app
// both start it through Start, so there is one server code path.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/httpapi"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/web"
)

// Who hosts the server, reported by GET /api/status.
const (
	HostedByServe   = "serve"
	HostedByDesktop = "desktop"
)

// Options configure Start.
type Options struct {
	// Port to listen on; 0 picks a free port.
	Port int
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// HostedBy is HostedByServe (default) or HostedByDesktop.
	HostedBy string
}

// ListenError is returned by Start when the port cannot be opened.
type ListenError struct {
	Port int
	Err  error
}

func (e *ListenError) Error() string {
	return fmt.Sprintf("cannot listen on 127.0.0.1:%d: %v", e.Port, e.Err)
}

func (e *ListenError) Unwrap() error { return e.Err }

// Handle is a running server.
type Handle struct {
	port   int
	jobs   []string
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

// Start opens the listener, starts the background jobs and serves the API
// and web UI until ctx is cancelled or Shutdown is called. It returns once
// the server accepts connections.
func Start(ctx context.Context, svc *service.Service, opts Options) (*Handle, error) {
	lg := opts.Logger
	if lg == nil {
		lg = slog.Default()
	}
	hostedBy := opts.HostedBy
	if hostedBy == "" {
		hostedBy = HostedByServe
	}
	ln, err := httpapi.Listen(opts.Port)
	if err != nil {
		return nil, &ListenError{Port: opts.Port, Err: err}
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srv := httpapi.New(svc, httpapi.Options{Port: port, AllowedOrigins: svc.Config().AllowedOrigins, Logger: lg, HostedBy: hostedBy})
	if _, err := web.Mount(srv.Mux(), svc, web.Options{Logger: lg}); err != nil {
		_ = ln.Close()
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	runner := app.NewRunner(lg)
	h := &Handle{port: port, cancel: cancel, done: make(chan struct{})}
	h.jobs = addJobs(runner, svc, lg)
	runner.Start(runCtx)

	lg.Info("serve started", "port", port, "hosted_by", hostedBy)
	go func() {
		err := srv.Serve(runCtx, ln)
		cancel()
		runner.Wait()
		if err != nil && !errors.Is(err, context.Canceled) {
			h.mu.Lock()
			h.err = err
			h.mu.Unlock()
		}
		lg.Info("serve stopped", "port", port)
		close(h.done)
	}()
	return h, nil
}

// addJobs registers the background jobs for the configuration and returns
// their names.
func addJobs(r *app.Runner, svc *service.Service, lg *slog.Logger) []string {
	cfg := svc.Config()
	var names []string
	add := func(name string, every time.Duration, fn func(context.Context) error) {
		r.Add(name, every, fn)
		names = append(names, name)
	}
	add("daily-backup", time.Hour, func(ctx context.Context) error {
		_, _, err := svc.DailyBackup(ctx)
		return err
	})
	add("agent-sweeper", time.Minute, func(ctx context.Context) error {
		_, err := svc.SweepAgents(ctx)
		return err
	})
	add("close-reminder", time.Minute, svc.ReminderTick)
	add("phone-inbox", 30*time.Second, func(ctx context.Context) error {
		_, err := svc.ScanInbox(ctx)
		return err
	})
	if cfg.Update.Check {
		add("update-check", time.Duration(cfg.Update.IntervalHours)*time.Hour, svc.UpdateTick)
	}
	if svc.AIActive() {
		add("ai-batch", time.Duration(cfg.AI.BatchMinutes)*time.Minute, svc.AIBatchTick)
	} else if w := svc.AIStatusWarning(); w != "" {
		lg.Warn("AI provider not active", "reason", w)
	}
	return names
}

// Port is the port the server listens on.
func (h *Handle) Port() int { return h.port }

// Addr is "127.0.0.1:<port>".
func (h *Handle) Addr() string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(h.port)) }

// URL is the base URL, "http://127.0.0.1:<port>".
func (h *Handle) URL() string { return "http://" + h.Addr() }

// Jobs lists the names of the background jobs that were started.
func (h *Handle) Jobs() []string { return append([]string(nil), h.jobs...) }

// Done is closed when the server and its jobs have stopped.
func (h *Handle) Done() <-chan struct{} { return h.done }

// Err is the serve error after Done is closed (nil after a normal shutdown).
func (h *Handle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// Shutdown stops the server gracefully (open requests get up to 5 seconds)
// and waits for the jobs to return, or until ctx ends.
func (h *Handle) Shutdown(ctx context.Context) error {
	h.cancel()
	select {
	case <-h.done:
		return h.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
