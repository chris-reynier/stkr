package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// ---- inputs ----

type switchIn struct {
	Context string `json:"context" jsonschema:"context id to start tracking, for example a project id; see list_contexts"`
	Note    string `json:"note,omitempty" jsonschema:"optional short note for the time block"`
}

type emptyIn struct{}

type dayIn struct {
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, today or yesterday; default today"`
}

type weekIn struct {
	Week string `json:"week,omitempty" jsonschema:"ISO week such as 2026-W47; default the current week"`
}

type addIdeaIn struct {
	Text string `json:"text" jsonschema:"the idea, action or decision text (max 10 KB)"`
	Tag  string `json:"tag,omitempty" jsonschema:"pro (default) or personal; personal ideas are never shown back to agents"`
	Type string `json:"type,omitempty" jsonschema:"note (default, an idea), action or decision (a follow-up item for the current project)"`
}

type listIdeasIn struct {
	Status string `json:"status,omitempty" jsonschema:"new, triaged, done or archived; default any"`
	Tag    string `json:"tag,omitempty" jsonschema:"pro or personal; personal ideas are never returned"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum ideas to return, newest first (default 50, max 200)"`
}

type triageIn struct {
	ID      int64   `json:"id" jsonschema:"idea id from list_ideas"`
	Status  *string `json:"status,omitempty" jsonschema:"new, triaged, done or archived"`
	Project *string `json:"project,omitempty" jsonschema:"project context id to file the idea under; empty clears it"`
	Tag     *string `json:"tag,omitempty" jsonschema:"pro or personal"`
	Title   *string `json:"title,omitempty" jsonschema:"short title"`
}

type listItemsIn struct {
	Status  string `json:"status,omitempty" jsonschema:"open or done; default any"`
	Project string `json:"project,omitempty" jsonschema:"project context id"`
	Since   string `json:"since,omitempty" jsonschema:"only items captured on or after this date (YYYY-MM-DD, today, yesterday)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum items to return, newest first (default 50, max 200)"`
}

type digestIn struct {
	Kind string `json:"kind" jsonschema:"start (morning brief prompt) or close (end-of-day prompt)"`
}

type contextsIn struct {
	IncludeInactive bool `json:"include_inactive,omitempty" jsonschema:"also return parked (inactive) contexts"`
}

type agentEventIn struct {
	Instance    string `json:"instance" jsonschema:"stable id of the agent window or session"`
	Event       string `json:"event,omitempty" jsonschema:"state (default), focused or closed"`
	State       string `json:"state,omitempty" jsonschema:"idle, working, action_required, done or error"`
	Slot        *int   `json:"slot,omitempty" jsonschema:"Stream Deck slot number, if any"`
	Workspace   string `json:"workspace,omitempty" jsonschema:"workspace folder; mapped to a context when it matches a context workdir"`
	WindowTitle string `json:"window_title,omitempty" jsonschema:"title of the agent window"`
	Summary     string `json:"summary,omitempty" jsonschema:"what the agent is doing (cut to 200 characters)"`
}

// ---- outputs ----

type contextOut struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Kind        domain.ContextKind `json:"kind"`
	TempoTicket string             `json:"tempo_ticket,omitempty"`
	Active      bool               `json:"active"`
}

type nowOut struct {
	Tracking    bool            `json:"tracking"`
	Context     *contextOut     `json:"context,omitempty"`
	EventID     int64           `json:"event_id,omitempty"`
	Since       time.Time       `json:"since,omitzero"`
	Elapsed     domain.Duration `json:"elapsed_seconds"`
	IdleSuspect bool            `json:"idle_suspect"`
	Note        string          `json:"note,omitempty"`
	Today       domain.Duration `json:"today_counted_seconds"`
}

type ideaOut struct {
	ID               int64             `json:"id"`
	TS               time.Time         `json:"ts"`
	ContextID        string            `json:"context_id,omitempty"`
	Tag              string            `json:"tag"`
	Text             string            `json:"text,omitempty"`
	Title            string            `json:"title,omitempty"`
	SuggestedProject string            `json:"suggested_project,omitempty"`
	Status           domain.IdeaStatus `json:"status"`
	Source           string            `json:"source,omitempty"`
}

