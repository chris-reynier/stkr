package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
	"github.com/chris-reynier/stkr/internal/timeline"
)

// Activity limits (contract activity-events.md v1).
const (
	// MaxActivityBatch is the largest accepted set of events in one request.
	MaxActivityBatch = 5000
	// MaxActivityDuration is the longest single event.
	MaxActivityDuration = 24 * time.Hour
	// ActivityFutureSlack is how far ahead of STKR's clock an event may start.
	ActivityFutureSlack = 5 * time.Minute

	maxActivityApp       = 64
	maxActivityWorkspace = 128
	maxActivityContext   = 64
	maxActivityApps      = 10
	maxActivityPeriods   = 50
	// periodJoin merges two non-active periods of the same state separated
	// by at most this much.
	periodJoin = time.Minute
)

// ActivityInput is one activity event as a client sends it. Only these fields
// exist: window titles, URLs, commands, keystrokes, screen and clipboard
// content have no field and are rejected by the HTTP and MCP decoders.
type ActivityInput struct {
	TS              string `json:"ts"`
	App             string `json:"app"`
	Category        string `json:"category"`
	Workspace       string `json:"workspace,omitempty"`
	ContextID       string `json:"context_id,omitempty"`
	DurationSeconds int64  `json:"duration_seconds"`
	LockState       string `json:"lock_state"`
}

// ActivityResult is the outcome of AddActivity.
type ActivityResult struct {
	Received int `json:"received"`
	// Inserted are new identities, Merged known identities whose duration
	// grew or whose context was set, Unchanged replays.
	Inserted  int `json:"inserted"`
	Merged    int `json:"merged"`
	Unchanged int `json:"unchanged"`
	// OutOfRange events are older than activity.retention_days or start
	// more than five minutes in the future; they are skipped.
	OutOfRange int `json:"out_of_range"`
	// Unmapped events named a context_id that does not exist; they are
	// stored without a context.
	Unmapped int `json:"unmapped"`
	// Pruned is the number of events deleted for being past retention.
	Pruned   int64    `json:"pruned"`
	Warnings []string `json:"warnings"`
}

func badText(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return !utf8.ValidString(s)
}

// looksLikePath reports whether s holds a path or URL rather than a name.
func looksLikePath(s string) bool { return strings.ContainsAny(s, `/\:`) }

// activityID is the identity of an event: the same start, application,
// category, workspace and lock state is the same event.
func activityID(ts time.Time, app string, cat domain.ActivityCategory, ws string, lock domain.LockState) string {
	key := strings.Join([]string{store.FormatTS(ts), strings.ToLower(app), string(cat), strings.ToLower(ws), string(lock)}, "|")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

// validateActivity checks one event; n is its 1-based position for errors.
// Errors never repeat the field values.
func validateActivity(n int, in ActivityInput) (domain.ActivityEvent, error) {
	bad := func(format string, a ...any) error {
		return invalid("event %d: %s", n, fmt.Sprintf(format, a...))
	}
	var e domain.ActivityEvent
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(in.TS))
	if err != nil {
		return e, bad("ts must be an RFC 3339 time with an offset")
	}
	app := strings.TrimSpace(in.App)
	switch {
	case app == "":
		return e, bad("app is required")
	case utf8.RuneCountInString(app) > maxActivityApp || badText(app):
		return e, bad("app must be at most %d printable characters", maxActivityApp)
	case looksLikePath(app):
		return e, bad("app must be an application or executable name, not a path or URL")
	}
	cat := domain.ActivityCategory(strings.ToLower(strings.TrimSpace(in.Category)))
	if !cat.Valid() {
		return e, bad("category must be one of development, meeting, email, administration, browser, break, lock, other")
	}
	ws := strings.TrimSpace(in.Workspace)
	switch {
	case utf8.RuneCountInString(ws) > maxActivityWorkspace || badText(ws):
		return e, bad("workspace must be at most %d printable characters", maxActivityWorkspace)
	case looksLikePath(ws):
		return e, bad("workspace must be a name or id, not a path")
	}
	cid := strings.ToLower(strings.TrimSpace(in.ContextID))
	if utf8.RuneCountInString(cid) > maxActivityContext || badText(cid) {
		return e, bad("context_id must be at most %d characters", maxActivityContext)
	}
	if in.DurationSeconds < 1 || time.Duration(in.DurationSeconds)*time.Second > MaxActivityDuration {
		return e, bad("duration_seconds must be between 1 and %d", int(MaxActivityDuration/time.Second))
	}
	lock := domain.LockState(strings.ToLower(strings.TrimSpace(in.LockState)))
	if !lock.Valid() {
		return e, bad("lock_state must be active, idle, locked or break")
	}
	e = domain.ActivityEvent{
		ID: activityID(ts, app, cat, ws, lock), TS: ts.UTC(), App: app, Category: cat, Workspace: ws,
		ContextID: cid, Duration: domain.Duration(time.Duration(in.DurationSeconds) * time.Second), LockState: lock,
	}
	return e, nil
}

