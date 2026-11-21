package web

import (
	"net/http"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func init() {
	Register(Page{ID: "panel", Title: "Panel", Path: "/ui/panel", Order: 95, Hidden: true, Compact: true, Mount: mountPanel})
}

// panelFilterAbove is the number of contexts above which the switch list
// gets a filter box.
const panelFilterAbove = 8

// panelContext is one button of the switch list.
type panelContext struct {
	ID, Label string
	Current   bool
}

// panelData is the data of the compact panel page and of its live fragment.
type panelData struct {
	Now      *service.StatusNow
	Contexts []panelContext // filtered, most recently used first
	// Total is the number of switchable contexts before filtering.
	Total      int
	Filter     string
	ShowFilter bool

	CloseDue      bool
	Banner        string
	AgentsWaiting int
	PollSeconds   int
	CanRecord     bool
	Recording     bool
	Pinned        bool
	FocusIdea     bool
	Self          string
}

// panelSelf is where the panel's forms redirect to.
const panelSelf = "/ui/panel"

func mountPanel(ui *UI) {
	ui.Get("GET /ui/panel", func(w http.ResponseWriter, r *http.Request) {
		d, err := ui.panelData(r)
		if err != nil {
			ui.Fail(w, err)
			return
		}
		d.FocusIdea = r.URL.Query().Get("focus") == "idea"
		ui.Render(w, r, "panel", "Panel", d)
	})
	// The part of the panel that changes while it is open. The idea box is
	// outside it so a refresh never discards what the user is typing.
	ui.Get("GET /ui/panel/live", func(w http.ResponseWriter, r *http.Request) {
		d, err := ui.panelData(r)
		if err != nil {
			ui.Fail(w, err)
			return
		}
		ui.Fragment(w, "panel", "live", d)
	})
	ui.Post("POST /ui/import/clipboard", func(r *http.Request, _ string) (string, error) {
		res, err := ui.Svc.ImportClipboard(r.Context())
		if err != nil {
			return "", err
		}
		return res.Summary(), nil
	})
}

func (u *UI) panelData(r *http.Request) (panelData, error) {
	ctx := r.Context()
	st, err := u.Svc.Status(ctx)
	if err != nil {
		return panelData{}, err
	}
	all, err := u.Svc.ListContexts(ctx, service.ContextFilter{})
	if err != nil {
		return panelData{}, err
	}
	cfg := u.Svc.Config()
	d := panelData{
		Now: st.Now, CloseDue: st.CloseDue, AgentsWaiting: st.AgentsWaiting,
		Banner: u.Svc.UpdateBanner(ctx), PollSeconds: cfg.Desktop.PollSeconds,
		Filter: strings.TrimSpace(r.URL.Query().Get("q")), Self: panelSelf, Pinned: true,
	}
	if d.PollSeconds < 2 {
		d.PollSeconds = 5
	}
	// The saved file holds the pin state: the desktop app changes it while
	// the server keeps running with the values it started with.
	if saved, err := u.Svc.ConfigFile(); err == nil {
		d.Pinned = saved.Desktop.PanelOnTop
	}
	health := u.Svc.CaptureHealth(ctx)
	d.Recording = health.Recording
	d.CanRecord = health.Recording || (health.FFmpeg == "ok" && health.Whisper == "ok")

	// Most recently used first (the order of Status), then the others.
	labels := map[string]string{}
	for _, c := range all {
		labels[c.ID] = c.Label
	}
	var ordered []panelContext
	seen := map[string]bool{}
	add := func(id, label string) {
		if seen[id] || id == domain.ContextOff {
			return
		}
		seen[id] = true
		if strings.TrimSpace(label) == "" {
			label = id
		}
		ordered = append(ordered, panelContext{ID: id, Label: label, Current: st.Now != nil && st.Now.ContextID == id})
	}
	for _, c := range st.Contexts {
		add(c.ID, labels[c.ID])
	}
	for _, c := range all {
		if c.Active {
			add(c.ID, c.Label)
		}
	}
	d.Total = len(ordered)
	d.ShowFilter = d.Total > panelFilterAbove
	f := strings.ToLower(d.Filter)
	for _, c := range ordered {
		if f == "" || strings.Contains(strings.ToLower(c.Label), f) || strings.Contains(strings.ToLower(c.ID), f) {
			d.Contexts = append(d.Contexts, c)
		}
	}
	return d, nil
}