type addIdeaOut struct {
	Idea    *ideaOut     `json:"idea,omitempty"`
	Item    *domain.Item `json:"item,omitempty"`
	Created bool         `json:"created"`
}

type ideasOut struct {
	Ideas []ideaOut `json:"ideas"`
	Count int       `json:"count"`
}

type itemsOut struct {
	Items []domain.Item `json:"items"`
	Count int           `json:"count"`
}

type digestOut struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Chars int    `json:"chars"`
}

type contextsOut struct {
	Contexts []contextOut `json:"contexts"`
	Count    int          `json:"count"`
}

type agentEventOut struct {
	Instance     string `json:"instance,omitempty"`
	State        string `json:"state,omitempty"`
	ContextID    string `json:"context_id,omitempty"`
	ContextLabel string `json:"context_label,omitempty"`
	Mapped       bool   `json:"mapped"`
	Switched     bool   `json:"switched"`
	Closed       bool   `json:"closed"`
}

// ---- registration ----

func readOnly(title string) *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: &f}
}

func writes(title string, idempotent bool) *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &f, IdempotentHint: idempotent, OpenWorldHint: &f}
}

// tool registers h under t. The handler returns the output and a one-line
// summary; errors become MCP tool errors "[code] message".
func tool[In, Out any](a *adapter, s *mcp.Server, t *mcp.Tool, h func(ctx context.Context, in In) (Out, string, error)) {
	a.tools = append(a.tools, t)
	mcp.AddTool(s, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, summary, err := h(ctx, in)
		if err != nil {
			var zero Out
			return nil, zero, a.toolError(t.Name, err)
		}
		return &mcp.CallToolResult{Content: text(summary)}, out, nil
	})
}

func (a *adapter) addTools(s *mcp.Server) {
	tool(a, s, &mcp.Tool{Name: "switch_context", Annotations: writes("Switch context", true),
		Description: "Start tracking time on a context (project or mode). Ends the current block. Switching to the context already active is a no-op."},
		a.switchContext)
	tool(a, s, &mcp.Tool{Name: "stop", Annotations: writes("Stop tracking", true),
		Description: "Stop tracking time (switches to the off context)."},
		a.stop)
	tool(a, s, &mcp.Tool{Name: "get_now", Annotations: readOnly("What is running now"),
		Description: "Return the context being tracked, since when, and the counted time today."},
		a.getNow)
	tool(a, s, &mcp.Tool{Name: "get_day", Annotations: readOnly("Day report"),
		Description: "Return the time report of one day: events, blocks, gaps and totals per context. Notes of personal and system contexts are removed."},
		a.getDay)
	tool(a, s, &mcp.Tool{Name: "get_week", Annotations: readOnly("Week report"),
		Description: "Return the time report of one ISO week: seven day reports and totals per context. Totals are unrounded."},
		a.getWeek)
	tool(a, s, &mcp.Tool{Name: "add_idea", Annotations: writes("Capture an idea", false),
		Description: "Capture an idea. With type action or decision it becomes a follow-up item of the current project instead. Capture is instant; AI cleanup runs later."},
		a.addIdea)
	tool(a, s, &mcp.Tool{Name: "list_ideas", Annotations: readOnly("List ideas"),
		Description: "List captured ideas, newest first. Personal ideas are never returned."},
		a.listIdeas)
	tool(a, s, &mcp.Tool{Name: "triage_idea", Annotations: writes("Triage an idea", true),
		Description: "Change the status, project, tag or title of an idea. Only the fields you pass change."},
		a.triageIdea)
	tool(a, s, &mcp.Tool{Name: "list_items", Annotations: readOnly("List items"),
		Description: "List follow-up items (action, decision, note, followup, risk), newest first. Items of personal and system projects are never returned."},
		a.listItems)
	tool(a, s, &mcp.Tool{Name: "get_digest", Annotations: readOnly("Start or close day prompt"),
		Description: "Return the Start Day or Close Day prompt text, including the fence format the reply must use. Does not copy to the clipboard, switch context or write files."},
		a.getDigest)
	tool(a, s, &mcp.Tool{Name: "list_contexts", Annotations: readOnly("List contexts"),
		Description: "List the contexts (projects, modes, personal, system) you can switch to."},
		a.listContexts)
	tool(a, s, &mcp.Tool{Name: "report_agent_event", Annotations: writes("Report an agent event", true),
		Description: "Report the state of a coding agent window (idle, working, action_required, done, error) so STKR can show it and map its workspace to a context."},
		a.reportAgentEvent)
	a.addReviewTools(s)
}

