package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chris-reynier/stkr/internal/domain"
)

type errBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr maps a service error to a JSON error response.
func (s *Server) writeErr(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody{"not_found", err.Error()})
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, http.StatusConflict, errBody{"conflict", err.Error()})
	case errors.Is(err, domain.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, errBody{"invalid", err.Error()})
	case errors.Is(err, domain.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errBody{"unavailable", err.Error()})
	case errors.As(err, &maxErr):
		writeJSON(w, http.StatusRequestEntityTooLarge, errBody{"too_large", "request body too large"})
	default:
		s.log.Error("request failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, errBody{"internal", "internal error"})
	}
}
