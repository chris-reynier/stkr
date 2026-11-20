package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// ContextOff is the system context that ends tracking. "stop" switches to it.
const ContextOff = "off"

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// ValidContextID reports whether id is a valid context slug.
func ValidContextID(id string) bool { return slugRe.MatchString(id) }

// CheckContextID returns an ErrInvalid error when id is not a valid slug.
func CheckContextID(id string) error {
	if !ValidContextID(id) {
		return New(ErrInvalid, fmt.Sprintf("invalid context id %q: use 1-40 characters a-z, 0-9, '-' or '_', starting with a letter or digit", id))
	}
	return nil
}

// ContextKind classifies a context.
type ContextKind string

// Context kinds.
const (
	KindProject  ContextKind = "project"
	KindMode     ContextKind = "mode"
	KindPersonal ContextKind = "personal"
	KindSystem   ContextKind = "system"
)

// ContextKinds lists the valid kinds.
var ContextKinds = []ContextKind{KindProject, KindMode, KindPersonal, KindSystem}

// Valid reports whether k is a known kind.
func (k ContextKind) Valid() bool { return oneOf(k, ContextKinds) }

// Exportable reports whether time in this kind may go to external timesheets.
// Personal and system contexts are never exported.
func (k ContextKind) Exportable() bool { return k == KindProject || k == KindMode }

// EventSource says where a switch came from.
type EventSource string

// Event sources.
const (
	SourceDeck   EventSource = "deck"
	SourceCLI    EventSource = "cli"
	SourceAPI    EventSource = "api"
	SourceHook   EventSource = "hook"
	SourceMCP    EventSource = "mcp"
	SourceUI     EventSource = "ui"
	SourceSystem EventSource = "system"
)

// EventSources lists the valid sources.
var EventSources = []EventSource{SourceDeck, SourceCLI, SourceAPI, SourceHook, SourceMCP, SourceUI, SourceSystem}

// Valid reports whether s is a known source.
func (s EventSource) Valid() bool { return oneOf(s, EventSources) }

// AgentState is the state of a coding agent instance.
type AgentState string

// Agent states.
const (
	AgentIdle           AgentState = "idle"
	AgentWorking        AgentState = "working"
	AgentActionRequired AgentState = "action_required"
	AgentDone           AgentState = "done"
	AgentError          AgentState = "error"
)

// AgentStates lists the valid agent states.
var AgentStates = []AgentState{AgentIdle, AgentWorking, AgentActionRequired, AgentDone, AgentError}

// Valid reports whether s is a known state.
func (s AgentState) Valid() bool { return oneOf(s, AgentStates) }

// IdeaStatus is the triage status of an idea.
type IdeaStatus string

// Idea statuses.
const (
	IdeaNew      IdeaStatus = "new"
	IdeaTriaged  IdeaStatus = "triaged"
	IdeaDone     IdeaStatus = "done"
	IdeaArchived IdeaStatus = "archived"
)

// IdeaStatuses lists the valid idea statuses.
var IdeaStatuses = []IdeaStatus{IdeaNew, IdeaTriaged, IdeaDone, IdeaArchived}

// Valid reports whether s is a known status.
func (s IdeaStatus) Valid() bool { return oneOf(s, IdeaStatuses) }

// AIState is the AI processing state of an idea.
type AIState string

// AI processing states.
const (
	AIPending AIState = "pending"
	AIDone    AIState = "done"
	AIFailed  AIState = "failed"
	AISkipped AIState = "skipped"
)

// AIStates lists the valid AI states.
var AIStates = []AIState{AIPending, AIDone, AIFailed, AISkipped}

// Valid reports whether s is a known AI state.
func (s AIState) Valid() bool { return oneOf(s, AIStates) }

// ItemType is the type of a daily item.
type ItemType string

// Item types.
const (
	ItemAction   ItemType = "action"
	ItemDecision ItemType = "decision"
	ItemNote     ItemType = "note"
	ItemFollowup ItemType = "followup"
	ItemRisk     ItemType = "risk"
)

// ItemTypes lists the valid item types.
var ItemTypes = []ItemType{ItemAction, ItemDecision, ItemNote, ItemFollowup, ItemRisk}

// Valid reports whether t is a known type.
func (t ItemType) Valid() bool { return oneOf(t, ItemTypes) }

func oneOf[T comparable](v T, list []T) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Duration is a time.Duration that encodes to JSON as whole seconds.
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// MarshalJSON encodes the duration as an integer number of seconds.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(time.Duration(d) / time.Second))
}

// UnmarshalJSON decodes an integer number of seconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s int64
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*d = Duration(time.Duration(s) * time.Second)
	return nil
}