// ---- handlers ----

func (a *adapter) switchContext(ctx context.Context, in switchIn) (domain.SwitchResult, string, error) {
	if err := checkText("note", in.Note, maxShort); err != nil {
		return domain.SwitchResult{}, "", err
	}
	if strings.TrimSpace(in.Context) == "" {
		return domain.SwitchResult{}, "", invalid("context is required")
	}
	res, err := a.svc.Switch(ctx, strings.TrimSpace(in.Context), domain.SourceMCP, in.Note)
	if err != nil {
		return res, "", err
	}
	msg := "Now tracking " + res.Event.ContextID
	if !res.Created {
		msg = "Already tracking " + res.Event.ContextID
	}
	return res, msg, nil
}

func (a *adapter) stop(ctx context.Context, _ emptyIn) (domain.SwitchResult, string, error) {
	res, err := a.svc.Stop(ctx, domain.SourceMCP)
	if err != nil {
		return res, "", err
	}
	if !res.Created {
		return res, "Nothing was running", nil
	}
	return res, "Stopped tracking", nil
}

func (a *adapter) getNow(ctx context.Context, _ emptyIn) (nowOut, string, error) {
	s, err := a.svc.Now(ctx)
	if err != nil {
		return nowOut{}, "", err
	}
	n := nowOut{Tracking: s.Tracking, EventID: s.EventID, Since: s.Since, Elapsed: s.Elapsed, IdleSuspect: s.IdleSuspect, Note: s.Note, Today: s.Today}
	if s.Context != nil {
		n.Context = &contextOut{ID: s.Context.ID, Label: s.Context.Label, Kind: s.Context.Kind, TempoTicket: s.Context.TempoTicket, Active: s.Context.Active}
	}
	if !n.Tracking || n.Context == nil {
		return n, "Not tracking. Counted today: " + domain.FormatDuration(n.Today.D()), nil
	}
	return n, fmt.Sprintf("Tracking %s for %s. Counted today: %s", n.Context.ID,
		domain.FormatDuration(n.Elapsed.D()), domain.FormatDuration(n.Today.D())), nil
}

// privateContexts returns the ids of contexts whose content is private
// (personal and system).
func (a *adapter) privateContexts(ctx context.Context) (map[string]bool, error) {
	all, err := a.svc.ListContexts(ctx, service.ContextFilter{IncludeParked: true})
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, c := range all {
		if !c.Kind.Exportable() {
			m[c.ID] = true
		}
	}
	return m, nil
}

func scrubDay(d *domain.DayReport, priv map[string]bool) {
	for i := range d.Events {
		if priv[d.Events[i].ContextID] {
			d.Events[i].Note = ""
		}
	}
	for i := range d.Blocks {
		if priv[d.Blocks[i].ContextID] {
			d.Blocks[i].Note = ""
		}
	}
}

func (a *adapter) getDay(ctx context.Context, in dayIn) (domain.DayReport, string, error) {
	d, err := a.svc.Day(ctx, in.Date)
	if err != nil {
		return d, "", err
	}
	priv, err := a.privateContexts(ctx)
	if err != nil {
		return d, "", err
	}
	scrubDay(&d, priv)
	return d, fmt.Sprintf("%s: counted %s, %s, %s", d.Date, domain.FormatDuration(d.Counted.D()),
		plural(len(d.Blocks), "block"), plural(len(d.Gaps), "gap")), nil
}

