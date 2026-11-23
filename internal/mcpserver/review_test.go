package mcpserver

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestReviewTools(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	if _, err := f.svc.Switch(ctx, "acme", domain.SourceCLI, "deploy"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Switch(ctx, "break", domain.SourceCLI, sentinel); err != nil {
		t.Fatal(err)
	}

	out := f.ok(t, "get_review_context", map[string]any{"date": "2026-11-16"})
	if out["date"] != "2026-11-16" || out["basis"] != "requested" {
		t.Fatalf("review: %v", out)
	}
	_, _, _, raw := f.call(t, "get_review_context", map[string]any{"date": "2026-11-16"})
	if strings.Contains(raw, sentinel) {
		t.Fatalf("private data or paths leaked: %s", raw)
	}
	f.ok(t, "get_review_context", nil)
	f.fail(t, "get_review_context", map[string]any{"date": "2026-11-17"}, "[invalid]")
	f.fail(t, "get_review_context", map[string]any{"date": "monday"}, "[invalid]")

	ev := map[string]any{"ts": "2026-11-16T08:30:00Z", "app": "Code", "category": "development", "workspace": "acme", "duration_seconds": 600, "lock_state": "active"}
	out = f.ok(t, "report_activity_event", map[string]any{"events": []any{ev}})
	if out["inserted"] != 1.0 {
		t.Fatalf("activity: %v", out)
	}
	if out = f.ok(t, "report_activity_event", map[string]any{"events": []any{ev}}); out["unchanged"] != 1.0 {
		t.Fatalf("replay: %v", out)
	}
	bad := map[string]any{"ts": "2026-11-16T08:30:00Z", "app": `C:\x\code.exe`, "category": "development", "duration_seconds": 1, "lock_state": "active"}
	f.fail(t, "report_activity_event", map[string]any{"events": []any{bad}}, "[invalid]")
	// Fields that do not exist (window titles) break the input schema.
	withTitle := map[string]any{"ts": "2026-11-16T08:30:00Z", "app": "Code", "category": "development", "duration_seconds": 1, "lock_state": "active", "window_title": "x"}
	if _, _, isErr, _ := f.call(t, "report_activity_event", map[string]any{"events": []any{withTitle}}); !isErr {
		t.Fatal("window_title accepted")
	}

	out = f.ok(t, "get_activity_summary", map[string]any{"date": "2026-11-16"})
	if out["events"] != 1.0 || out["active_seconds"] != 600.0 {
		t.Fatalf("summary: %v", out)
	}
	ws := out["workspaces"].([]any)
	if len(ws) != 1 || ws[0].(map[string]any)["context_id"] != "acme" {
		t.Fatalf("workspace mapping: %v", ws)
	}

	text := "```stkr-review v1\n2026-11-16|action|acme|Write the release notes\n2026-11-16|action|acme|Write the release notes\n```"
	out = f.ok(t, "list_unapproved_review_records", map[string]any{"text": text})
	recs := out["records"].([]any)
	if len(recs) != 2 || recs[0].(map[string]any)["status"] != "new" || recs[1].(map[string]any)["status"] != "duplicate" {
		t.Fatalf("preview: %v", out)
	}
	if items, _ := f.svc.ItemsOn(ctx, "2026-11-16"); len(items) != 0 {
		t.Fatal("preview wrote items")
	}
	f.fail(t, "list_unapproved_review_records", map[string]any{"text": strings.Repeat("x", maxImportText+1)}, "[invalid]")
	if strings.Contains(f.logs.String(), "release notes") || strings.Contains(f.logs.String(), "Code") {
		t.Fatal("tool content logged")
	}
}

// TestServerIsLocalOnly guards the promise that `stkr mcp` only speaks over
// stdio: the package must not import networking packages.
func TestServerIsLocalOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == "net" || strings.HasPrefix(p, "net/") {
				t.Errorf("%s imports %s: the MCP server must stay stdio-only", name, p)
			}
		}
		if strings.Contains(string(src), "StreamableHTTP") || strings.Contains(string(src), "SSEHandler") {
			t.Errorf("%s uses a network MCP transport", name)
		}
	}
}
