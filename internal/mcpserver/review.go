package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// maxImportText is the largest text list_unapproved_review_records reads
// (the import limit).
const maxImportText = 256 << 10

type reviewIn struct {
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, not in the future; default the previous working day"`
}

type activityEventIn struct {
	TS              string `json:"ts" jsonschema:"start time, RFC 3339 with offset"`
	App             string `json:"app" jsonschema:"application or executable name (max 64 characters, not a path)"`
	Category        string `json:"category" jsonschema:"development, meeting, email, administration, browser, break, lock or other"`
	Workspace       string `json:"workspace,omitempty" jsonschema:"workspace name or id, not a path (max 128 characters)"`
	ContextID       string `json:"context_id,omitempty" jsonschema:"STKR context id; default mapped from the workspace"`
	DurationSeconds int64  `json:"duration_seconds" jsonschema:"duration in whole seconds, 1 to 86400"`
	LockState       string `json:"lock_state" jsonschema:"active, idle, locked or break"`
}

type activityIn struct {
	Events []activityEventIn `json:"events" jsonschema:"activity events (at most 5000); window titles, URLs and clipboard text are not accepted"`
}

type previewIn struct {
	Text string `json:"text" jsonschema:"answer text holding a stkr-review (or stkr-brief / stkr-close) block, max 256 KB"`
}

func (a *adapter) addReviewTools(s *mcp.Server) {
	tool(a, s, &mcp.Tool{Name: "get_review_context", Annotations: readOnly("Review context of a day"),
		Description: "Return the privacy-bounded review metadata of one working day: tracked blocks, gaps, totals, open items, new ideas, agents, " +
			"local activity, close state and the evidence STKR does not have. Without a date it picks the previous working day. Same data as GET /api/review/context."},
		a.getReviewContext)
	tool(a, s, &mcp.Tool{Name: "report_activity_event", Annotations: writes("Report local activity", true),
		Description: "Store local activity evidence (application category, workspace, lock state, duration). Replays are merged, so the same events may be sent again. " +
			"One malformed event rejects the whole call. Same rules as POST /api/activity/events."},
		a.reportActivity)
	tool(a, s, &mcp.Tool{Name: "get_activity_summary", Annotations: readOnly("Activity summary of a day"),
		Description: "Summarise the local activity of one day: time per lock state, category, application and workspace, and the locked, idle and break periods."},
		a.getActivitySummary)
	tool(a, s, &mcp.Tool{Name: "list_unapproved_review_records", Annotations: readOnly("Preview an import"),
		Description: "Check an answer before the user imports it: list the records a stkr-review (or brief/close) block would store, whether each is new, " +
			"already stored or repeated, and the warnings. Writes nothing; the user approves by importing the text (stkr import, Import Review key)."},
		a.previewImport)
}

func (a *adapter) getReviewContext(ctx context.Context, in reviewIn) (service.ReviewContext, string, error) {
	rc, err := a.svc.ReviewContext(ctx, in.Date)
	if err != nil {
		return rc, "", err
	}
	return rc, fmt.Sprintf("%s (%s): counted %s, %s, %s missing", rc.Date, rc.Basis,
		domain.FormatDuration(rc.Tracking.Counted.D()), plural(len(rc.Blocks), "block"), plural(len(rc.Missing), "evidence type")), nil
}

func (a *adapter) reportActivity(ctx context.Context, in activityIn) (service.ActivityResult, string, error) {
	if len(in.Events) > service.MaxActivityBatch {
		return service.ActivityResult{}, "", invalid("at most %d events per call", service.MaxActivityBatch)
	}
	evs := make([]service.ActivityInput, 0, len(in.Events))
	for _, e := range in.Events {
		evs = append(evs, service.ActivityInput(e))
	}
	res, err := a.svc.AddActivity(ctx, evs)
	if err != nil {
		return res, "", err
	}
	return res, fmt.Sprintf("Activity: %d new, %d merged, %d unchanged, %d out of range", res.Inserted, res.Merged, res.Unchanged, res.OutOfRange), nil
}

func (a *adapter) getActivitySummary(ctx context.Context, in dayIn) (service.ActivitySummary, string, error) {
	sum, err := a.svc.ActivityDay(ctx, in.Date)
	if err != nil {
		return sum, "", err
	}
	return sum, fmt.Sprintf("%s: %s, active %s", sum.Date, plural(sum.Events, "event"), domain.FormatDuration(sum.Active.D())), nil
}

func (a *adapter) previewImport(ctx context.Context, in previewIn) (service.ImportPreview, string, error) {
	if err := checkText("text", in.Text, maxImportText); err != nil {
		return service.ImportPreview{}, "", err
	}
	p, err := a.svc.PreviewImport(ctx, in.Text)
	if err != nil {
		return p, "", err
	}
	n := 0
	for _, r := range p.Records {
		if r.Status == "new" {
			n++
		}
	}
	return p, fmt.Sprintf("%s, %d new; %d block(s) rejected", plural(len(p.Records), "record"), n, p.BlocksRejected), nil
}