func (a *adapter) getWeek(ctx context.Context, in weekIn) (domain.WeekReport, string, error) {
	w, err := a.svc.Week(ctx, in.Week)
	if err != nil {
		return w, "", err
	}
	priv, err := a.privateContexts(ctx)
	if err != nil {
		return w, "", err
	}
	for i := range w.Days {
		scrubDay(&w.Days[i], priv)
	}
	return w, fmt.Sprintf("%s: counted %s over %s", w.Week, domain.FormatDuration(w.Counted.D()),
		plural(w.WorkingDays, "working day")), nil
}

// ideaView converts an idea for agents. The second result is true when the
// idea is private (personal tag, or captured in a personal or system
// context); its text is then left out.
func ideaView(i domain.Idea, priv map[string]bool) (ideaOut, bool) {
	private := i.Tag == "personal" || priv[i.ContextID]
	o := ideaOut{ID: i.ID, TS: i.TS, ContextID: i.ContextID, Tag: i.Tag, Status: i.Status, Source: i.Source}
	if private {
		return o, true
	}
	o.Text = i.CleanText
	if o.Text == "" {
		o.Text = i.RawText
	}
	o.Title, o.SuggestedProject = i.Title, i.SuggestedProject
	return o, false
}

func (a *adapter) addIdea(ctx context.Context, in addIdeaIn) (addIdeaOut, string, error) {
	if err := checkText("text", in.Text, MaxTextBytes); err != nil {
		return addIdeaOut{}, "", err
	}
	res, err := a.svc.AddIdea(ctx, service.IdeaInput{Text: in.Text, Tag: in.Tag, Type: in.Type, Via: string(domain.SourceMCP)})
	if err != nil {
		return addIdeaOut{}, "", err
	}
	out := addIdeaOut{Item: res.Item, Created: res.Created}
	switch {
	case res.Idea != nil:
		priv, err := a.privateContexts(ctx)
		if err != nil {
			return out, "", err
		}
		v, _ := ideaView(*res.Idea, priv)
		out.Idea = &v
		return out, fmt.Sprintf("Captured idea %d", v.ID), nil
	case res.Item != nil && !res.Created:
		return out, fmt.Sprintf("Item %d already existed", res.Item.ID), nil
	case res.Item != nil:
		return out, fmt.Sprintf("Captured %s item %d", res.Item.Type, res.Item.ID), nil
	}
	return out, "Captured", nil
}

func (a *adapter) listIdeas(ctx context.Context, in listIdeasIn) (ideasOut, string, error) {
	limit, err := checkLimit(in.Limit)
	if err != nil {
		return ideasOut{}, "", err
	}
	if strings.EqualFold(strings.TrimSpace(in.Tag), "personal") {
		return ideasOut{Ideas: []ideaOut{}}, "0 ideas (personal ideas are never shown to agents)", nil
	}
	list, err := a.svc.ListIdeas(ctx, service.IdeaFilter{Status: domain.IdeaStatus(strings.ToLower(strings.TrimSpace(in.Status))), Tag: in.Tag})
	if err != nil {
		return ideasOut{}, "", err
	}
	priv, err := a.privateContexts(ctx)
	if err != nil {
		return ideasOut{}, "", err
	}
	out := ideasOut{Ideas: []ideaOut{}}
	for _, i := range list {
		if len(out.Ideas) >= limit {
			break
		}
		if v, hidden := ideaView(i, priv); !hidden {
			out.Ideas = append(out.Ideas, v)
		}
	}
	out.Count = len(out.Ideas)
	return out, plural(out.Count, "idea"), nil
}

