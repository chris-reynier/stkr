package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

var update = flag.Bool("update", false, "rewrite golden files and the contract doc")

const sentinel = "SECRET-SENTINEL-9f3a"

type fix struct {
	svc  *service.Service
	cs   *mcp.ClientSession
	logs *bytes.Buffer
}

func newFix(t *testing.T) *fix {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	cfg := config.Default()
	cfg.Timezone = "UTC"
	// Values that must never reach an agent.
	cfg.OneDriveDir = filepath.Join(dir, sentinel)
	cfg.PhoneInbox = filepath.Join(dir, sentinel+"-inbox")
	cfg.Launch.Command = sentinel
	cfg.Whisper.Model = sentinel
	cfg.Ollama.URL = "http://127.0.0.1:1/" + sentinel
	logs := &bytes.Buffer{}
	lg := slog.New(slog.NewTextHandler(logs, nil))
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), Logger: lg, PromptsDir: filepath.Join(dir, "prompts")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	if _, err := svc.AddContext(ctx, domain.Context{ID: "acme", Label: "Acme", Kind: domain.KindProject, Workdir: "/work/" + sentinel, NotebookURL: "https://x.example/" + sentinel}); err != nil {
		t.Fatal(err)
	}
	srv := New(svc, Options{Version: "test", Logger: lg})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := cl.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close(); _ = ss.Close() })
	return &fix{svc: svc, cs: cs, logs: logs}
}

// call runs a tool and returns the decoded structured output, the summary
// text, the error flag and the raw JSON of the whole result.
func (f *fix) call(t *testing.T, name string, args map[string]any) (map[string]any, string, bool, string) {
	t.Helper()
	res, err := f.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		// Input schema violations are protocol errors.
		return nil, err.Error(), true, err.Error()
	}
	raw, _ := json.Marshal(res)
	var sum string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sum += tc.Text
		}
	}
	out, _ := res.StructuredContent.(map[string]any)
	return out, sum, res.IsError, string(raw)
}

func (f *fix) ok(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	out, sum, isErr, _ := f.call(t, name, args)
	if isErr {
		t.Fatalf("%s failed: %s", name, sum)
	}
	if sum == "" {
		t.Fatalf("%s: empty summary", name)
	}
	return out
}

func (f *fix) fail(t *testing.T, name string, args map[string]any, want string) {
	t.Helper()
	_, sum, isErr, _ := f.call(t, name, args)
	if !isErr || !strings.Contains(sum, want) {
		t.Fatalf("%s: want error containing %q, got isErr=%v %q", name, want, isErr, sum)
	}
}

