package httpapi

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"time"
)

const maxBody = 1 << 20

func (s *Server) allowedHosts() []string {
	p := strconv.Itoa(s.port)
	return []string{"127.0.0.1:" + p, "localhost:" + p}
}

// hostGuard rejects requests whose Host is not the loopback address, which
// blocks DNS rebinding.
func (s *Server) hostGuard(next http.Handler) http.Handler {
	hosts := s.allowedHosts()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(hosts, r.Host) {
			writeJSON(w, http.StatusMisdirectedRequest, errBody{Error: "invalid_host", Message: "host not allowed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originGuard rejects state-changing requests from a foreign Origin, which
// blocks cross-site requests from web pages. A missing Origin is allowed
// (curl, Stream Deck, scripts).
func (s *Server) originGuard(next http.Handler) http.Handler {
	allowed := slices.Clone(s.allowedOrigins)
	p := strconv.Itoa(s.port)
	allowed = append(allowed, "http://127.0.0.1:"+p, "http://localhost:"+p)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if o := r.Header.Get("Origin"); o != "" && !slices.Contains(allowed, o) {
				writeJSON(w, http.StatusForbidden, errBody{Error: "forbidden_origin", Message: "origin not allowed"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logRequests logs method, path, status and duration. Query strings and
// bodies are never logged.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now() // request latency, not domain time
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic in handler", "path", r.URL.Path, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				writeJSON(w, http.StatusInternalServerError, errBody{Error: "internal", Message: "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