func (a *adapter) triageIdea(ctx context.Context, in triageIn) (ideaOut, string, error) {
	for f, v := range map[string]*string{"project": in.Project, "tag": in.Tag, "title": in.Title, "status": in.Status} {
		if v != nil {
			if err := checkText(f, *v, maxShort); err != nil {
				return ideaOut{}, "", err
			}
		}
	}
	priv, err := a.privateContexts(ctx)
	if err != nil {
		return ideaOut{}, "", err
	}
	cur, err := a.svc.GetIdea(ctx, in.ID)
	if err != nil {
		return ideaOut{}, "", err
	}
	if _, hidden := ideaView(cur, priv); hidden {
		return ideaOut{}, "", domain.New(domain.ErrNotFound, "idea not found")
	}
	p := service.IdeaPatch{Project: in.Project, Tag: in.Tag, Title: in.Title}
	if in.Status != nil {
		st := domain.IdeaStatus(strings.ToLower(strings.TrimSpace(*in.Status)))
		p.Status = &st
	}
	i, err := a.svc.TriageIdea(ctx, in.ID, p)
	if err != nil {
		return ideaOut{}, "", err
	}
	v, _ := ideaView(i, priv)
	return v, fmt.Sprintf("Updated idea %d (%s)", v.ID, v.Status), nil
}

func (a *adapter) listItems(ctx context.Context, in listItemsIn) (itemsOut, string, error) {
	limit, err := checkLimit(in.Limit)
	if err != nil {
		return itemsOut{}, "", err
	}
	list, err := a.svc.ListItems(ctx, service.ItemFilter{Status: in.Status, Project: in.Project, Since: in.Since})
	if err != nil {
		return itemsOut{}, "", err
	}
	out := itemsOut{Items: []domain.Item{}}
	for i := len(list) - 1; i >= 0 && len(out.Items) < limit; i-- {
		out.Items = append(out.Items, list[i])
	}
	out.Count = len(out.Items)
	return out, plural(out.Count, "item"), nil
}

func (a *adapter) getDigest(ctx context.Context, in digestIn) (digestOut, string, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	txt, err := a.svc.Digest(ctx, kind)
	if err != nil {
		return digestOut{}, "", err
	}
	out := digestOut{Kind: kind, Text: txt, Chars: len([]rune(txt))}
	return out, fmt.Sprintf("%s prompt, %d characters", kind, out.Chars), nil
}

func (a *adapter) listContexts(ctx context.Context, in contextsIn) (contextsOut, string, error) {
	list, err := a.svc.ListContexts(ctx, service.ContextFilter{IncludeParked: in.IncludeInactive})
	if err != nil {
		return contextsOut{}, "", err
	}
	out := contextsOut{Contexts: make([]contextOut, 0, len(list))}
	for _, c := range list {
		out.Contexts = append(out.Contexts, contextOut{ID: c.ID, Label: c.Label, Kind: c.Kind, TempoTicket: c.TempoTicket, Active: c.Active})
	}
	out.Count = len(out.Contexts)
	return out, plural(out.Count, "context"), nil
}

func (a *adapter) reportAgentEvent(ctx context.Context, in agentEventIn) (agentEventOut, string, error) {
	for f, v := range map[string]string{"instance": in.Instance, "workspace": in.Workspace, "window_title": in.WindowTitle, "summary": in.Summary} {
		if err := checkText(f, v, 1024); err != nil {
			return agentEventOut{}, "", err
		}
	}
	ev := service.AgentEvent{
		Instance: in.Instance, Event: strings.ToLower(strings.TrimSpace(in.Event)),
		State: domain.AgentState(strings.ToLower(strings.TrimSpace(in.State))), Slot: in.Slot,
		Workspace: in.Workspace, WindowTitle: in.WindowTitle, Summary: in.Summary,
	}
	if ev.Event == "" {
		ev.Event = service.AgentEventState
	}
	res, err := a.svc.AgentEvent(ctx, ev)
	if err != nil {
		return agentEventOut{}, "", err
	}
	if res.Instance == nil {
		return agentEventOut{Closed: true}, "Instance closed", nil
	}
	v := res.Instance
	out := agentEventOut{Instance: v.ID, State: string(v.State), ContextID: v.ContextID, ContextLabel: v.ContextLabel, Mapped: v.Mapped, Switched: res.Switched}
	return out, fmt.Sprintf("Recorded %s for %s", out.State, oneLine(v.Label)), nil
}