func TestToolsHappyPathAndErrors(t *testing.T) {
	f := newFix(t)

	out := f.ok(t, "list_contexts", nil)
	if out["count"].(float64) < 5 {
		t.Fatalf("contexts: %v", out)
	}
	for _, c := range out["contexts"].([]any) {
		m := c.(map[string]any)
		if _, has := m["workdir"]; has {
			t.Fatalf("workdir leaked: %v", m)
		}
	}
	f.fail(t, "switch_context", map[string]any{"context": "nope"}, "[not_found]")
	f.fail(t, "switch_context", map[string]any{"context": " "}, "[invalid]")

	out = f.ok(t, "switch_context", map[string]any{"context": "acme", "note": "hello"})
	if out["created"] != true || out["event"].(map[string]any)["source"] != "mcp" {
		t.Fatalf("switch: %v", out)
	}
	out = f.ok(t, "get_now", nil)
	if out["tracking"] != true || out["context"].(map[string]any)["id"] != "acme" {
		t.Fatalf("now: %v", out)
	}
	if _, has := out["context"].(map[string]any)["workdir"]; has {
		t.Fatal("workdir in now")
	}

	out = f.ok(t, "get_day", nil)
	if out["date"] != "2026-11-16" {
		t.Fatalf("day: %v", out["date"])
	}
	f.fail(t, "get_day", map[string]any{"date": "garbage"}, "[invalid]")
	out = f.ok(t, "get_week", map[string]any{"week": "2026-W47"})
	if out["week"] != "2026-W47" {
		t.Fatalf("week: %v", out["week"])
	}
	f.fail(t, "get_week", map[string]any{"week": "x"}, "[invalid]")

	out = f.ok(t, "add_idea", map[string]any{"text": "try htmx"})
	id := out["idea"].(map[string]any)["id"].(float64)
	f.fail(t, "add_idea", map[string]any{"text": ""}, "[invalid]")
	f.fail(t, "add_idea", map[string]any{"text": strings.Repeat("x", MaxTextBytes+1)}, "10240")
	f.fail(t, "add_idea", map[string]any{"text": "x", "tag": "weird"}, "[invalid]")
	out = f.ok(t, "add_idea", map[string]any{"text": "ship the release", "type": "action"})
	if out["item"] == nil || out["created"] != true {
		t.Fatalf("action item: %v", out)
	}

	out = f.ok(t, "list_ideas", map[string]any{"status": "new"})
	if out["count"].(float64) != 1 {
		t.Fatalf("ideas: %v", out)
	}
	f.fail(t, "list_ideas", map[string]any{"limit": 500}, "limit")
	f.fail(t, "list_ideas", map[string]any{"status": "bogus"}, "[invalid]")

	out = f.ok(t, "triage_idea", map[string]any{"id": id, "status": "triaged", "project": "acme", "title": "HTMX"})
	if out["status"] != "triaged" || out["suggested_project"] != "acme" {
		t.Fatalf("triage: %v", out)
	}
	f.fail(t, "triage_idea", map[string]any{"id": 9999, "status": "done"}, "[not_found]")
	f.fail(t, "triage_idea", map[string]any{"id": id, "status": "nonsense"}, "[invalid]")

	out = f.ok(t, "list_items", map[string]any{"status": "open", "since": "today"})
	if out["count"].(float64) != 1 {
		t.Fatalf("items: %v", out)
	}
	f.fail(t, "list_items", map[string]any{"status": "weird"}, "[invalid]")

	for _, k := range []string{"start", "close"} {
		out = f.ok(t, "get_digest", map[string]any{"kind": k})
		if !strings.Contains(out["text"].(string), "```stkr-") {
			t.Fatalf("digest %s lacks fence", k)
		}
	}
	f.fail(t, "get_digest", map[string]any{"kind": "x"}, "[invalid]")
	// get_digest must not switch to planning.
	if n := f.ok(t, "get_now", nil); n["context"].(map[string]any)["id"] != "acme" {
		t.Fatalf("digest changed context: %v", n)
	}

	out = f.ok(t, "report_agent_event", map[string]any{"instance": "a1", "state": "working", "workspace": "/work/" + sentinel, "summary": "doing it"})
	if out["state"] != "working" || out["context_id"] != "acme" || out["mapped"] != true {
		t.Fatalf("agent: %v", out)
	}
	f.fail(t, "report_agent_event", map[string]any{"instance": "a1", "state": "bogus"}, "[invalid]")
	out = f.ok(t, "report_agent_event", map[string]any{"instance": "a1", "event": "closed"})
	if out["closed"] != true {
		t.Fatalf("closed: %v", out)
	}

	out = f.ok(t, "stop", nil)
	if out["created"] != true {
		t.Fatalf("stop: %v", out)
	}
	f.ok(t, "stop", nil) // idempotent
}

func TestPersonalContentIsHidden(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	out := f.ok(t, "add_idea", map[string]any{"text": "private thought", "tag": "personal"})
	if out["idea"].(map[string]any)["text"] != nil {
		t.Fatalf("personal text echoed: %v", out)
	}
	pid := out["idea"].(map[string]any)["id"].(float64)
	if _, err := f.svc.Switch(ctx, "break", domain.SourceCLI, "lunch with X"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddIdea(ctx, service.IdeaInput{Text: "break idea"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Switch(ctx, "acme", domain.SourceCLI, ""); err != nil {
		t.Fatal(err)
	}
	f.ok(t, "add_idea", map[string]any{"text": "public idea"})
	if _, _, _, raw := f.call(t, "list_ideas", nil); strings.Contains(raw, "private thought") || strings.Contains(raw, "break idea") || !strings.Contains(raw, "public idea") {
		t.Fatalf("list_ideas: %s", raw)
	}
	if out := f.ok(t, "list_ideas", map[string]any{"tag": "personal"}); out["count"].(float64) != 0 {
		t.Fatalf("personal listed: %v", out)
	}
	f.fail(t, "triage_idea", map[string]any{"id": pid, "status": "done"}, "[not_found]")
	if _, _, _, raw := f.call(t, "get_day", nil); strings.Contains(raw, "lunch with X") {
		t.Fatalf("personal note leaked: %s", raw)
	}
	if _, _, _, raw := f.call(t, "get_week", nil); strings.Contains(raw, "lunch with X") {
		t.Fatalf("personal note leaked in week: %s", raw)
	}
}

func TestNoSecretsInAnyOutput(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	var all strings.Builder
	for _, c := range []struct {
		n string
		a map[string]any
	}{
		{"list_contexts", map[string]any{"include_inactive": true}},
		{"switch_context", map[string]any{"context": "acme"}},
		{"get_now", nil}, {"get_day", nil}, {"get_week", nil}, {"list_ideas", nil}, {"list_items", nil},
		{"get_digest", map[string]any{"kind": "start"}}, {"get_digest", map[string]any{"kind": "close"}},
		{"report_agent_event", map[string]any{"instance": "z", "state": "idle", "workspace": "/work/" + sentinel}},
		{"stop", nil},
		{"switch_context", map[string]any{"context": "nope"}},
	} {
		_, _, _, raw := f.call(t, c.n, c.a)
		all.WriteString(raw)
	}
	man, err := f.cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: ManifestURI})
	if err != nil {
		t.Fatal(err)
	}
	all.WriteString(man.Contents[0].Text)
	for _, p := range []string{"start_day", "close_day"} {
		r, err := f.cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: p})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(r)
		all.Write(b)
	}
	tl, _ := f.cs.ListTools(ctx, nil)
	b, _ := json.Marshal(tl)
	all.Write(b)
	s := all.String()
	if i := strings.Index(s, sentinel); i >= 0 {
		t.Fatalf("secret sentinel leaked near: %q", s[max(0, i-80):min(len(s), i+80)])
	}
}

