package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

// Agent timing rules (contract agent-events.md v1).
const (
	// AgentDoneIdleAfter is how long a done instance stays done without events.
	AgentDoneIdleAfter = 30 * time.Minute
	// AgentStaleAfter is how long any instance may be silent before it is idle.
	AgentStaleAfter = 24 * time.Hour
	// MaxAgentSummary is the longest stored summary; longer ones are cut.
	MaxAgentSummary = 200
	// MaxAgentSync is the largest accepted sync set.
	MaxAgentSync = 200

	maxAgentID    = 128
	maxAgentPath  = 1000
	maxAgentTitle = 500
	focusLimit    = 10 * time.Second
)

// Agent event kinds.
const (
	AgentEventState   = "state"
	AgentEventFocused = "focused"
	AgentEventClosed  = "closed"
)

// Results of GoNext / GoAgent.
const (
	GoFocused  = "focused"
	GoLaunched = "launched"
	GoNone     = "none"
	GoFailed   = "failed"
	GoPending  = "pending"
)

// AgentEvent is a plugin or hook report about one instance. For sync, Event
// is ignored and State is required.
type AgentEvent struct {
	Instance    string            `json:"instance"`
	Event       string            `json:"event,omitempty"`
	State       domain.AgentState `json:"state,omitempty"`
	Slot        *int              `json:"slot,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	WindowTitle string            `json:"window_title,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	// TS is accepted for information. STKR stamps instances with its own clock.
	TS string `json:"ts,omitempty"`
}

// AgentView is an instance with its resolved context and display label.
type AgentView struct {
	domain.AgentInstance
	// ContextLabel is empty when the workspace maps to no context.
	ContextLabel string `json:"context_label,omitempty"`
	// Label is what to show: context label, else workspace folder, else window title, else id.
	Label string `json:"label"`
	// Mapped tells whether the workspace maps to a context.
	Mapped bool `json:"mapped"`
}

// AgentCounts counts instances per state.
type AgentCounts struct {
	Error          int `json:"error"`
	ActionRequired int `json:"action_required"`
	Done           int `json:"done"`
	Working        int `json:"working"`
	Idle           int `json:"idle"`
}

// Total is the number of instances.
func (c AgentCounts) Total() int { return c.Error + c.ActionRequired + c.Done + c.Working + c.Idle }

// UnknownWorkspace is a workspace that no context's workdir matches.
type UnknownWorkspace struct {
	Workspace string `json:"workspace"`
	Instances int    `json:"instances"`
}

// AgentList is the hub state.
type AgentList struct {
	Instances []AgentView        `json:"instances"`
	Counts    AgentCounts        `json:"counts"`
	Unknown   []UnknownWorkspace `json:"unknown_workspaces"`
	Next      *AgentView         `json:"next"`
}

// AgentEventResult is the outcome of AgentEvent.
type AgentEventResult struct {
	// Instance is nil after a closed event.
	Instance *AgentView `json:"instance"`
	// Switched tells that a time event was recorded (focused, mapped workspace).
	Switched bool `json:"switched"`
}

// GoResult is the outcome of GoNext / GoAgent.
type GoResult struct {
	// Result is focused, launched, none, failed or pending.
	Result   string     `json:"result"`
	Instance *AgentView `json:"instance"`
	Switched bool       `json:"switched"`
	Message  string     `json:"message,omitempty"`
}

// GoOptions tune GoNext / GoAgent.
type GoOptions struct {
	// Source is the event source of the switch (default deck).
	Source domain.EventSource
	// Budget is how long to wait for the window focus or launch before
	// returning "pending" (it then finishes in the background). Zero waits.
	Budget time.Duration
}

// ---- workspace mapping ----

// normPath makes workspace paths comparable: forward slashes, no trailing
// slash, cleaned, and case-folded for Windows-style paths and on Windows and
// macOS.
func normPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" || (len(p) >= 2 && p[1] == ':') {
		p = strings.ToLower(p)
	}
	return p
}

// mapContext returns the id of the context whose workdir is workspace.
func mapContext(ctxs []domain.Context, workspace string) string {
	w := normPath(workspace)
	if w == "" {
		return ""
	}
	for _, c := range ctxs {
		if c.Workdir != "" && normPath(c.Workdir) == w {
			return c.ID
		}
	}
	return ""
}

