package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func init() {
	Register(Page{ID: "agents", Title: "Agents", Path: "/ui/agents", Order: 30, Mount: mountAgents})
}

type agentsData struct {
	List     service.AgentList
	Contexts []domain.Context // active, for the "map to context" select
}

// goMessage turns a Go result into the text shown after the redirect.
func goMessage(res service.GoResult) (string, error) {
	switch res.Result {
	case service.GoFocused:
		return "Window focused.", nil
	case service.GoLaunched:
		return "Workspace opened.", nil
	case service.GoNone:
		return "No agent instances.", nil
	case service.GoPending:
		return "Still switching.", nil
	}
	msg := res.Message
	if msg == "" {
		msg = "could not reach the agent window"
	}
	return "", domain.New(domain.ErrUnavailable, msg)
}

func mountAgents(ui *UI) {
	ui.Get("GET /ui/agents", func(w http.ResponseWriter, r *http.Request) {
		l, err := ui.Svc.Agents(r.Context())
		if err != nil {
			ui.Fail(w, err)
			return
		}
		cs, err := ui.Svc.ListContexts(r.Context(), service.ContextFilter{})
		if err != nil {
			ui.Fail(w, err)
			return
		}
		var active []domain.Context
		for _, c := range cs {
			if c.ID != domain.ContextOff {
				active = append(active, c)
			}
		}
		ui.Render(w, r, "agents", "Agents", agentsData{List: l, Contexts: active})
	})
	opts := service.GoOptions{Source: domain.SourceUI}
	ui.Post("POST /ui/agents/go", func(r *http.Request, _ string) (string, error) {
		res, err := ui.Svc.GoAgent(r.Context(), r.PostFormValue("id"), opts)
		if err != nil {
			return "", err
		}
		return goMessage(res)
	})
	ui.Post("POST /ui/agents/go-next", func(r *http.Request, _ string) (string, error) {
		res, err := ui.Svc.GoNext(r.Context(), opts)
		if err != nil {
			return "", err
		}
		return goMessage(res)
	})
	ui.Post("POST /ui/agents/queue", func(r *http.Request, _ string) (string, error) {
		_, err := ui.Svc.Queue(r.Context(), r.PostFormValue("id"))
		return "Queued.", err
	})
	ui.Post("POST /ui/agents/ack-done", func(r *http.Request, _ string) (string, error) {
		n, err := ui.Svc.AckDone(r.Context())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d done instance(s) set to idle.", n), nil
	})
	ui.Post("POST /ui/agents/map", func(r *http.Request, _ string) (string, error) {
		n, err := ui.Svc.MapWorkspace(r.Context(), strings.TrimSpace(r.PostFormValue("workspace")), r.PostFormValue("context"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Workspace mapped; %d instance(s) updated.", n), nil
	})
}
