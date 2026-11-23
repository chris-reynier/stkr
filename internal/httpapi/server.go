// Package httpapi is the localhost HTTP adapter: security middleware, JSON
// error mapping and the core endpoints. It is a thin layer over service.Service.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/chris-reynier/stkr/internal/buildinfo"
	"github.com/chris-reynier/stkr/internal/service"
)

// Server serves the HTTP API on a loopback listener.
type Server struct {
	svc            *service.Service
	log            *slog.Logger
	port           int
	allowedOrigins []string
	hostedBy       string
	mux            *http.ServeMux
}

// Options configure a Server.
type Options struct {
	// Port is the port clients use to reach the server. It is what the Host
	// and Origin guards accept.
	Port int
	// AllowedOrigins are extra origins accepted on state-changing requests.
	AllowedOrigins []string
	Logger         *slog.Logger
	// HostedBy is reported by GET /api/status: "serve" (default) or "desktop".
	HostedBy string
}

// New builds a Server.
func New(svc *service.Service, opts Options) *Server {
	lg := opts.Logger
	if lg == nil {
		lg = slog.Default()
	}
	hostedBy := opts.HostedBy
	if hostedBy == "" {
		hostedBy = "serve"
	}
	s := &Server{svc: svc, log: lg, port: opts.Port, allowedOrigins: opts.AllowedOrigins, hostedBy: hostedBy, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Mux returns the route table so later features can register routes before
// Handler is called. Routes added there get all middleware.
func (s *Server) Mux() *http.ServeMux { return s.mux }

// Handler returns the full handler chain: recover, logging, Host guard, Origin
// guard, then the routes.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.originGuard(h)
	h = s.hostGuard(h)
	h = s.logRequests(h)
	h = s.recoverPanics(h)
	return h
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /api/switch/{ctx}", s.handleSwitch)
	s.mux.HandleFunc("POST /api/stop", s.handleStop)
	s.mux.HandleFunc("GET /api/now", s.handleNow)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/contexts", s.handleContexts)
	s.mux.HandleFunc("GET /img/now.png", s.handleNowImage)
	s.mux.HandleFunc("GET /api/week/{week}/tempo.csv", s.handleTempoCSV)
	s.agentRoutes()
	s.dailyRoutes()
	s.captureRoutes()
	s.reviewRoutes()
	s.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, errBody{Error: "not_found", Message: "no such route"})
	})
}

// Listen opens a loopback listener. Port 0 picks a free port.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// Serve serves on ln until ctx is cancelled, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		done <- srv.Shutdown(sctx)
	}()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return <-done
	}
	return err
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": buildinfo.Version})
}
