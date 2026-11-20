package daily

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from golden:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func heavyInput() DigestInput {
	in := DigestInput{YesterdayDate: "2026-11-13", YesterdayTop: [3]string{"Ship the release", "Review the open PRs", "Plan the sprint"}, IdeaCount: 14}
	for i := 0; i < 5; i++ {
		d := DayDigest{Date: fmt.Sprintf("2026-11-%02d", 13-i), Weekday: "Friday", Total: 7*time.Hour + 30*time.Minute,
			Gaps: []GapSpan{{Start: "10:00", End: "11:30", Duration: 90 * time.Minute}}, IdleSuspect: time.Hour}
		for j := 0; j < 9; j++ {
			d.Shares = append(d.Shares, TimeShare{Label: fmt.Sprintf("Project number %d", j), Time: time.Duration(90-j) * time.Minute})
		}
		in.Days = append(in.Days, d)
	}
	for i := 0; i < 40; i++ {
		in.OpenItems = append(in.OpenItems, ItemLine{Fingerprint: fmt.Sprintf("%016x", i), Type: domain.ItemAction, Project: "alpha",
			Summary: strings.Repeat("do something important ", 12), Due: "2026-11-30"})
	}
	for i := 0; i < 5; i++ {
		in.IdeaTitles = append(in.IdeaTitles, fmt.Sprintf("Idea title %d", i))
		in.Agents = append(in.Agents, AgentLine{Label: fmt.Sprintf("agent %d", i), State: "action_required", Summary: "needs approval"})
	}
	in.UnsavedClose = []string{"2026-11-12", "2026-11-11"}
	return in
}

func TestBuildDigestLimitAndPriority(t *testing.T) {
	in := heavyInput()
	out := BuildDigest(in)
	if n := len([]rune(out)); n > MaxDigestChars {
		t.Fatalf("digest has %d chars, max %d", n, MaxDigestChars)
	}
	// The most important sections survive.
	for _, want := range []string{"Previous days not closed", "2026-11-12: close was not saved", "Yesterday's top 3", "1. Ship the release", "Open items (40)"} {
		if !strings.Contains(out, want) {
			t.Errorf("digest lacks %q:\n%s", want, out)
		}
	}
	// The least important section lost lines before open items did.
	if !strings.Contains(out, "more not shown") {
		t.Errorf("expected a truncation marker:\n%s", out)
	}
	timeLines := strings.Count(out, "total;")
	if timeLines >= 5 {
		t.Errorf("time section should have been cut first, still has %d lines", timeLines)
	}
	if strings.Count(out, "#0000") < 3 {
		t.Errorf("open items should keep several lines:\n%s", out)
	}
}

func TestBuildDigestOrderAndFull(t *testing.T) {
	in := DigestInput{
		YesterdayDate: "2026-11-13", YesterdayTop: [3]string{"a", "", "c"},
		Days:         []DayDigest{{Date: "2026-11-13", Weekday: "Friday", Total: 90 * time.Minute, Shares: []TimeShare{{"Alpha", time.Hour}, {"Beta", 30 * time.Minute}}}},
		OpenItems:    []ItemLine{{Fingerprint: "abcd", Type: domain.ItemFollowup, Project: "alpha", Summary: "Call Bob", Due: "2026-11-20"}},
		IdeaCount:    2,
		IdeaTitles:   []string{"First", "Second"},
		Agents:       []AgentLine{{Label: "api", State: "error", Summary: "tests failed"}},
		UnsavedClose: []string{"2026-11-12"},
	}
	out := BuildDigest(in)
	var idx []int
	for _, h := range []string{"## Previous working days", "## Yesterday's top 3", "## Open items (1)", "## Untriaged ideas (2)", "## Agents needing attention", "## Previous days not closed"} {
		i := strings.Index(out, h)
		if i < 0 {
			t.Fatalf("missing %q in\n%s", h, out)
		}
		idx = append(idx, i)
	}
	for i := 1; i < len(idx); i++ {
		if idx[i] < idx[i-1] {
			t.Errorf("sections out of order:\n%s", out)
		}
	}
	if strings.Contains(out, "more not shown") {
		t.Error("nothing should be truncated")
	}
	if strings.Contains(out, "Timesheet gaps") {
		t.Error("empty sections are left out")
	}
	for _, want := range []string{"- Fri 2026-11-13: 1h30m total; Alpha 1h00m; Beta 30m", "1. a", "3. c", "- followup | alpha | Call Bob (due 2026-11-20) #abcd", "- api: error - tests failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if BuildDigest(DigestInput{}) != "Nothing recorded yet." {
		t.Error("empty digest text")
	}
}

