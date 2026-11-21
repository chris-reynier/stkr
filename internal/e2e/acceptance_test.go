package e2e

// Acceptance suite: each test drives the compiled binary the way a user or an
// integration would and proves one or more bullets of docs/dev/acceptance.md.
// Nothing here needs a network, a real external tool or a desktop.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/testutil"
)

// ---- helpers ----

// deck sends a request the way a Stream Deck HTTP key does.
func deck(t *testing.T, method, url string) (int, []byte) {
	t.Helper()
	return call(t, method, url, "X-STKR-Source", "deck")
}

func mustRun(t *testing.T, home string, args ...string) string {
	t.Helper()
	out, errOut, code := run(t, home, args...)
	if code != 0 {
		t.Fatalf("stkr %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

type nowJSON struct {
	Tracking bool   `json:"tracking"`
	Since    string `json:"since"`
	Context  struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"context"`
}

func nowOf(t *testing.T, home string) nowJSON {
	t.Helper()
	var n nowJSON
	if err := json.Unmarshal([]byte(mustRun(t, home, "--json", "now")), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

type ideaJSON struct {
	ID        int64  `json:"id"`
	Tag       string `json:"tag"`
	RawText   string `json:"raw_text"`
	CleanText string `json:"clean_text"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	AIState   string `json:"ai_state"`
	AudioPath string `json:"audio_path"`
	Source    string `json:"source"`
}

func ideasOf(t *testing.T, home string) []ideaJSON {
	t.Helper()
	var l struct {
		Ideas []ideaJSON `json:"ideas"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, home, "--json", "ideas")), &l); err != nil {
		t.Fatal(err)
	}
	return l.Ideas
}

// logText returns everything the binary logged in the home directory.
func logText(t *testing.T, home string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(home, "logs", "*"))
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// mcpSession is a minimal MCP client over the binary's stdio.
type mcpSession struct {
	t     *testing.T
	stdin io.WriteCloser
	sc    *bufio.Scanner
	cmd   *exec.Cmd
	id    int
}

func startMCP(t *testing.T, home string, env ...string) *mcpSession {
	t.Helper()
	cmd := exec.Command(binPath, "mcp")
	cmd.Env = append(append(os.Environ(), "STKR_HOME="+home), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &mcpSession{t: t, stdin: stdin, cmd: cmd}
	s.sc = bufio.NewScanner(stdout)
	s.sc.Buffer(make([]byte, 1<<20), 8<<20)
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	s.rpc("initialize", `{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"acceptance","version":"0"}}`)
	if _, err := fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *mcpSession) rpc(method, params string) map[string]any {
	s.t.Helper()
	s.id++
	if _, err := fmt.Fprintf(s.stdin, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", s.id, method, params); err != nil {
		s.t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() {
		time.Sleep(20 * time.Second)
		select {
		case <-done:
		default:
			_ = s.cmd.Process.Kill()
		}
	}()
	defer func() { done <- true }()
	if !s.sc.Scan() {
		s.t.Fatalf("no answer to %s: %v", method, s.sc.Err())
	}
	var m map[string]any
	if err := json.Unmarshal(s.sc.Bytes(), &m); err != nil {
		s.t.Fatalf("not JSON: %q", s.sc.Text())
	}
	return m
}

// tool calls a tool and returns its structured result, the full result text
// and whether it was a tool error.
func (s *mcpSession) tool(name string, args map[string]any) (map[string]any, string, bool) {
	s.t.Helper()
	b, _ := json.Marshal(args)
	m := s.rpc("tools/call", fmt.Sprintf(`{"name":%q,"arguments":%s}`, name, b))
	res, ok := m["result"].(map[string]any)
	if !ok {
		s.t.Fatalf("%s: %v", name, m)
	}
	st, _ := res["structuredContent"].(map[string]any)
	raw, _ := json.Marshal(res)
	return st, string(raw), res["isError"] == true
}

// ---- Core: a full day through the Stream Deck HTTP calls only ----

func TestAcceptanceFullDayDeckOnly(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	mustRun(t, home, "ctx", "add", "acme", "--label", "ACME", "--ticket", "ACME-1")
	base, _ := startServe(t, home)

	// The deck wiring first reads the context list to build its keys.
	if code, b := deck(t, "GET", base+"/api/contexts"); code != 200 || !strings.Contains(string(b), `"acme"`) {
		t.Fatalf("contexts: %d %s", code, b)
	}
	if code, b := deck(t, "GET", base+"/img/now.png"); code != 200 || len(b) < 100 {
		t.Fatalf("now.png: %d", code)
	}

	steps := []string{"planning", "acme", "acme", "meeting"} // the second acme is a double click
	for _, c := range steps {
		if code, b := deck(t, "POST", base+"/api/switch/"+c); code != 200 {
			t.Fatalf("switch %s: %d %s", c, code, b)
		}
	}
	// Meeting capture: an action and a decision taken in the meeting.
	for _, body := range []string{`{"text":"send the budget to finance","type":"action"}`, `{"text":"ship on friday","type":"decision"}`} {
		req, _ := http.NewRequest("POST", base+"/api/idea", strings.NewReader(body))
		req.Header.Set("X-STKR-Source", "deck")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("meeting capture: %d %s", resp.StatusCode, b)
		}
	}
	if code, b := deck(t, "POST", base+"/api/switch/acme"); code != 200 {
		t.Fatalf("back to acme: %d %s", code, b)
	}

	// stkr now and the Today page agree on the running context and start time.
	n := nowOf(t, home)
	if !n.Tracking || n.Context.ID != "acme" {
		t.Fatalf("now: %+v", n)
	}
	_, page := call(t, "GET", base+"/ui/today")
	section := regexp.MustCompile(`(?s)<section id="now".*?</section>`).Find(page)
	since, err := time.Parse(time.RFC3339Nano, n.Since)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("<strong>ACME</strong> since %s", since.Format("15:04"))
	if !strings.Contains(string(section), want) {
		t.Fatalf("Today page does not show %q:\n%s", want, section)
	}
	code, b := deck(t, "GET", base+"/api/now")
	var api nowJSON
	if code != 200 || json.Unmarshal(b, &api) != nil || api.Context.ID != n.Context.ID || api.Since != n.Since {
		t.Fatalf("/api/now: %d %s", code, b)
	}

	// Lunch, then stop for the day.
	for _, step := range []struct{ method, path string }{{"POST", "/api/switch/break"}, {"POST", "/api/stop"}} {
		if code, b := deck(t, step.method, base+step.path); code != 200 {
			t.Fatalf("%s: %d %s", step.path, code, b)
		}
	}
	if n = nowOf(t, home); n.Tracking {
		t.Fatalf("still tracking after stop: %+v", n)
	}

	// The day holds exactly the expected events, all from the deck.
	var day struct {
		Events []struct {
			ContextID string `json:"context_id"`
			Source    string `json:"source"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, home, "--json", "day")), &day); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range day.Events {
		if e.Source != "deck" {
			t.Errorf("event %s has source %q, want deck", e.ContextID, e.Source)
		}
		got = append(got, e.ContextID)
	}
	if want := "planning acme meeting acme break off"; strings.Join(got, " ") != want {
		t.Fatalf("events = %v, want %s (the double click must not add an event)", got, want)
	}

	// The meeting items belong to the project that was running before the meeting.
	_, page = call(t, "GET", base+"/ui/today")
	for _, s := range []string{"send the budget to finance", "ship on friday"} {
		if !strings.Contains(string(page), s) {
			t.Errorf("Today page lacks meeting item %q", s)
		}
	}
	// Time of personal contexts (break) never reaches the Tempo export.
	if csv := mustRun(t, home, "week", "--csv"); strings.Contains(csv, "break") {
		t.Errorf("personal time in tempo csv:\n%s", csv)
	}
}