// mapWorkspace finds the context of a workspace name: a context with that id,
// or one whose work folder ends in that name.
func mapWorkspace(ctxs map[string]domain.Context, ws string) string {
	if ws == "" {
		return ""
	}
	if c, ok := ctxs[strings.ToLower(ws)]; ok {
		return c.ID
	}
	ids := make([]string, 0, len(ctxs))
	for id := range ctxs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		wd := strings.TrimRight(strings.ReplaceAll(ctxs[id].Workdir, `\`, "/"), "/")
		if wd != "" && strings.EqualFold(path.Base(wd), ws) {
			return id
		}
	}
	return ""
}

// AddActivity validates and stores activity events from a local client. The
// whole set is checked first: one malformed event rejects the request
// (ErrInvalid) and nothing is stored. Events outside the retention window are
// skipped and counted. Known identities are merged (see store.UpsertActivity),
// so a client may resend its whole day. Event content is never logged.
func (s *Service) AddActivity(ctx context.Context, in []ActivityInput) (ActivityResult, error) {
	res := ActivityResult{Received: len(in), Warnings: []string{}}
	if !s.cfg.Activity.Enabled {
		return res, domain.New(domain.ErrUnavailable, "activity events are turned off (activity.enabled)")
	}
	if len(in) > MaxActivityBatch {
		return res, invalid("at most %d events per request (got %d)", MaxActivityBatch, len(in))
	}
	events := make([]domain.ActivityEvent, 0, len(in))
	for i, raw := range in {
		e, err := validateActivity(i+1, raw)
		if err != nil {
			return res, err
		}
		events = append(events, e)
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return res, err
	}
	now := s.clk.Now()
	cutoff := timeline.Midnight(now, s.loc).AddDate(0, 0, -s.cfg.Activity.RetentionDays)
	err = s.st.InTx(ctx, func(tx *store.Store) error {
		for _, e := range events {
			if e.TS.Before(cutoff) || e.TS.After(now.Add(ActivityFutureSlack)) {
				res.OutOfRange++
				continue
			}
			switch {
			case e.ContextID != "":
				if _, ok := ctxs[e.ContextID]; !ok {
					res.Unmapped++
					e.ContextID = ""
				}
			default:
				e.ContextID = mapWorkspace(ctxs, e.Workspace)
			}
			e.Received = now
			r, err := tx.UpsertActivity(ctx, e)
			if err != nil {
				return err
			}
			switch r {
			case store.ActivityInserted:
				res.Inserted++
			case store.ActivityMerged:
				res.Merged++
			case store.ActivityUnchanged:
				res.Unchanged++
			}
		}
		n, err := tx.DeleteActivityBefore(ctx, cutoff)
		res.Pruned = n
		return err
	})
	if err != nil {
		return res, err
	}
	if res.OutOfRange > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d event(s) outside the accepted range (last %d days, up to 5 minutes ahead) were skipped",
			res.OutOfRange, s.cfg.Activity.RetentionDays))
	}
	if res.Unmapped > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d event(s) named an unknown context_id and were stored without a context", res.Unmapped))
	}
	s.log.Info("activity", "received", res.Received, "inserted", res.Inserted, "merged", res.Merged,
		"unchanged", res.Unchanged, "out_of_range", res.OutOfRange, "pruned", res.Pruned)
	return res, nil
}

// ActivityShare is time per category, application or workspace.
type ActivityShare struct {
	Name      string          `json:"name"`
	ContextID string          `json:"context_id,omitempty"`
	Seconds   domain.Duration `json:"seconds"`
}

// ActivityPeriod is a stretch of idle, locked or break time.
type ActivityPeriod struct {
	Start    time.Time        `json:"start"`
	End      time.Time        `json:"end"`
	State    domain.LockState `json:"state"`
	Duration domain.Duration  `json:"duration_seconds"`
}

// ActivitySummary is the activity evidence of one local day.
type ActivitySummary struct {
	Date   string `json:"date"`
	Events int    `json:"events"`
	// First and Last are the start of the first and the end of the last event.
	First time.Time `json:"first,omitzero"`
	Last  time.Time `json:"last,omitzero"`
	// Time per lock state.
	Active domain.Duration `json:"active_seconds"`
	Idle   domain.Duration `json:"idle_seconds"`
	Locked domain.Duration `json:"locked_seconds"`
	Break  domain.Duration `json:"break_seconds"`
	// Private is active time in personal or system contexts. Its
	// application, category and workspace are not shown.
	Private domain.Duration `json:"private_seconds"`
	// Categories, Apps (top 10) and Workspaces split the active time.
	Categories []ActivityShare `json:"categories"`
	Apps       []ActivityShare `json:"apps"`
	Workspaces []ActivityShare `json:"workspaces"`
	// Periods are the idle, locked and break stretches, oldest first (at
	// most 50; PeriodsDropped tells how many more there were).
	Periods        []ActivityPeriod `json:"periods"`
	PeriodsDropped int              `json:"periods_dropped,omitempty"`
}

func shares(m map[string]time.Duration, ctxOf map[string]string, limit int) []ActivityShare {
	out := make([]ActivityShare, 0, len(m))
	for k, d := range m {
		out = append(out, ActivityShare{Name: k, ContextID: ctxOf[k], Seconds: domain.Duration(d)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds != out[j].Seconds {
			return out[i].Seconds > out[j].Seconds
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ActivityDay summarises the activity events of a local date ("", today,
// yesterday or YYYY-MM-DD).
func (s *Service) ActivityDay(ctx context.Context, date string) (ActivitySummary, error) {
	day, err := s.ParseDate(date)
	if err != nil {
		return ActivitySummary{}, err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return ActivitySummary{}, err
	}
	return s.activitySummary(ctx, day, ctxs)
}

func (s *Service) activitySummary(ctx context.Context, day time.Time, ctxs map[string]domain.Context) (ActivitySummary, error) {
	out := ActivitySummary{Date: day.Format(dateLayout), Categories: []ActivityShare{}, Apps: []ActivityShare{},
		Workspaces: []ActivityShare{}, Periods: []ActivityPeriod{}}
	events, err := s.st.ActivityBetween(ctx, day, timeline.NextMidnight(day, s.loc))
	if err != nil {
		return out, err
	}
	out.Events = len(events)
	cats, apps, wss := map[string]time.Duration{}, map[string]time.Duration{}, map[string]time.Duration{}
	wsCtx := map[string]string{}
	var periods []ActivityPeriod
	joinable := false // the last event was a non-active one
	for _, e := range events {
		d := e.Duration.D()
		end := e.TS.Add(d)
		if out.First.IsZero() {
			out.First = e.TS.In(s.loc)
		}
		if end.After(out.Last) {
			out.Last = end.In(s.loc)
		}
		switch e.LockState {
		case domain.LockActive:
			joinable = false
			if c, ok := ctxs[e.ContextID]; ok && !c.Kind.Exportable() {
				out.Private += domain.Duration(d)
				continue
			}
			out.Active += domain.Duration(d)
			cats[string(e.Category)] += d
			apps[e.App] += d
			if e.Workspace != "" {
				wss[e.Workspace] += d
				if e.ContextID != "" {
					wsCtx[e.Workspace] = e.ContextID
				}
			}
			continue
		case domain.LockIdle:
			out.Idle += domain.Duration(d)
		case domain.LockLocked:
			out.Locked += domain.Duration(d)
		case domain.LockBreak:
			out.Break += domain.Duration(d)
		}
		join := joinable
		joinable = true
		if n := len(periods); join && n > 0 && periods[n-1].State == e.LockState && !e.TS.After(periods[n-1].End.Add(periodJoin)) {
			if end.After(periods[n-1].End) {
				periods[n-1].End = end.In(s.loc)
			}
			continue
		}
		periods = append(periods, ActivityPeriod{Start: e.TS.In(s.loc), End: end.In(s.loc), State: e.LockState})
	}
	for i := range periods {
		periods[i].Duration = domain.Duration(periods[i].End.Sub(periods[i].Start))
	}
	if len(periods) > maxActivityPeriods {
		out.PeriodsDropped = len(periods) - maxActivityPeriods
		periods = periods[:maxActivityPeriods]
	}
	out.Periods = append(out.Periods, periods...)
	out.Categories = shares(cats, nil, 0)
	out.Apps = shares(apps, nil, maxActivityApps)
	out.Workspaces = shares(wss, wsCtx, 0)
	return out, nil
}