func TestManifestAndPrompts(t *testing.T) {
	f := newFix(t)
	ctx := context.Background()
	rl, err := f.cs.ListResources(ctx, nil)
	if err != nil || len(rl.Resources) != 1 || rl.Resources[0].URI != ManifestURI {
		t.Fatalf("resources: %v %v", rl, err)
	}
	man, err := f.cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: ManifestURI})
	if err != nil {
		t.Fatal(err)
	}
	m := man.Contents[0].Text
	for _, want := range []string{"# STKR manifest", "```stkr-brief v1", "```stkr-close v1", "Privacy", "report_agent_event", "action_required", "switch_context", "list_contexts"} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest lacks %q", want)
		}
	}
	pl, err := f.cs.ListPrompts(ctx, nil)
	if err != nil || len(pl.Prompts) != 2 {
		t.Fatalf("prompts: %v %v", pl, err)
	}
	r, err := f.cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "close_day"})
	if err != nil || len(r.Messages) != 1 || !strings.Contains(r.Messages[0].Content.(*mcp.TextContent).Text, "stkr-close v1") {
		t.Fatalf("close_day: %v %v", r, err)
	}
}

func TestToolListGoldenAndContract(t *testing.T) {
	f := newFix(t)
	tl, err := f.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Tools) != 12 {
		t.Fatalf("want 12 tools, got %d", len(tl.Tools))
	}
	golden, _ := json.MarshalIndent(tl.Tools, "", "  ")
	golden = append(golden, '\n')
	contract := RenderContract(tl.Tools)
	gp := filepath.Join("testdata", "tools.golden.json")
	cp := filepath.Join("..", "..", "docs", "contracts", "mcp-tools.md")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(gp, golden, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cp, []byte(contract), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(gp); !bytes.Equal(normalize(got), normalize(golden)) {
		t.Error("tool list differs from testdata/tools.golden.json; run: go test ./internal/mcpserver -update")
	}
	if got, _ := os.ReadFile(cp); !bytes.Equal(normalize(got), normalize([]byte(contract))) {
		t.Error("docs/contracts/mcp-tools.md is stale; run: go test ./internal/mcpserver -update")
	}
}

func normalize(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

func TestErrorsNeverLeakInternals(t *testing.T) {
	f := newFix(t)
	a := &adapter{svc: f.svc, log: slog.New(slog.NewTextHandler(f.logs, nil))}
	err := a.toolError("x", os.ErrPermission)
	if err.Error() != "[internal] internal error; see the stkr log" {
		t.Fatalf("got %q", err)
	}
	if got := a.toolError("x", context.Canceled).Error(); !strings.Contains(got, "canceled") {
		t.Fatalf("got %q", got)
	}
}

func TestChecks(t *testing.T) {
	if _, err := checkLimit(-1); err == nil {
		t.Error("negative limit")
	}
	if n, _ := checkLimit(0); n != DefaultLimit {
		t.Error("default limit")
	}
	if err := checkText("t", "\xff", 10); err == nil {
		t.Error("invalid utf8")
	}
	if plural(1, "idea") != "1 idea" || plural(2, "idea") != "2 ideas" {
		t.Error("plural")
	}
}