// ---- Core: backups, migrations and the legacy archive ----

func TestAcceptanceBackupRestoreAndLegacy(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	mustRun(t, home, "switch", "planning")
	out := strings.TrimSpace(mustRun(t, home, "backup"))
	i := strings.Index(out, filepath.Join(home, "backups"))
	if i < 0 {
		t.Fatalf("backup output: %q", out)
	}
	bk := strings.TrimSpace(out[i:])
	if _, err := os.Stat(bk); err != nil {
		t.Fatalf("backup file: %v", err)
	}
	if tmp, _ := filepath.Glob(filepath.Join(home, "backups", "*.tmp")); len(tmp) != 0 {
		t.Errorf("temp files left in backups: %v", tmp)
	}

	// Restoring is a file copy into a fresh home.
	home2 := filepath.Join(t.TempDir(), "home2")
	mustRun(t, home2, "config", "show") // creates the folder
	data, err := os.ReadFile(bk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home2, "stkr.db"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if n := nowOf(t, home2); !n.Tracking || n.Context.ID != "planning" {
		t.Fatalf("restored database: %+v", n)
	}

	// The legacy archive copies files and never touches the database.
	before, _ := os.ReadFile(filepath.Join(home, "stkr.db"))
	legacy := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "tasklog.json"), []byte(`{"not":"parsed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, home, "import-legacy", legacy)
	after, _ := os.ReadFile(filepath.Join(home, "stkr.db"))
	if string(before) != string(after) {
		t.Error("import-legacy changed the database file")
	}
	arch, _ := filepath.Glob(filepath.Join(home, "backups", "legacy-*", "tasklog.json"))
	if len(arch) != 1 {
		t.Errorf("legacy file not archived: %v", arch)
	}
	if n := nowOf(t, home); !n.Tracking || n.Context.ID != "planning" {
		t.Errorf("state after legacy import: %+v", n)
	}
}

// ---- Daily loop ----

const dupBlock = "Done.\n```stkr-close v1\ntop | 1 | Finish the report\nitem | action | acme | Email the client | 2026-12-01\nitem | decision | acme | Use the new vendor |\n```\n"

type importResult struct {
	Created  int `json:"items_created"`
	Existing int `json:"items_existing"`
}

func TestAcceptanceDuplicateImport(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	mustRun(t, home, "ctx", "add", "acme", "--label", "ACME")
	base, _ := startServe(t, home)

	// First import over the CLI.
	out, _, code := runIn(t, home, dupBlock, "--json", "import", "-")
	var r importResult
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.Created != 2 {
		t.Fatalf("first import: %d %s", code, out)
	}
	// The same answer again over HTTP and over the CLI: nothing new.
	code, b := postText(t, base+"/api/import", dupBlock)
	if code != 200 || json.Unmarshal(b, &r) != nil || r.Created != 0 || r.Existing != 2 {
		t.Fatalf("HTTP re-import: %d %s", code, b)
	}
	out, _, code = runIn(t, home, dupBlock, "--json", "import", "-")
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.Created != 0 || r.Existing != 2 {
		t.Fatalf("CLI re-import: %d %s", code, out)
	}
	// Whitespace and case differences are the same item.
	loose := strings.ReplaceAll(dupBlock, "Email the client", "  email  the CLIENT ")
	if out, _, code = runIn(t, home, loose, "--json", "import", "-"); code != 0 || json.Unmarshal([]byte(out), &r) != nil || r.Created != 0 {
		t.Fatalf("normalised re-import: %d %s", code, out)
	}
	_, page := call(t, "GET", base+"/ui/today")
	if n := strings.Count(string(page), "Email the client"); n != 1 {
		t.Errorf("item shown %d times on Today, want 1", n)
	}
}

func TestAcceptanceNotesPreserved(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	od := filepath.Join(t.TempDir(), "onedrive")
	mustRun(t, home, "ctx", "add", "acme", "--label", "ACME")
	mustRun(t, home, "config", "set", "onedrive_dir", od)

	if _, _, code := runIn(t, home, dupBlock, "import", "-"); code != 0 {
		t.Fatal("first import failed")
	}
	logPath := filepath.Join(od, "acme", "log.md")
	first, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(first), "Email the client") {
		t.Fatalf("project log not written: %v\n%s", err, first)
	}

	// The user edits their own parts of the file.
	notes := "## Notes\n\nThe client prefers phone calls.\n- keep this line\n\n"
	edited := string(first)
	i := strings.Index(edited, "## Notes")
	if i < 0 {
		t.Fatalf("no Notes section in\n%s", first)
	}
	j := strings.Index(edited[i+3:], "\n## ")
	end := len(edited)
	if j >= 0 {
		end = i + 3 + j + 1
	}
	edited = edited[:i] + notes + edited[end:]
	edited += "\n## My own section\nstays too\n"
	if err := os.WriteFile(logPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	userParts := []string{"## Notes\n\nThe client prefers phone calls.\n- keep this line\n", "## My own section\nstays too\n"}

	for i, blk := range []string{
		"```stkr-close v1\nitem | note | acme | second import |\n```",
		"```stkr-brief v1\nitem | risk | acme | third import |\n```",
	} {
		if _, _, code := runIn(t, home, blk, "import", "-"); code != 0 {
			t.Fatalf("import %d failed", i+2)
		}
	}
	got, _ := os.ReadFile(logPath)
	for _, p := range userParts {
		if !strings.Contains(string(got), p) {
			t.Errorf("user text %q lost:\n%s", p, got)
		}
	}
	if !strings.Contains(string(got), "third import") || !strings.Contains(string(got), "Email the client") {
		t.Errorf("generated sections not updated:\n%s", got)
	}
}

// ---- Capture: missing tools are reported, never fatal ----

func TestAcceptanceMissingWhisperAndFFmpeg(t *testing.T) {
	fake := testutil.BuildFakebin(t) // a "ffmpeg" that succeeds and writes nothing
	home := filepath.Join(t.TempDir(), "home")
	inbox := filepath.Join(t.TempDir(), "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	memo := filepath.Join(inbox, "voice-memo.wav")
	if err := os.WriteFile(memo, []byte("RIFF-not-really-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(memo, old, old)
	for k, v := range map[string]string{
		"phone_inbox": inbox,
		"ffmpeg":      fake,
		"whisper.bin": filepath.Join(t.TempDir(), "no-such-whisper"),
	} {
		mustRun(t, home, "config", "set", k, v)
	}
	base, _ := startServe(t, home)

	// The audio idea exists before (and without) any transcription.
	deadline := time.Now().Add(15 * time.Second)
	var ideas []ideaJSON
	for {
		ideas = ideasOf(t, home)
		if len(ideas) == 1 && ideas[0].AIState == "skipped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("audio idea not inserted/skipped: %+v", ideas)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if ideas[0].AudioPath == "" || ideas[0].Source != "phone" || ideas[0].RawText != "" {
		t.Errorf("idea = %+v", ideas[0])
	}
	code, b := call(t, "GET", base+"/api/capture/health")
	if code != 200 || !strings.Contains(string(b), `"whisper":"missing"`) {
		t.Errorf("health should report whisper missing: %d %s", code, b)
	}

	// Voice capture with ffmpeg gone: a clear 503, and the server keeps working.
	mustRun(t, home, "config", "set", "ffmpeg", filepath.Join(t.TempDir(), "no-such-ffmpeg"))
	base2, _ := startServe(t, home)
	if code, b := deck(t, "POST", base2+"/api/idea/record/toggle"); code != 503 || !strings.Contains(string(b), "ffmpeg") {
		t.Errorf("toggle without ffmpeg: %d %s", code, b)
	}
	if code, _ := deck(t, "GET", base2+"/healthz"); code != 200 {
		t.Errorf("healthz after failed recording: %d", code)
	}
	if code, b := deck(t, "POST", base2+"/api/switch/planning"); code != 200 {
		t.Errorf("switch after failed recording: %d %s", code, b)
	}
	if code, _ := postJSON(t, base2+"/api/idea", `{"text":"text ideas still work"}`); code != 201 {
		t.Errorf("text idea without ffmpeg: %d", code)
	}
}

func TestAcceptanceBookmarkValidationAndTitleFailure(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	for _, bad := range []string{"", "not a url", "file:///etc/passwd", "http://user:pw@example.com/"} {
		if _, _, code := run(t, home, "bookmark", bad); code != 2 {
			t.Errorf("bookmark %q: exit %d, want 2", bad, code)
		}
	}
	// A title lookup that fails (nothing listens there) keeps the bookmark.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	u := "http://" + addr + "/page"
	if out := mustRun(t, home, "bookmark", u); !strings.Contains(out, u) {
		t.Errorf("bookmark output: %q", out)
	}
	if out := mustRun(t, home, "links"); !strings.Contains(out, u) {
		t.Errorf("links: %q", out)
	}
}

// ---- MCP ----

func TestAcceptanceMCPSwitchCaptureTriageReport(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	mustRun(t, home, "ctx", "add", "acme", "--label", "ACME")
	s := startMCP(t, home)

	// Read: contexts, now, week.
	st, _, isErr := s.tool("list_contexts", nil)
	if isErr || !strings.Contains(fmt.Sprint(st), "acme") {
		t.Fatalf("list_contexts: %v", st)
	}
	if st, raw, isErr := s.tool("get_week", nil); isErr || st == nil {
		t.Fatalf("get_week: %s", raw)
	}

	// Switch.
	if st, raw, isErr := s.tool("switch_context", map[string]any{"context": "acme", "note": "via agent"}); isErr || st["created"] != true {
		t.Fatalf("switch_context: %s", raw)
	}
	if n := nowOf(t, home); n.Context.ID != "acme" {
		t.Fatalf("CLI does not see the MCP switch: %+v", n)
	}
	var day struct {
		Events []struct{ Source string } `json:"events"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, home, "--json", "day")), &day); err != nil || len(day.Events) != 1 || day.Events[0].Source != "mcp" {
		t.Fatalf("event source: %v %+v", err, day)
	}

	// Capture and triage an idea.
	st, raw, isErr := s.tool("add_idea", map[string]any{"text": "agent found a flaky test"})
	if isErr {
		t.Fatalf("add_idea: %s", raw)
	}
	id := int64(st["idea"].(map[string]any)["id"].(float64))
	if st, raw, isErr = s.tool("triage_idea", map[string]any{"id": id, "status": "triaged", "project": "acme", "title": "Flaky test"}); isErr || st["status"] != "triaged" || st["suggested_project"] != "acme" {
		t.Fatalf("triage_idea: %s", raw)
	}
	if st, _, _ = s.tool("list_ideas", nil); !strings.Contains(fmt.Sprint(st), "Flaky test") {
		t.Fatalf("list_ideas: %v", st)
	}
	ideas := ideasOf(t, home)
	if len(ideas) != 1 || ideas[0].Status != "triaged" || ideas[0].Source == "" {
		t.Fatalf("CLI view of idea: %+v", ideas)
	}
	// Personal ideas are captured but never shown back to the agent.
	if _, raw, isErr = s.tool("add_idea", map[string]any{"text": "PERSONAL-SECRET-GIFT", "tag": "personal"}); isErr || strings.Contains(raw, "PERSONAL-SECRET-GIFT") {
		t.Fatalf("personal add_idea echoed text: %s", raw)
	}
	if _, raw, _ = s.tool("list_ideas", nil); strings.Contains(raw, "PERSONAL-SECRET-GIFT") {
		t.Fatalf("personal idea listed: %s", raw)
	}

	// Report an agent event; the agents view of the CLI shows it.
	if st, raw, isErr = s.tool("report_agent_event", map[string]any{"instance": "mcp-1", "state": "action_required", "summary": "needs approval"}); isErr || st["state"] != "action_required" {
		t.Fatalf("report_agent_event: %s", raw)
	}
	if out := mustRun(t, home, "agents"); !strings.Contains(out, "mcp-1") || !strings.Contains(out, "action_required") {
		t.Fatalf("agents: %s", out)
	}
	// A tool error is reported as such, not as a crash.
	if _, raw, isErr = s.tool("switch_context", map[string]any{"context": "does-not-exist"}); !isErr || !strings.Contains(raw, "not_found") {
		t.Fatalf("unknown context: %s", raw)
	}
}