func (s *Service) allContexts(ctx context.Context) ([]domain.Context, error) {
	return s.ListContexts(ctx, ContextFilter{IncludeParked: true})
}

func folderName(workspace string) string {
	p := strings.ReplaceAll(strings.TrimSpace(workspace), `\`, "/")
	p = path.Base(path.Clean(p))
	if p == "." || p == "/" {
		return ""
	}
	return p
}

func view(a domain.AgentInstance, ctxs []domain.Context) AgentView {
	v := AgentView{AgentInstance: a}
	if id := mapContext(ctxs, a.Workspace); id != "" {
		v.ContextID = id
		for _, c := range ctxs {
			if c.ID == id {
				v.ContextLabel = c.Label
			}
		}
		v.Mapped = true
	} else {
		v.ContextID = ""
	}
	switch {
	case v.ContextLabel != "":
		v.Label = v.ContextLabel
	case folderName(a.Workspace) != "":
		v.Label = folderName(a.Workspace)
	case a.WindowTitle != "":
		v.Label = a.WindowTitle
	default:
		v.Label = a.ID
	}
	return v
}

// ---- validation ----

func trimTo(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// clean validates and normalises an incoming event.
func cleanAgentEvent(ev AgentEvent, syncing bool) (AgentEvent, error) {
	ev.Instance = strings.TrimSpace(ev.Instance)
	if ev.Instance == "" || len(ev.Instance) > maxAgentID || hasControl(ev.Instance) {
		return ev, invalid("instance must be 1-%d characters without control characters", maxAgentID)
	}
	if syncing {
		ev.Event = AgentEventState
	}
	switch ev.Event {
	case AgentEventState, AgentEventFocused, AgentEventClosed:
	default:
		return ev, invalid("event must be state, focused or closed")
	}
	if ev.State != "" && !ev.State.Valid() {
		return ev, invalid("invalid state %q (want idle, working, action_required, done or error)", ev.State)
	}
	if ev.State == "" && ev.Event == AgentEventState {
		return ev, invalid("state is required")
	}
	if ev.Slot != nil && (*ev.Slot < 1 || *ev.Slot > 6) {
		return ev, invalid("slot must be 1-6 or null")
	}
	ev.Workspace = strings.TrimSpace(ev.Workspace)
	if len(ev.Workspace) > maxAgentPath || hasControl(ev.Workspace) {
		return ev, invalid("workspace is too long or has control characters")
	}
	ev.WindowTitle = trimTo(strings.TrimSpace(ev.WindowTitle), maxAgentTitle)
	ev.Summary = trimTo(strings.TrimSpace(ev.Summary), MaxAgentSummary)
	return ev, nil
}

// ---- events ----

// AgentEvent applies one plugin/hook event: it upserts the instance, maps its
// workspace to a context and, for focused, records a deck switch and clears
// done and queued. closed deletes the instance.
func (s *Service) AgentEvent(ctx context.Context, ev AgentEvent) (AgentEventResult, error) {
	ev, err := cleanAgentEvent(ev, false)
	if err != nil {
		return AgentEventResult{}, err
	}
	ctxs, err := s.allContexts(ctx)
	if err != nil {
		return AgentEventResult{}, err
	}
	now := s.clk.Now()
	var cur domain.AgentInstance
	err = s.st.InTx(ctx, func(tx *store.Store) error {
		if ev.Event == AgentEventClosed {
			if err := tx.DeleteAgent(ctx, ev.Instance); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			return nil
		}
		a, err := tx.GetAgent(ctx, ev.Instance)
		if errors.Is(err, domain.ErrNotFound) {
			a = domain.AgentInstance{ID: ev.Instance, State: domain.AgentIdle}
		} else if err != nil {
			return err
		}
		applyAgentFields(&a, ev, ev.Event == AgentEventState)
		a.ContextID = mapContext(ctxs, a.Workspace)
		a.Updated = now
		if ev.Event == AgentEventFocused {
			if a.State == domain.AgentDone {
				a.State = domain.AgentIdle
			}
			a.Queued = false
		}
		cur = a
		return tx.UpsertAgent(ctx, a)
	})
	if err != nil {
		return AgentEventResult{}, err
	}
	s.log.Info("agent event", "instance", ev.Instance, "event", ev.Event, "state", string(ev.State), "summary", len(ev.Summary))
	if ev.Event == AgentEventClosed {
		return AgentEventResult{}, nil
	}
	res := AgentEventResult{}
	v := view(cur, ctxs)
	res.Instance = &v
	if ev.Event == AgentEventFocused && cur.ContextID != "" {
		res.Switched = s.agentSwitch(ctx, cur.ContextID, domain.SourceDeck)
	}
	return res, nil
}

// applyAgentFields copies the fields an event carries onto a. A state event
// replaces the summary; other events keep it unless they carry one.
func applyAgentFields(a *domain.AgentInstance, ev AgentEvent, replaceSummary bool) {
	if ev.State != "" {
		a.State = ev.State
	}
	if ev.Slot != nil {
		a.Slot = *ev.Slot
	}
	if ev.Workspace != "" {
		a.Workspace = ev.Workspace
	}
	if ev.WindowTitle != "" {
		a.WindowTitle = ev.WindowTitle
	}
	if replaceSummary || ev.Summary != "" {
		a.Summary = ev.Summary
	}
}

// agentSwitch records the time event for a focused instance. A parked or
// missing context is not an error for the agent flow; it is only logged.
func (s *Service) agentSwitch(ctx context.Context, ctxID string, src domain.EventSource) bool {
	res, err := s.Switch(ctx, ctxID, src, "")
	if err != nil {
		s.log.Warn("agent switch skipped", "context", ctxID, "err", err)
		return false
	}
	return res.Created
}

// SyncAgents replaces the full instance set with items. Instances missing
// from items are deleted. An instance whose state did not change keeps its
// updated time (so a periodic sync does not keep done instances alive); the
// queued flag is always kept.
func (s *Service) SyncAgents(ctx context.Context, items []AgentEvent) (AgentList, error) {
	if len(items) > MaxAgentSync {
		return AgentList{}, invalid("at most %d instances can be synced", MaxAgentSync)
	}
	seen := make(map[string]bool, len(items))
	clean := make([]AgentEvent, 0, len(items))
	for _, it := range items {
		c, err := cleanAgentEvent(it, true)
		if err != nil {
			return AgentList{}, fmt.Errorf("instance %q: %w", it.Instance, err)
		}
		if seen[c.Instance] {
			return AgentList{}, invalid("instance %q appears twice", c.Instance)
		}
		seen[c.Instance] = true
		clean = append(clean, c)
	}
	ctxs, err := s.allContexts(ctx)
	if err != nil {
		return AgentList{}, err
	}
	now := s.clk.Now()
	err = s.st.InTx(ctx, func(tx *store.Store) error {
		old, err := tx.ListAgents(ctx)
		if err != nil {
			return err
		}
		byID := make(map[string]domain.AgentInstance, len(old))
		for _, a := range old {
			byID[a.ID] = a
			if !seen[a.ID] {
				if err := tx.DeleteAgent(ctx, a.ID); err != nil {
					return err
				}
			}
		}
		for _, ev := range clean {
			a, existed := byID[ev.Instance]
			if !existed {
				a = domain.AgentInstance{ID: ev.Instance, Updated: now}
			}
			prevState := a.State
			// A sync is a full snapshot: absent fields are cleared.
			a.State, a.Slot, a.Workspace, a.WindowTitle, a.Summary = ev.State, 0, ev.Workspace, ev.WindowTitle, ev.Summary
			if ev.Slot != nil {
				a.Slot = *ev.Slot
			}
			a.ContextID = mapContext(ctxs, a.Workspace)
			if !existed || prevState != a.State {
				a.Updated = now
			}
			if err := tx.UpsertAgent(ctx, a); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return AgentList{}, err
	}
	s.log.Info("agent sync", "instances", len(clean))
	return s.Agents(ctx)
}

// ---- reading ----

func tier(st domain.AgentState) int {
	switch st {
	case domain.AgentError:
		return 0
	case domain.AgentActionRequired:
		return 1
	case domain.AgentDone:
		return 2
	}
	return 3
}

// pickNext returns the index of the instance to attend to next, or -1. Order:
// error, action_required, done, then everything else; within a state queued
// instances first, then the oldest updated, then the lowest slot, then id.
func pickNext(list []domain.AgentInstance) int {
	best := -1
	for i, a := range list {
		if best < 0 || agentBefore(a, list[best]) {
			best = i
		}
	}
	return best
}

func agentBefore(a, b domain.AgentInstance) bool {
	if ta, tb := tier(a.State), tier(b.State); ta != tb {
		return ta < tb
	}
	if a.Queued != b.Queued {
		return a.Queued
	}
	if !a.Updated.Equal(b.Updated) {
		return a.Updated.Before(b.Updated)
	}
	if a.Slot != b.Slot && a.Slot != 0 && b.Slot != 0 {
		return a.Slot < b.Slot
	}
	return a.ID < b.ID
}

func slotLess(a, b domain.AgentInstance) bool {
	if (a.Slot == 0) != (b.Slot == 0) {
		return a.Slot != 0
	}
	if a.Slot != b.Slot {
		return a.Slot < b.Slot
	}
	return a.ID < b.ID
}

// Agents returns the hub state: all instances (by slot), counts per state,
// unknown workspaces and the instance Next would pick.
func (s *Service) Agents(ctx context.Context) (AgentList, error) {
	list, err := s.st.ListAgents(ctx)
	if err != nil {
		return AgentList{}, err
	}
	ctxs, err := s.allContexts(ctx)
	if err != nil {
		return AgentList{}, err
	}
	out := AgentList{Instances: []AgentView{}, Unknown: []UnknownWorkspace{}}
	if i := pickNext(list); i >= 0 {
		v := view(list[i], ctxs)
		out.Next = &v
	}
	sort.Slice(list, func(i, j int) bool { return slotLess(list[i], list[j]) })
	unknown := map[string]*UnknownWorkspace{}
	for _, a := range list {
		v := view(a, ctxs)
		out.Instances = append(out.Instances, v)
		switch a.State {
		case domain.AgentError:
			out.Counts.Error++
		case domain.AgentActionRequired:
			out.Counts.ActionRequired++
		case domain.AgentDone:
			out.Counts.Done++
		case domain.AgentWorking:
			out.Counts.Working++
		default:
			out.Counts.Idle++
		}
		if !v.Mapped && a.Workspace != "" {
			k := normPath(a.Workspace)
			if unknown[k] == nil {
				unknown[k] = &UnknownWorkspace{Workspace: a.Workspace}
			}
			unknown[k].Instances++
		}
	}
	for _, u := range unknown {
		out.Unknown = append(out.Unknown, *u)
	}
	sort.Slice(out.Unknown, func(i, j int) bool { return out.Unknown[i].Workspace < out.Unknown[j].Workspace })
	return out, nil
}

// Next returns the instance to attend to next, or nil when there are none.
func (s *Service) Next(ctx context.Context) (*AgentView, error) {
	l, err := s.Agents(ctx)
	return l.Next, err
}

// ---- actions ----

// Queue marks an instance to be picked first within its state.
func (s *Service) Queue(ctx context.Context, id string) (AgentView, error) {
	var a domain.AgentInstance
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		var err error
		if a, err = tx.GetAgent(ctx, id); err != nil {
			return err
		}
		a.Queued = true
		return tx.UpsertAgent(ctx, a)
	})
	if err != nil {
		return AgentView{}, err
	}
	ctxs, err := s.allContexts(ctx)
	return view(a, ctxs), err
}

// AckDone sets every done instance to idle and returns how many changed.
func (s *Service) AckDone(ctx context.Context) (int, error) {
	n := 0
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		list, err := tx.ListAgents(ctx)
		if err != nil {
			return err
		}
		for _, a := range list {
			if a.State == domain.AgentDone {
				a.State = domain.AgentIdle
				if err := tx.UpsertAgent(ctx, a); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}

// SweepAgents applies the idle rules: done with no event for 30 minutes, or
// any instance silent for 24 hours, becomes idle. It returns the number changed.
func (s *Service) SweepAgents(ctx context.Context) (int, error) {
	now := s.clk.Now()
	n := 0
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		list, err := tx.ListAgents(ctx)
		if err != nil {
			return err
		}
		for _, a := range list {
			age := now.Sub(a.Updated)
			stale := (a.State == domain.AgentDone && age >= AgentDoneIdleAfter) || age >= AgentStaleAfter
			if a.State == domain.AgentIdle || !stale {
				continue
			}
			a.State = domain.AgentIdle
			if err := tx.UpsertAgent(ctx, a); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if n > 0 {
		s.log.Info("agent sweep", "idled", n)
	}
	return n, err
}

// MapWorkspace sets the workdir of a context to workspace and re-maps the
// instances. It returns the number of instances now mapped to the context.
func (s *Service) MapWorkspace(ctx context.Context, workspace, contextID string) (int, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || len(workspace) > maxAgentPath {
		return 0, invalid("workspace is required")
	}
	if _, err := s.UpdateContext(ctx, contextID, ContextPatch{Workdir: &workspace}); err != nil {
		return 0, err
	}
	n := 0
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		list, err := tx.ListAgents(ctx)
		if err != nil {
			return err
		}
		for _, a := range list {
			if normPath(a.Workspace) == normPath(workspace) {
				a.ContextID = contextID
				if err := tx.UpsertAgent(ctx, a); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}

// GoNext picks the next instance (see Next), logs the switch to its context,
// clears its done and queued flags and focuses its window, falling back to
// opening the workspace with the launcher.
func (s *Service) GoNext(ctx context.Context, opt GoOptions) (GoResult, error) {
	n, err := s.Next(ctx)
	if err != nil {
		return GoResult{}, err
	}
	if n == nil {
		return GoResult{Result: GoNone, Message: "no agent instances"}, nil
	}
	return s.GoAgent(ctx, n.ID, opt)
}

// GoAgent does for instance id what GoNext does for the next one.
func (s *Service) GoAgent(ctx context.Context, id string, opt GoOptions) (GoResult, error) {
	if opt.Source == "" {
		opt.Source = domain.SourceDeck
	}
	ctxs, err := s.allContexts(ctx)
	if err != nil {
		return GoResult{}, err
	}
	now := s.clk.Now()
	var a domain.AgentInstance
	err = s.st.InTx(ctx, func(tx *store.Store) error {
		var err error
		if a, err = tx.GetAgent(ctx, id); err != nil {
			return err
		}
		if a.State == domain.AgentDone {
			a.State = domain.AgentIdle
		}
		a.Queued = false
		a.Updated = now
		return tx.UpsertAgent(ctx, a)
	})
	if err != nil {
		return GoResult{}, err
	}
	v := view(a, ctxs)
	res := GoResult{Instance: &v}
	if v.Mapped {
		res.Switched = s.agentSwitch(ctx, v.ContextID, opt.Source)
	}
	type outcome struct{ result, msg string }
	done := make(chan outcome, 1)
	go func() {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), focusLimit)
		defer cancel()
		r, m := s.focusAgent(fctx, a)
		done <- outcome{r, m}
	}()
	var wait <-chan time.Time
	if opt.Budget > 0 {
		t := time.NewTimer(opt.Budget)
		defer t.Stop()
		wait = t.C
	}
	select {
	case o := <-done:
		res.Result, res.Message = o.result, o.msg
	case <-wait:
		res.Result, res.Message = GoPending, "focus is still in progress"
	}
	s.log.Info("agent go", "instance", id, "result", res.Result, "switched", res.Switched)
	return res, nil
}

// focusAgent tries the window titles of a in turn and then the launcher.
func (s *Service) focusAgent(ctx context.Context, a domain.AgentInstance) (result, msg string) {
	var titles []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		for _, x := range titles {
			if x == t {
				return
			}
		}
		if t != "" {
			titles = append(titles, t)
		}
	}
	add(a.WindowTitle)
	add(folderName(a.Workspace))
	if len(titles) == 0 {
		add(s.cfg.AgentWindowTitle)
	}
	var failure error
	for _, t := range titles {
		err := s.windows.Focus(ctx, t, 0)
		if err == nil {
			return GoFocused, ""
		}
		if errors.Is(err, domain.ErrUnavailable) {
			break
		}
		if !errors.Is(err, domain.ErrNotFound) {
			failure = err
		}
	}
	if a.Workspace != "" {
		if err := s.launcher.OpenWorkspace(ctx, a.Workspace); err != nil {
			return GoFailed, "could not open the workspace: " + err.Error()
		}
		return GoLaunched, ""
	}
	if failure != nil {
		return GoFailed, failure.Error()
	}
	return GoFailed, "no window found and the instance has no workspace to open"
}