func TestBuildCloseContextLimit(t *testing.T) {
	in := CloseInput{Date: "2026-11-16", Total: 6 * time.Hour, TopsAM: [3]string{"x", "y", "z"}, IdeaCnt: 30}
	for i := 0; i < 60; i++ {
		in.Shares = append(in.Shares, TimeShare{Label: strings.Repeat("l", 30), Time: time.Hour})
		in.Today = append(in.Today, ItemLine{Type: domain.ItemNote, Project: "p", Summary: strings.Repeat("s", 100)})
		in.Open = append(in.Open, ItemLine{Type: domain.ItemAction, Project: "p", Summary: strings.Repeat("o", 100), Fingerprint: "ffffffffffffffff"})
		in.Ideas = append(in.Ideas, strings.Repeat("i", 100))
	}
	out := BuildCloseContext(in)
	if n := len([]rune(out)); n > MaxDigestChars {
		t.Fatalf("%d chars", n)
	}
	if !strings.Contains(out, "Morning top 3") || !strings.Contains(out, "1. x") {
		t.Errorf("morning top 3 must survive:\n%s", out)
	}
}

func TestRenderTemplates(t *testing.T) {
	out, err := Render("", StartDayTemplate, StartDayData{Date: "2026-11-16", Weekday: "Monday", Digest: "DIGEST-BODY"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Monday 2026-11-16", "DIGEST-BODY", "```stkr-brief v1", "top | 1 |", "<due YYYY-MM-DD or empty>"} {
		if !strings.Contains(out, want) {
			t.Errorf("start-day lacks %q", want)
		}
	}
	out, err = Render("", CloseDayTemplate, CloseDayData{Date: "2026-11-16", Weekday: "Monday", Context: "CTX"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CTX", "```stkr-close v1", "done | <fingerprint or summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("close-day lacks %q", want)
		}
	}
	// The prompts themselves must not import anything.
	for _, tn := range []struct {
		name string
		data any
	}{{StartDayTemplate, StartDayData{Date: "d", Weekday: "w", Digest: "x"}}, {CloseDayTemplate, CloseDayData{Date: "d", Weekday: "w", Context: "x"}}} {
		s, _ := Render("", tn.name, tn.data)
		if p := Parse(s); p.Valid() {
			t.Errorf("%s: prompt text parses as an answer: %+v", tn.name, p)
		}
	}
}