// ---- Runtime AI: provider switch without rebuild ----

func TestAcceptanceProviderSwitchWithoutRebuild(t *testing.T) {
	var ollamaCalls atomic.Int32
	mode := atomic.Value{}
	mode.Store("good")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = io.WriteString(w, `{"version":"0.0.1"}`)
			return
		}
		ollamaCalls.Add(1)
		content := "this is not json at all"
		if mode.Load() == "good" {
			inner, _ := json.Marshal(map[string]string{"title": "From ollama", "text": "Cleaned by ollama.", "suggested_project": ""})
			content = string(inner)
		}
		out, _ := json.Marshal(map[string]any{"message": map[string]string{"content": content}})
		_, _ = w.Write(out)
	}))
	defer srv.Close()
	fake := testutil.BuildFakebin(t)
	inner, _ := json.Marshal(map[string]string{"title": "From claude", "text": "Cleaned by claude.", "suggested_project": ""})
	envelope, _ := json.Marshal(map[string]any{"result": string(inner)})
	t.Setenv("FAKEBIN_STDOUT", string(envelope))

	home := filepath.Join(t.TempDir(), "home")
	set := func(k, v string) { mustRun(t, home, "config", "set", k, v) }
	byRaw := func(raw string) ideaJSON {
		for _, i := range ideasOf(t, home) {
			if i.RawText == raw {
				return i
			}
		}
		t.Fatalf("idea %q not found", raw)
		return ideaJSON{}
	}

	// 1. No provider: everything works, nothing is sent anywhere.
	mustRun(t, home, "idea", "first idea")
	if out := mustRun(t, home, "ai", "status"); !strings.Contains(out, "active:    no") {
		t.Fatalf("status: %s", out)
	}
	if byRaw("first idea").AIState != "pending" {
		t.Fatal("capture must leave cleanup pending")
	}

	// 2. Ollama (a fake server): cleanup runs.
	set("ai.provider", "ollama")
	set("ollama.enabled", "true")
	set("ollama.url", srv.URL)
	if out := mustRun(t, home, "ai", "run-once"); !strings.Contains(out, "done 1") {
		t.Fatalf("ollama run-once: %s", out)
	}
	if i := byRaw("first idea"); i.AIState != "done" || i.CleanText != "Cleaned by ollama." {
		t.Fatalf("after ollama: %+v", i)
	}

	// 3. Invalid output: the raw text is kept and the idea is marked failed.
	mode.Store("bad")
	mustRun(t, home, "idea", "second idea")
	mustRun(t, home, "ai", "run-once")
	if i := byRaw("second idea"); i.AIState != "failed" || i.RawText != "second idea" || i.CleanText != "" {
		t.Fatalf("after invalid output: %+v", i)
	}
	if n := ollamaCalls.Load(); n < 3 { // 1 good + first attempt + one retry
		t.Errorf("ollama calls = %d, want a bounded retry (>=3)", n)
	}
	before := ollamaCalls.Load()

	// 4. Claude Code through the exec adapter, same binary, no rebuild.
	set("ai.provider", "claude_code")
	set("claude_code.enabled", "true")
	set("claude_code.bin", fake)
	mustRun(t, home, "idea", "third idea")
	mustRun(t, home, "idea", "private errand", "--personal")
	if out := mustRun(t, home, "ai", "test"); !strings.Contains(out, "answered") {
		t.Fatalf("ai test: %s", out)
	}
	mustRun(t, home, "ai", "run-once")
	if i := byRaw("third idea"); i.AIState != "done" || i.Title != "From claude" {
		t.Fatalf("after claude_code: %+v", i)
	}
	// A non-local provider never sees personal content.
	if i := byRaw("private errand"); i.AIState != "skipped" || i.CleanText != "" {
		t.Fatalf("personal idea with a cloud provider: %+v", i)
	}
	if ollamaCalls.Load() != before {
		t.Error("ollama was called after switching to claude_code")
	}

	// 5. Back to none: still fully functional.
	set("ai.provider", "none")
	mustRun(t, home, "idea", "fourth idea")
	if out := mustRun(t, home, "ai", "status"); !strings.Contains(out, "active:    no") {
		t.Fatalf("status: %s", out)
	}
	if byRaw("fourth idea").AIState != "pending" {
		t.Fatal("capture without provider")
	}
}

