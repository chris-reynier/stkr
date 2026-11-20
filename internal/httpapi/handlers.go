package httpapi

import (
	"net/http"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// source decides the event source: the Stream Deck marks itself with the
// X-STKR-Source header or ?src=deck; everything else is "api".
func source(r *http.Request) domain.EventSource {
	if r.Header.Get("X-STKR-Source") == "deck" || r.URL.Query().Get("src") == "deck" {
		return domain.SourceDeck
	}
	return domain.SourceAPI
}

func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.Switch(r.Context(), r.PathValue("ctx"), source(r), r.URL.Query().Get("note"))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.Stop(r.Context(), source(r))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleNow(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.Now(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleContexts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.svc.ListContexts(r.Context(), service.ContextFilter{
		IncludeParked: q.Get("parked") == "1" || q.Get("parked") == "true",
		Kind:          domain.ContextKind(q.Get("kind")),
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contexts": list})
}