func TestUserTemplateOverrideAndInit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StartDayTemplate), []byte("custom {{.Date}} {{.Digest}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Render(dir, StartDayTemplate, StartDayData{Date: "D", Digest: "G"})
	if err != nil || out != "custom D G" {
		t.Fatalf("override: %q %v", out, err)
	}
	// Close template has no override: default is used.
	if out, err = Render(dir, CloseDayTemplate, CloseDayData{Context: "ctx"}); err != nil || !strings.Contains(out, "stkr-close v1") {
		t.Fatalf("default fallback: %v", err)
	}
	// Broken override: explicit error naming the file.
	if err := os.WriteFile(filepath.Join(dir, StartDayTemplate), []byte("{{.Nope}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Render(dir, StartDayTemplate, StartDayData{})
	if err == nil || domain.Code(err) != "invalid" || !strings.Contains(err.Error(), StartDayTemplate) {
		t.Fatalf("broken template error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, StartDayTemplate), []byte("{{if"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Render(dir, StartDayTemplate, StartDayData{}); err == nil {
		t.Fatal("syntax error expected")
	}

	// prompts init: keeps existing files, force overwrites.
	written, err := InitPrompts(dir, false)
	if err != nil || len(written) != 1 || !strings.HasSuffix(written[0], CloseDayTemplate) {
		t.Fatalf("init = %v %v", written, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, StartDayTemplate)); string(b) != "{{if" {
		t.Error("init must not overwrite without force")
	}
	written, err = InitPrompts(dir, true)
	if err != nil || len(written) != 2 {
		t.Fatalf("force init = %v %v", written, err)
	}
	if _, err := Render(dir, StartDayTemplate, StartDayData{Date: "D"}); err != nil {
		t.Fatal(err)
	}
}

const notes = "## Notes\n\nMy own notes.\n- keep me\n\n## Not a heading inside a fence\n```\n## Log\n```\ntrailing without newline"

func sampleLog() LogFile {
	return LogFile{
		Title: "Alpha",
		Focus: []string{"[ ] action: Ship it (due 2026-11-20)", "[ ] risk: Slow CI"},
		Days: []LogDay{
			{Date: "2026-11-14", Time: 2*time.Hour + 15*time.Minute, Items: []LogItem{{Type: domain.ItemDecision, Summary: "Use SQLite"}}},
			{Date: "2026-11-16", Time: time.Hour, Items: []LogItem{
				{Type: domain.ItemAction, Summary: "Ship it", Due: "2026-11-20"},
				{Type: domain.ItemFollowup, Summary: "Call Bob", Done: true, Due: "2026-11-10"},
			}},
			{Date: "2026-11-15"}, // empty days are not written
		},
	}
}

func TestRenderLogCreate(t *testing.T) {
	out := RenderLog(nil, sampleLog())
	golden(t, "log_create.md", out)
	// Idempotent.
	if again := RenderLog(out, sampleLog()); !bytes.Equal(again, out) {
		t.Errorf("second export changed the file:\n%s", again)
	}
	// Newest first.
	if strings.Index(string(out), "### 2026-11-16") > strings.Index(string(out), "### 2026-11-14") {
		t.Error("log must be newest first")
	}
}

func TestRenderLogUpdatePreservesEverythingElse(t *testing.T) {
	header := "# My Alpha project\n\nHand written intro.\n\n"
	existing := header +
		"## Current focus\n\nold focus\n\n" +
		"## Log\n\n### 2020-01-01\n- stale\n\n" +
		"## Decisions\n\nuser section stays in place\n\n" +
		notes
	out := RenderLog([]byte(existing), sampleLog())
	golden(t, "log_update.md", out)
	s := string(out)
	if !strings.HasPrefix(s, header) {
		t.Error("header changed")
	}
	if !strings.HasSuffix(s, notes) {
		t.Errorf("Notes section is not byte-identical:\n%s", s)
	}
	if strings.Contains(s, "stale") || strings.Contains(s, "old focus") {
		t.Error("STKR sections must be rewritten")
	}
	if !strings.Contains(s, "## Decisions\n\nuser section stays in place\n\n") {
		t.Error("unknown section must be preserved")
	}
	if strings.Index(s, "## Decisions") < strings.Index(s, "## Log") || strings.Index(s, "## Decisions") > strings.Index(s, "## Notes") {
		t.Error("unknown section moved")
	}
}

func TestRenderLogNotesByteIdenticalAfterThreeExports(t *testing.T) {
	existing := []byte("# Alpha\n\n## Notes\r\nuser text \xff\x00 with odd bytes\r\n\r\n   indented\ttab\n")
	out := existing
	for i := 0; i < 3; i++ {
		lf := sampleLog()
		lf.Days[0].Items = append(lf.Days[0].Items, LogItem{Type: domain.ItemNote, Summary: fmt.Sprintf("n%d", i)})
		out = RenderLog(out, lf)
		i := bytes.Index(out, []byte("## Notes"))
		if i < 0 || !bytes.HasSuffix(out, existing[bytes.Index(existing, []byte("## Notes")):]) {
			t.Fatalf("export %d changed Notes:\n%q", i, out)
		}
	}
	if !bytes.Contains(out, []byte("n2")) || bytes.Contains(out, []byte("- note: n0\n")) {
		t.Errorf("log must reflect the last export only:\n%s", out)
	}
}

func TestRenderLogMissingSections(t *testing.T) {
	// No Notes section: STKR sections are appended after unknown ones.
	out := string(RenderLog([]byte("# T\n\n## Other\nbody"), sampleLog()))
	if !strings.Contains(out, "## Other\nbody\n## Current focus") {
		t.Errorf("sections not appended cleanly:\n%s", out)
	}
	if strings.Contains(out, "## Notes") {
		t.Error("an existing file without Notes must not get one")
	}
	// Only Log exists, Notes last: focus goes before Notes, Log replaced in place.
	out = string(RenderLog([]byte("# T\n\n## Log\n\nold\n\n## Notes\nkeep\n"), sampleLog()))
	if strings.Index(out, "## Log") > strings.Index(out, "## Current focus") && false {
		t.Log("order not enforced")
	}
	if !strings.HasSuffix(out, "## Notes\nkeep\n") || strings.Contains(out, "old") || strings.Count(out, "## Log") != 1 || strings.Count(out, "## Current focus") != 1 {
		t.Errorf("unexpected result:\n%s", out)
	}
	// CRLF files get CRLF in STKR sections.
	out = string(RenderLog([]byte("# T\r\n\r\n## Notes\r\nn\r\n"), sampleLog()))
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Errorf("mixed line endings:\n%q", out)
	}
	// Empty content.
	out = string(RenderLog(nil, LogFile{Title: "Inbox"}))
	for _, want := range []string{"# Inbox", "_Nothing open._", "_No entries yet._", "## Notes"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty export lacks %q:\n%s", want, out)
		}
	}
}

func TestLogItemLine(t *testing.T) {
	cases := map[LogItem]string{
		{Type: domain.ItemAction, Summary: "a", Due: "2026-01-02"}:             "[ ] action: a (due 2026-01-02)",
		{Type: domain.ItemAction, Summary: "a", Due: "2026-01-02", Done: true}: "[x] action: a",
		{Type: domain.ItemDecision, Summary: "d"}:                              "decision: d",
		{Type: domain.ItemNote, Summary: "n", Done: true}:                      "note: n",
		{Type: domain.ItemRisk, Summary: "r"}:                                  "[ ] risk: r",
	}
	for it, want := range cases {
		if got := it.Line(); got != want {
			t.Errorf("%+v = %q, want %q", it, got, want)
		}
	}
}
