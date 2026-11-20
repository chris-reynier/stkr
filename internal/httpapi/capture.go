package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/keyimg"
	"github.com/chris-reynier/stkr/internal/service"
)

func (s *Server) captureRoutes() {
	s.mux.HandleFunc("POST /api/idea", s.handleIdea)
	s.mux.HandleFunc("POST /api/idea/record/toggle", s.handleRecordToggle)
	s.mux.HandleFunc("POST /api/bookmark", s.handleBookmark)
	s.mux.HandleFunc("POST /api/bookmark/from-clipboard", s.handleBookmarkClipboard)
	s.mux.HandleFunc("GET /api/capture/health", s.handleCaptureHealth)
	s.mux.HandleFunc("GET /img/key/idea.png", s.handleIdeaImage)
	s.mux.HandleFunc("GET /img/key/ideas-inbox.png", s.handleIdeasInboxImage)
}

type ideaRequest struct {
	Text string `json:"text"`
	Tag  string `json:"tag"`
	Type string `json:"type"`
}

func (s *Server) handleIdea(w http.ResponseWriter, r *http.Request) {
	var req ideaRequest
	if err := readJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	res, err := s.svc.AddIdea(r.Context(), service.IdeaInput{Text: req.Text, Tag: req.Tag, Type: req.Type, Via: string(source(r))})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	status := http.StatusCreated
	if !res.Created {
		status = http.StatusOK
	}
	writeJSON(w, status, res)
}

func (s *Server) handleRecordToggle(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.ToggleRecording(r.Context(), r.URL.Query().Get("tag"))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type bookmarkRequest struct {
	URL  string   `json:"url"`
	Tags []string `json:"tags"`
}

// bookmarkConflict is the 409 body: the error plus the stored bookmark.
type bookmarkConflict struct {
	errBody
	Bookmark domain.Bookmark `json:"bookmark"`
}

func (s *Server) writeBookmark(w http.ResponseWriter, b domain.Bookmark, err error) {
	var dup *service.DuplicateBookmarkError
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]any{"bookmark": b})
	case errors.As(err, &dup):
		writeJSON(w, http.StatusConflict, bookmarkConflict{errBody{"conflict", dup.Error()}, dup.Existing})
	default:
		s.writeErr(w, err)
	}
}

func (s *Server) handleBookmark(w http.ResponseWriter, r *http.Request) {
	var req bookmarkRequest
	if err := readJSON(r, &req); err != nil {
		s.writeErr(w, err)
		return
	}
	b, err := s.svc.AddBookmark(r.Context(), req.URL, req.Tags)
	s.writeBookmark(w, b, err)
}

func (s *Server) handleBookmarkClipboard(w http.ResponseWriter, r *http.Request) {
	var tags []string
	if t := r.URL.Query().Get("tags"); t != "" {
		tags = []string{t}
	}
	b, err := s.svc.BookmarkFromClipboard(r.Context(), tags)
	s.writeBookmark(w, b, err)
}

func (s *Server) handleCaptureHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.CaptureHealth(r.Context()))
}

func ideaSpec(recording bool) keyimg.KeySpec {
	if recording {
		return keyimg.KeySpec{Background: colKeyFail, Lines: []string{"IDEA", "REC", "tap to stop"}}
	}
	return keyimg.KeySpec{Background: colKeyNeutral, Lines: []string{"IDEA", "record"}}
}

func ideasInboxSpec(n int) keyimg.KeySpec {
	if n == 0 {
		return keyimg.KeySpec{Background: colKeyNeutral, Lines: []string{"IDEAS", "0 new"}}
	}
	return keyimg.KeySpec{Background: colProject, Lines: []string{"IDEAS", strconv.Itoa(n) + " new"}}
}

func (s *Server) handleIdeaImage(w http.ResponseWriter, _ *http.Request) {
	writePNG(w, keyimg.Render(ideaSpec(s.svc.Recording())))
}

func (s *Server) handleIdeasInboxImage(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.IdeasInboxCount(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writePNG(w, keyimg.Render(ideasInboxSpec(n)))
}
