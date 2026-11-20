package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/keyimg"
	"github.com/chris-reynier/stkr/internal/service"
)

// goBudget is how long next/go waits for the window focus before it answers
// "pending" (the focus then finishes in the background).
const goBudget = 2 * time.Second

func (s *Server) agentRoutes() {
	s.mux.HandleFunc("POST /api/agents/event", s.handleAgentEvent)
	s.mux.HandleFunc("POST /api/agents/sync", s.handleAgentSync)
	s.mux.HandleFunc("GET /api/agents", s.handleAgents)
	s.mux.HandleFunc("GET /api/agents/next", s.handleAgentNext)
	s.mux.HandleFunc("POST /api/agents/next/go", s.handleAgentGo)
	s.mux.HandleFunc("POST /api/agents/ack-done", s.handleAgentAck)
	s.mux.HandleFunc("POST /api/agents/{id}/queue", s.handleAgentQueue)
	s.mux.HandleFunc("GET /img/agents.png", s.handleAgentsImage)
}

// readJSON reads a JSON request body. Unknown fields are ignored so newer
// plugins keep working.
func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return err
		}
		return domain.New(domain.ErrInvalid, fmt.Sprintf("request body is not valid JSON: %v", err))
	}
	return nil
}

func (s *Server) handleAgentEvent(w http.ResponseWriter, r *http.Request) {
	var ev service.AgentEvent
	if err := readJSON(r, &ev); err != nil {
		s.writeErr(w, err)
		return
	}
	res, err := s.svc.AgentEvent(r.Context(), ev)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAgentSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Instances []service.AgentEvent `json:"instances"`
	}
	if err := readJSON(r, &body); err != nil {
		s.writeErr(w, err)
		return
	}
	l, err := s.svc.SyncAgents(r.Context(), body.Instances)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	l, err := s.svc.Agents(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleAgentNext(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.Next(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": n})
}

func (s *Server) handleAgentGo(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.GoNext(r.Context(), service.GoOptions{Source: source(r), Budget: goBudget})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleAgentAck(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.AckDone(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": n})
}

func (s *Server) handleAgentQueue(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.Queue(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": v})
}

var (
	colAgentError   = color.RGBA{185, 28, 28, 255}
	colAgentAction  = color.RGBA{234, 88, 12, 255}
	colAgentDone    = color.RGBA{21, 128, 61, 255}
	colAgentWorking = color.RGBA{37, 99, 235, 255}
	colAgentIdle    = color.RGBA{75, 85, 99, 255}
)

// agentsSpec builds the agents key: worst-state colour and word, counts
// (E error, A action required, D done, W working) and the next instance.
func agentsSpec(l service.AgentList) keyimg.KeySpec {
	c := l.Counts
	if c.Total() == 0 {
		return keyimg.KeySpec{Background: colAgentIdle, Lines: []string{"AGENTS", "none"}}
	}
	spec := keyimg.KeySpec{Background: colAgentIdle, Lines: make([]string, 0, 3)}
	word := "IDLE"
	switch {
	case c.Error > 0:
		spec.Background, word = colAgentError, "ERROR"
	case c.ActionRequired > 0:
		spec.Background, word = colAgentAction, "ACTION"
	case c.Done > 0:
		spec.Background, word = colAgentDone, "DONE"
	case c.Working > 0:
		spec.Background, word = colAgentWorking, "WORKING"
	}
	spec.Lines = append(spec.Lines, word, fmt.Sprintf("E%d A%d D%d W%d", c.Error, c.ActionRequired, c.Done, c.Working))
	if l.Next != nil {
		spec.Lines = append(spec.Lines, l.Next.Label)
	}
	return spec
}

func (s *Server) handleAgentsImage(w http.ResponseWriter, r *http.Request) {
	l, err := s.svc.Agents(r.Context())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writePNG(w, keyimg.Render(agentsSpec(l)))
}
