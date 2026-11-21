package service

import (
	"context"
	"sort"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	upd "github.com/chris-reynier/stkr/internal/update"
)

// StatusContextLimit is the maximum number of contexts in Status.
const StatusContextLimit = 15

// statusLookback is how far back events are read to order contexts by use.
const statusLookback = 14 * 24 * time.Hour

// Status is the compact state the desktop app polls (contract status-api.md).
type Status struct {
	Now           *StatusNow      `json:"now"`
	Contexts      []StatusContext `json:"contexts"`
	CloseDue      bool            `json:"close_due"`
	Update        *StatusUpdate   `json:"update"`
	AgentsWaiting int             `json:"agents_waiting"`
	IdeasNew      int             `json:"ideas_new"`
	Version       string          `json:"version"`
	// HostedBy is filled in by the HTTP adapter ("serve" or "desktop").
	HostedBy string `json:"hosted_by"`
}

// StatusNow is the context being tracked.
type StatusNow struct {
	ContextID   string             `json:"context_id"`
	Label       string             `json:"label"`
	Kind        domain.ContextKind `json:"kind"`
	Since       time.Time          `json:"since"`
	ElapsedS    int64              `json:"elapsed_s"`
	IdleSuspect bool               `json:"idle_suspect"`
}

// StatusContext is a context that can be switched to.
type StatusContext struct {
	ID    string             `json:"id"`
	Label string             `json:"label"`
	Kind  domain.ContextKind `json:"kind"`
}

// StatusUpdate is the cached update check.
type StatusUpdate struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

// Status aggregates what the tray needs in one call: the current context,
// the switch list (active contexts except off, most recently used first, at
// most StatusContextLimit), whether the close is due, the cached update, the
// number of agents waiting for approval (action_required) and of new pro ideas.
// update is nil when update.check is off, nothing is cached or this is a
// development build (same rule as the web banner).
func (s *Service) Status(ctx context.Context) (Status, error) {
	out := Status{Contexts: []StatusContext{}, Version: s.currentVersion()}
	day, err := s.Day(ctx, "")
	if err != nil {
		return out, err
	}
	now, err := s.nowFrom(ctx, day)
	if err != nil {
		return out, err
	}
	if now.Tracking && now.Context != nil {
		out.Now = &StatusNow{
			ContextID: now.Context.ID, Label: now.Context.Label, Kind: now.Context.Kind,
			Since: now.Since, ElapsedS: int64(now.Elapsed.D() / time.Second), IdleSuspect: now.IdleSuspect,
		}
	}
	if out.Contexts, err = s.recentContexts(ctx); err != nil {
		return out, err
	}
	cs, err := s.closeStateFrom(ctx, day)
	if err != nil {
		return out, err
	}
	out.CloseDue = cs.Due
	if s.cfg.Update.Check && !upd.IsDev(out.Version) {
		if info, ok := s.UpdateInfo(ctx); ok {
			out.Update = &StatusUpdate{Available: info.Available, Version: info.Latest}
		}
	}
	agents, err := s.st.ListAgents(ctx)
	if err != nil {
		return out, err
	}
	for _, a := range agents {
		if a.State == domain.AgentActionRequired {
			out.AgentsWaiting++
		}
	}
	if out.IdeasNew, err = s.IdeasInboxCount(ctx); err != nil {
		return out, err
	}
	return out, nil
}

// recentContexts lists active contexts except off, the most recently used
// first, then in the usual list order.
func (s *Service) recentContexts(ctx context.Context) ([]StatusContext, error) {
	list, err := s.st.ListContexts(ctx, false)
	if err != nil {
		return nil, err
	}
	now := s.clk.Now()
	evs, err := s.st.EventsBetween(ctx, now.Add(-statusLookback), now.Add(time.Minute))
	if err != nil {
		return nil, err
	}
	last := map[string]int{}
	for i, e := range evs {
		last[e.ContextID] = i + 1
	}
	sort.SliceStable(list, func(i, j int) bool { return last[list[i].ID] > last[list[j].ID] })
	out := []StatusContext{}
	for _, c := range list {
		if c.ID == domain.ContextOff {
			continue
		}
		out = append(out, StatusContext{ID: c.ID, Label: c.Label, Kind: c.Kind})
		if len(out) == StatusContextLimit {
			break
		}
	}
	return out, nil
}
