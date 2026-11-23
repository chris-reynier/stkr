package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func (s *Server) reviewRoutes() {
	s.mux.HandleFunc("GET /api/review/context", s.handleReviewContext)
	s.mux.HandleFunc("GET /api/review/prompt", s.handleReviewPrompt)
	s.mux.HandleFunc("POST /api/review/prompt", s.handleReviewPrompt)
	s.mux.HandleFunc("POST /api/activity/events", s.handleActivityEvents)
	s.mux.HandleFunc("GET /api/activity/summary", s.handleActivitySummary)
	s.mux.HandleFunc("POST /api/import/preview", s.handleImportPreview)
}

func (s *Server) handleReviewContext(w http.ResponseWriter, r *http.Request) {
	rc, err := s.svc.ReviewContext(r.Context(), r.URL.Query().Get("date"))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

// handleReviewPrompt returns the review prompt. GET has no side effects; POST
// also copies it to the clipboard unless clipboard=0.
func (s *Server) handleReviewPrompt(w http.ResponseWriter, r *http.Request) {
	clip := r.Method == http.MethodPost && flag(r, "clipboard", true)
	res, err := s.svc.ReviewPrompt(r.Context(), r.URL.Query().Get("date"), clip)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// readStrictJSON decodes exactly one JSON value and rejects unknown fields,
// so data that has no field (window titles, URLs, clipboard text) cannot be
// sent.
func readStrictJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.More() {
		err = errors.New("more than one JSON value")
	}
	if err == nil {
		if _, rerr := io.Copy(io.Discard, r.Body); rerr != nil {
			err = rerr
		}
	}
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return err
		}
		return domain.New(domain.ErrInvalid, fmt.Sprintf("request body is not valid: %v", err))
	}
	return nil
}

func (s *Server) handleActivityEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Events []service.ActivityInput `json:"events"`
	}
	if err := readStrictJSON(r, &body); err != nil {
		s.writeErr(w, err)
		return
	}
	if body.Events == nil {
		s.writeErr(w, domain.New(domain.ErrInvalid, "events is required"))
		return
	}
	if len(body.Events) > service.MaxActivityBatch {
		writeJSON(w, http.StatusRequestEntityTooLarge, errBody{"too_large", fmt.Sprintf("at most %d events per request", service.MaxActivityBatch)})
		return
	}
	res, err := s.svc.AddActivity(r.Context(), body.Events)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleActivitySummary(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = "today"
	}
	sum, err := s.svc.ActivityDay(r.Context(), date)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r.Body); err != nil {
		s.writeErr(w, err)
		return
	}
	p, err := s.svc.PreviewImport(r.Context(), buf.String())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