// FormatDuration renders d as "1h25m", "45m" or "0m" (rounded to minutes).
func FormatDuration(d time.Duration) string {
	neg := d < 0
	if neg {
		d = -d
	}
	m := int64(d.Round(time.Minute) / time.Minute)
	s := fmt.Sprintf("%dm", m)
	if m >= 60 {
		s = fmt.Sprintf("%dh%02dm", m/60, m%60)
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Context is something time can be tracked against.
type Context struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Kind        ContextKind `json:"kind"`
	TempoTicket string      `json:"tempo_ticket,omitempty"`
	Active      bool        `json:"active"`
	NotebookURL string      `json:"notebook_url,omitempty"`
	Workdir     string      `json:"workdir,omitempty"`
	Icon        string      `json:"icon,omitempty"`
	Sort        int         `json:"sort"`
}

// Event is a context switch. It starts a time block.
type Event struct {
	ID        int64       `json:"id"`
	TS        time.Time   `json:"ts"`
	ContextID string      `json:"context_id"`
	Source    EventSource `json:"source"`
	Note      string      `json:"note,omitempty"`
	Confirmed bool        `json:"confirmed"`
}

// Block is a span of time attributed to one context, started by an event.
// IdleSuspect blocks count only when Confirmed.
type Block struct {
	EventID     int64       `json:"event_id"`
	ContextID   string      `json:"context_id"`
	Label       string      `json:"label,omitempty"`
	Kind        ContextKind `json:"kind,omitempty"`
	Start       time.Time   `json:"start"`
	End         time.Time   `json:"end"`
	Duration    Duration    `json:"duration_seconds"`
	IdleSuspect bool        `json:"idle_suspect"`
	Confirmed   bool        `json:"confirmed"`
	Counted     bool        `json:"counted"`
	Note        string      `json:"note,omitempty"`
}

// Gap is uncovered working-hours time on a working day. It is never counted.
type Gap struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Duration Duration  `json:"duration_seconds"`
}

// ContextTotal is the counted (unrounded) time of one context.
type ContextTotal struct {
	ContextID   string      `json:"context_id"`
	Label       string      `json:"label"`
	Kind        ContextKind `json:"kind"`
	TempoTicket string      `json:"tempo_ticket,omitempty"`
	Exportable  bool        `json:"exportable"`
	Counted     Duration    `json:"counted_seconds"`
}

// DayReport is the time view of one local day.
type DayReport struct {
	Date        string         `json:"date"`
	Weekday     string         `json:"weekday"`
	WorkingDay  bool           `json:"working_day"`
	WindowStart time.Time      `json:"window_start"`
	WindowEnd   time.Time      `json:"window_end"`
	Events      []Event        `json:"events"`
	Blocks      []Block        `json:"blocks"`
	Gaps        []Gap          `json:"gaps"`
	Totals      []ContextTotal `json:"totals"`
	Counted     Duration       `json:"counted_seconds"`
	IdleSuspect Duration       `json:"idle_suspect_seconds"`
	GapTotal    Duration       `json:"gap_seconds"`
}

// WeekReport is the time view of one ISO week (Monday to Sunday).
type WeekReport struct {
	Week        string         `json:"week"`
	Start       string         `json:"start"`
	End         string         `json:"end"`
	Days        []DayReport    `json:"days"`
	Totals      []ContextTotal `json:"totals"`
	Counted     Duration       `json:"counted_seconds"`
	IdleSuspect Duration       `json:"idle_suspect_seconds"`
	GapTotal    Duration       `json:"gap_seconds"`
	WorkingDays int            `json:"working_days"`
}

// NowStatus is the current tracking state.
type NowStatus struct {
	Tracking    bool      `json:"tracking"`
	Context     *Context  `json:"context,omitempty"`
	EventID     int64     `json:"event_id,omitempty"`
	Since       time.Time `json:"since,omitzero"`
	Elapsed     Duration  `json:"elapsed_seconds"`
	IdleSuspect bool      `json:"idle_suspect"`
	Note        string    `json:"note,omitempty"`
	Today       Duration  `json:"today_counted_seconds"`
}

// SwitchResult is the outcome of a switch or stop.
type SwitchResult struct {
	Event   Event `json:"event"`
	Created bool  `json:"created"`
}

// AgentInstance is a coding agent window known to STKR.
type AgentInstance struct {
	ID          string     `json:"id"`
	Slot        int        `json:"slot"`
	ContextID   string     `json:"context_id,omitempty"`
	Workspace   string     `json:"workspace,omitempty"`
	WindowTitle string     `json:"window_title,omitempty"`
	State       AgentState `json:"state"`
	Summary     string     `json:"summary,omitempty"`
	Updated     time.Time  `json:"updated"`
	Queued      bool       `json:"queued"`
}

// Idea is a captured idea.
type Idea struct {
	ID               int64      `json:"id"`
	TS               time.Time  `json:"ts"`
	ContextID        string     `json:"context_id,omitempty"`
	Tag              string     `json:"tag"`
	RawText          string     `json:"raw_text"`
	CleanText        string     `json:"clean_text,omitempty"`
	Title            string     `json:"title,omitempty"`
	SuggestedProject string     `json:"suggested_project,omitempty"`
	Status           IdeaStatus `json:"status"`
	AudioPath        string     `json:"audio_path,omitempty"`
	Source           string     `json:"source,omitempty"`
	AIState          AIState    `json:"ai_state"`
}

// Bookmark is a saved link.
type Bookmark struct {
	ID      int64     `json:"id"`
	Title   string    `json:"title"`
	URL     string    `json:"url"`
	Tags    []string  `json:"tags"`
	Pinned  bool      `json:"pinned"`
	Created time.Time `json:"created"`
}

// Item is an action, decision, note, follow-up or risk from the daily loop.
type Item struct {
	ID          int64    `json:"id"`
	Date        string   `json:"date"`
	Project     string   `json:"project,omitempty"`
	Type        ItemType `json:"type"`
	Summary     string   `json:"summary"`
	Source      string   `json:"source,omitempty"`
	Due         string   `json:"due,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	Fingerprint string   `json:"fingerprint"`
	Status      string   `json:"status"`
}

// Day holds the daily-loop state of one date.
type Day struct {
	Date       string    `json:"date"`
	Top        [3]string `json:"top"`
	BriefSaved bool      `json:"brief_saved"`
	CloseSaved bool      `json:"close_saved"`
}
