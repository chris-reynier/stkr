package mcpserver

import (
	"context"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
)

var fenceRE = regexp.MustCompile("(?s)```stkr-[a-z]+ v[0-9]+\n.*?```")

// Manifest returns the markdown served as stkr://manifest. The tool list and
// the fence formats come from the code (tool registry and the prompt
// templates), so they cannot drift from what the server does.
func (a *adapter) manifest() string {
	var b strings.Builder
	b.WriteString("# STKR manifest\n\n")
	b.WriteString("STKR is a local task and time tracker. This server gives you the same operations as the `stkr` command line and the local web UI, on the same data.\n\n")
	b.WriteString("## Concepts\n\n")
	b.WriteString("- **Context**: what you work on, with an id and a kind: `project`, `mode` (planning, meeting), `personal` or `system` (`off`). Time tracking is one running context at a time; `switch_context` ends the previous block and starts a new one.\n")
	b.WriteString("- **Idea**: a captured thought, tagged `pro` or `personal`, triaged with status `new`, `triaged`, `done` or `archived`.\n")
	b.WriteString("- **Item**: a follow-up record: `action`, `decision`, `note`, `followup`, `risk`, `achievement` or `gap`, filed under a project, `open` or `done`.\n")
	b.WriteString("- **Agent instance**: a coding agent window. Report its state with `report_agent_event`: `")
	states := make([]string, 0, len(domain.AgentStates))
	for _, s := range domain.AgentStates {
		states = append(states, string(s))
	}
	b.WriteString(strings.Join(states, "`, `"))
	b.WriteString("`.\n")
	b.WriteString("- **Day**: Start Day and Close Day build a prompt from STKR data. `get_digest` returns that prompt text without side effects; send your answer back in the fence format below.\n")
	b.WriteString("- **Review**: `get_review_context` returns the facts of the previous working day (or a given date) and the evidence STKR does not have. A review answer ends with one `stkr-review v1` block; check it with `list_unapproved_review_records`, then let the user import it.\n")
	b.WriteString("- **Activity**: local evidence of which kind of application was in front and when the session was locked, idle or on a break. `report_activity_event` stores it, `get_activity_summary` reads it.\n\n")
	b.WriteString("## Tools\n\n")
	for _, t := range a.tools {
		b.WriteString("- `" + t.Name + "`: " + t.Description + "\n")
	}
	b.WriteString("\nInput limits: text up to 10 KB, `limit` up to 200. Errors come back as tool errors that start with a code in brackets: `[not_found]`, `[invalid]`, `[conflict]` or `[unavailable]`.\n\n")
	b.WriteString("## Fence formats for brief, close and review replies\n\n")
	b.WriteString("End a Start Day, Close Day or review answer with exactly one of these fenced blocks. The user imports it with `stkr import`; do not call a tool for it.\n\n")
	for _, name := range []string{daily.StartDayTemplate, daily.CloseDayTemplate, daily.ReviewDayTemplate} {
		out, err := daily.Render("", name, map[string]string{"Date": "", "Weekday": "", "Digest": "", "Context": ""})
		if err != nil {
			continue
		}
		if f := fenceRE.FindString(out); f != "" {
			b.WriteString(f + "\n\n")
		}
	}
	b.WriteString("Rules: one record per line, fields separated by `|`. In a `stkr-review` block a malformed line (bad date, missing field) rejects the whole block.\n\n")
	b.WriteString("## Local only\n\nThis server runs as a child process of your client and speaks over standard input and output. It opens no network port.\n\n")
	b.WriteString("## Privacy\n\n")
	b.WriteString("- Personal ideas, and ideas captured while a personal or system context was running, are never returned. Items of personal and system projects are never returned. Notes of personal and system time blocks are removed.\n")
	b.WriteString("- No tool returns configuration, file paths, workspace paths of other tools, or secrets.\n")
	b.WriteString("- Do not copy personal or time details into other tools or prompts unless the user asks.\n")
	return b.String()
}

func (a *adapter) addResources(s *mcp.Server) {
	s.AddResource(&mcp.Resource{
		URI: ManifestURI, Name: "manifest", Title: "STKR manifest", MIMEType: "text/markdown",
		Description: "STKR concepts, tools, import fence formats and privacy rules.",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: ManifestURI, MIMEType: "text/markdown", Text: a.manifest()}}}, nil
	})
	for _, p := range []struct{ name, kind, desc string }{
		{"start_day", "start", "Start Day prompt built from today's STKR data."},
		{"close_day", "close", "Close Day prompt built from today's STKR data."},
	} {
		s.AddPrompt(&mcp.Prompt{Name: p.name, Description: p.desc}, func(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			txt, err := a.svc.Digest(ctx, p.kind)
			if err != nil {
				return nil, a.toolError(p.name, err)
			}
			return &mcp.GetPromptResult{Description: p.desc, Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: txt}}}}, nil
		})
	}
}