// ---- Security ----

var routeRe = regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE|HEAD) (/[^"\s]*)"`)

// registeredRoutes reads every route pattern registered in the HTTP and web
// packages from the source (the mux does not expose its table).
func registeredRoutes(t *testing.T) [][2]string {
	t.Helper()
	var routes [][2]string
	seen := map[string]bool{}
	for _, dir := range []string{"../httpapi", "../web"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range routeRe.FindAllStringSubmatch(string(b), -1) {
				key := m[1] + " " + m[2]
				if !seen[key] {
					seen[key] = true
					routes = append(routes, [2]string{m[1], m[2]})
				}
			}
		}
	}
	return routes
}

func concretePath(p string) string {
	p = strings.ReplaceAll(p, "{$}", "")
	return regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(p, "1")
}

func TestAcceptanceEveryRouteIsGuarded(t *testing.T) {
	routes := registeredRoutes(t)
	if len(routes) < 50 {
		t.Fatalf("found only %d routes in the source; the scan is broken", len(routes))
	}
	home := filepath.Join(t.TempDir(), "home")
	base, _ := startServe(t, home)
	u, _ := url.Parse(base)

	for _, r := range routes {
		method, path := r[0], concretePath(r[1])
		// A foreign Host (DNS rebinding) is refused on every route.
		if code, b := call(t, method, base+path, "Host", "evil.example"); code != http.StatusMisdirectedRequest {
			t.Errorf("%s %s with foreign Host: %d %s", method, r[1], code, b)
		}
		if code, _ := call(t, method, base+path, "Host", "evil.example:"+u.Port()); code != http.StatusMisdirectedRequest {
			t.Errorf("%s %s with foreign Host and port: %d", method, r[1], code)
		}
		// A foreign Origin is refused on every state-changing route.
		if method != "GET" && method != "HEAD" {
			if code, b := call(t, method, base+path, "Origin", "http://evil.example"); code != http.StatusForbidden {
				t.Errorf("%s %s with foreign Origin: %d %s", method, r[1], code, b)
			}
		}
	}
	// A GET never changes data, so foreign Origins on GET are harmless; prove it.
	call(t, "GET", base+"/api/switch/planning", "Origin", "http://evil.example")
	if n := nowOf(t, home); n.Tracking {
		t.Error("a GET switched the context")
	}
}

func TestAcceptanceLoopbackOnly(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	base, _ := startServe(t, home)
	u, _ := url.Parse(base)
	if u.Hostname() != "127.0.0.1" {
		t.Fatalf("serve announces %s", base)
	}
	addrs, _ := net.InterfaceAddrs()
	tested := 0
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), u.Port()), 300*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Errorf("server is reachable on %s", ipn.IP)
		}
		tested++
	}
	t.Logf("checked %d non-loopback addresses", tested)
}

// Sentinels that must never appear in logs: user content, AI output, prompts,
// and a secret from the environment.
const (
	sNote     = "SENTINEL-NOTE-7f3a"
	sIdeaCLI  = "SENTINEL-IDEA-CLI-91bc"
	sIdeaHTTP = "SENTINEL-IDEA-HTTP-22de"
	sIdeaMCP  = "SENTINEL-IDEA-MCP-5a60"
	sImport   = "SENTINEL-IMPORT-c4d1"
	sAI       = "SENTINEL-AI-ANSWER-08ee"
	sToken    = "SENTINEL-GITHUB-TOKEN-b77f"
	sBookmark = "SENTINEL-BOOKMARK-3d9a"
	sPersonal = "SENTINEL-PERSONAL-6c15"
)

func TestAcceptanceNoContentOrSecretsInLogsOrOutputs(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = io.WriteString(w, `{"version":"0.0.1"}`)
			return
		}
		inner, _ := json.Marshal(map[string]string{"title": "T", "text": sAI, "suggested_project": ""})
		out, _ := json.Marshal(map[string]any{"message": map[string]string{"content": string(inner)}})
		_, _ = w.Write(out)
	}))
	defer ollama.Close()
	t.Setenv("GITHUB_TOKEN", sToken)
	// Any update request must fail locally: the fake token never leaves this machine.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")

	home := filepath.Join(t.TempDir(), "home")
	od := filepath.Join(t.TempDir(), "od")
	mustRun(t, home, "ctx", "add", "acme", "--label", "ACME")
	for k, v := range map[string]string{
		"ai.provider": "ollama", "ollama.enabled": "true", "ollama.url": ollama.URL,
		"onedrive_dir": od, "bookmarks.fetch_title": "false",
	} {
		mustRun(t, home, "config", "set", k, v)
	}
	base, stop := startServe(t, home)

	deck(t, "POST", base+"/api/switch/acme?note="+sNote)
	mustRun(t, home, "idea", sIdeaCLI)
	mustRun(t, home, "idea", sPersonal, "--personal")
	postJSON(t, base+"/api/idea", `{"text":"`+sIdeaHTTP+`"}`)
	postJSON(t, base+"/api/bookmark", `{"url":"http://example.invalid/`+sBookmark+`"}`)
	postText(t, base+"/api/import", "```stkr-close v1\nitem | note | acme | "+sImport+" |\n```")
	mustRun(t, home, "ai", "run-once")
	mustRun(t, home, "start-day", "--no-clipboard", "--print")
	s := startMCP(t, home)
	s.tool("add_idea", map[string]any{"text": sIdeaMCP})
	s.tool("get_digest", map[string]any{"kind": "start"})
	_ = s.stdin.Close()
	_ = s.cmd.Wait()
	_ = stop()

	logs := logText(t, home)
	if !strings.Contains(logs, "msg=http") && !strings.Contains(logs, `"msg":"http"`) {
		t.Fatalf("log has no request lines; the scan would prove nothing:\n%.500s", logs)
	}
	for _, s := range []string{sNote, sIdeaCLI, sIdeaHTTP, sIdeaMCP, sImport, sAI, sToken, sBookmark, sPersonal} {
		if strings.Contains(logs, s) {
			t.Errorf("log contains %q", s)
		}
	}

	// The token from the environment appears in no output either.
	for _, args := range [][]string{{"config", "show"}, {"--json", "version"}, {"ai", "status"}, {"update", "--check"}} {
		out, errOut, _ := run(t, home, args...)
		if strings.Contains(out+errOut, sToken) {
			t.Errorf("stkr %v prints the token", args)
		}
	}
	base2, _ := startServe(t, home)
	for _, p := range []string{"/ui/settings", "/ui/today", "/ui/agents", "/api/contexts", "/api/capture/health"} {
		if _, b := call(t, "GET", base2+p); strings.Contains(string(b), sToken) {
			t.Errorf("%s shows the token", p)
		}
	}
	// Personal ideas are not exported or shown to the agent digest.
	if b, _ := os.ReadFile(filepath.Join(od, "_daily", "daily-digest.md")); strings.Contains(string(b), sPersonal) {
		t.Error("personal idea in the digest file")
	}
}

// ---- Non-functional ----

// Startup never waits for the network: with the update check pointed at a
// black hole the server still starts at once and answers during the check.
func TestAcceptanceStartupWithoutNetwork(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://10.255.255.1:9")
	t.Setenv("HTTP_PROXY", "http://10.255.255.1:9")
	t.Setenv("NO_PROXY", "")
	home := filepath.Join(t.TempDir(), "home")
	mustRun(t, home, "config", "set", "update.check", "true")

	start := time.Now()
	base, _ := startServe(t, home)
	elapsed := time.Since(start)
	t.Logf("startup to first line: %v", elapsed)
	if elapsed > 2*time.Second { // budget is 1 s; CI machines get a margin
		t.Errorf("startup took %v with an unreachable network", elapsed)
	}
	t0 := time.Now()
	if code, _ := deck(t, "POST", base+"/api/switch/planning"); code != 200 {
		t.Fatalf("switch: %d", code)
	}
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Errorf("deck POST took %v while the update check hangs", d)
	}
	if code, _ := call(t, "GET", base+"/healthz"); code != 200 {
		t.Errorf("healthz: %d", code)
	}
}

// Real HTTP round trips against the binary: deck POST under 100 ms and key
// image GET under 50 ms at the 95th percentile (best of three runs, so one
// noisy moment on a shared machine does not fail the build).
func TestAcceptancePerfBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	home := filepath.Join(t.TempDir(), "home")
	base, _ := startServe(t, home)
	client := &http.Client{}
	measure := func(method, path string, n int) time.Duration {
		d := make([]time.Duration, n)
		for i := range d {
			p := path
			if strings.Contains(path, "%s") {
				p = fmt.Sprintf(path, []string{"planning", "meeting"}[i%2])
			}
			req, _ := http.NewRequest(method, base+p, nil)
			req.Header.Set("X-STKR-Source", "deck")
			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			d[i] = time.Since(start)
			if resp.StatusCode != 200 {
				t.Fatalf("%s %s: %d", method, p, resp.StatusCode)
			}
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		return d[n*95/100]
	}
	check := func(name, method, path string, budget time.Duration) {
		measure(method, path, 30) // warm-up
		var got time.Duration
		for attempt := 1; attempt <= 3; attempt++ {
			got = measure(method, path, 200)
			t.Logf("%s p95 = %v (budget %v)", name, got, budget)
			if got <= budget {
				return
			}
		}
		t.Errorf("%s p95 = %v, budget %v", name, got, budget)
	}
	check("POST /api/switch", "POST", "/api/switch/%s", 100*time.Millisecond)
	check("GET /img/now.png", "GET", "/img/now.png", 50*time.Millisecond)
	check("GET /api/now", "GET", "/api/now", 100*time.Millisecond)
}
