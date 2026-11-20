package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestTrackingCommands(t *testing.T) {
	home := t.TempDir()

	out, _, code := exec(t, home, "ctx", "add", "acme", "--label", "ACME", "--kind", "project", "--ticket", "AC-1")
	if code != 0 || !strings.Contains(out, "added acme") {
		t.Fatalf("ctx add: %d %q", code, out)
	}
	out, _, code = exec(t, home, "switch", "acme", "-n", "start")
	if code != 0 || !strings.HasPrefix(out, "switched to acme") {
		t.Fatalf("switch: %d %q", code, out)
	}
	out, _, code = exec(t, home, "switch", "acme", "--json")
	var sr domain.SwitchResult
	if code != 0 || json.Unmarshal([]byte(out), &sr) != nil || sr.Created || sr.Event.ID != 1 {
		t.Fatalf("double switch: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "switch", "acme")
	if !strings.HasPrefix(out, "already on acme") {
		t.Fatalf("double switch human: %q", out)
	}

	out, _, code = exec(t, home, "now")
	if code != 0 || !strings.HasPrefix(out, "acme (ACME) since") || !strings.Contains(out, "today ") {
		t.Fatalf("now: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "now", "--json")
	var ns domain.NowStatus
	if json.Unmarshal([]byte(out), &ns) != nil || !ns.Tracking || ns.Context.ID != "acme" || ns.Note != "start" {
		t.Fatalf("now --json: %q", out)
	}

	out, _, code = exec(t, home, "stop")
	if code != 0 || !strings.HasPrefix(out, "stopped at") {
		t.Fatalf("stop: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "stop")
	if strings.TrimSpace(out) != "not tracking" {
		t.Fatalf("stop again: %q", out)
	}
	out, _, _ = exec(t, home, "stop", "--json")
	if !strings.Contains(out, `"created": false`) {
		t.Fatalf("stop --json: %q", out)
	}
	out, _, _ = exec(t, home, "now")
	if !strings.HasPrefix(out, "not tracking") {
		t.Fatalf("now after stop: %q", out)
	}

	out, _, code = exec(t, home, "day")
	if code != 0 || !strings.Contains(out, "EVENT") || !strings.Contains(out, "acme") || !strings.Contains(out, "counted ") {
		t.Fatalf("day: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "day", "today", "--json")
	var dr domain.DayReport
	if json.Unmarshal([]byte(out), &dr) != nil || len(dr.Blocks) != 1 || dr.Blocks[0].ContextID != "acme" || len(dr.Events) != 2 {
		t.Fatalf("day --json: %q", out)
	}
	out, _, _ = exec(t, home, "day", "2000-01-03")
	if !strings.Contains(out, "no time recorded") || !strings.Contains(out, "not a working day") {
		t.Fatalf("empty day: %q", out)
	}

	out, _, code = exec(t, home, "day", "confirm", "1")
	if code != 0 || !strings.Contains(out, "event 1 (acme") || !strings.Contains(out, "confirmed") {
		t.Fatalf("confirm: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "day", "confirm", "1", "--undo", "--json")
	if !strings.Contains(out, `"confirmed": false`) {
		t.Fatalf("confirm --undo: %q", out)
	}

	out, _, code = exec(t, home, "week")
	if code != 0 || !strings.Contains(out, "CONTEXT") || !strings.Contains(out, "working days") {
		t.Fatalf("week: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "week", "--json")
	var wr domain.WeekReport
	if json.Unmarshal([]byte(out), &wr) != nil || len(wr.Days) != 7 {
		t.Fatalf("week --json: %q", out)
	}
	out, _, code = exec(t, home, "week", "--csv")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || lines[0] != "date,context,label,tempo_ticket,hours" || len(lines) != 2 || !strings.Contains(lines[1], ",acme,ACME,AC-1,") {
		t.Fatalf("week --csv: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "week", "2000-W01")
	if !strings.Contains(out, "no time recorded") {
		t.Fatalf("empty week: %q", out)
	}
}

func TestTrackingErrors(t *testing.T) {
	home := t.TempDir()
	tests := []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"switch", "nope"}, ExitError, "stkr ctx add nope"},
		{[]string{"switch", "Bad Id"}, ExitUsage, "invalid context id"},
		{[]string{"switch"}, ExitUsage, "accepts 1 arg"},
		{[]string{"day", "yesterdayish"}, ExitUsage, "invalid date"},
		{[]string{"day", "confirm", "x"}, ExitUsage, "invalid event id"},
		{[]string{"day", "confirm", "42"}, ExitError, "not found"},
		{[]string{"week", "2026-47"}, ExitUsage, "invalid week"},
		{[]string{"ctx", "add", "off"}, ExitError, "already exists"},
		{[]string{"ctx", "add", "x", "--kind", "work"}, ExitUsage, "invalid kind"},
		{[]string{"ctx", "park", "off"}, ExitUsage, "cannot be parked"},
		{[]string{"ctx", "set", "nope", "--label", "x"}, ExitError, "not found"},
		{[]string{"ctx", "list", "--kind", "work"}, ExitUsage, "invalid kind"},
		{[]string{"import-legacy", filepath.Join(home, "missing")}, ExitError, "legacy path"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, errOut, code := exec(t, home, tt.args...)
			if code != tt.code || !strings.Contains(errOut, tt.msg) {
				t.Fatalf("code=%d stderr=%q", code, errOut)
			}
		})
	}
}

func TestCtxCommands(t *testing.T) {
	home := t.TempDir()
	out, _, code := exec(t, home, "ctx", "list")
	if code != 0 || !strings.Contains(out, "planning") || !strings.Contains(out, "STATUS") {
		t.Fatalf("list: %d %q", code, out)
	}
	_, _, _ = exec(t, home, "ctx", "add", "acme")
	out, _, code = exec(t, home, "ctx", "set", "acme", "--label", "ACME", "--ticket", "AC-2", "--kind", "mode", "--sort", "5", "--json")
	var c domain.Context
	if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.Label != "ACME" || c.Kind != domain.KindMode || c.Sort != 5 || c.TempoTicket != "AC-2" {
		t.Fatalf("set: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "ctx", "set", "acme", "--ticket", "")
	if !strings.Contains(out, "ticket -") {
		t.Fatalf("clear ticket: %q", out)
	}
	out, _, code = exec(t, home, "ctx", "park", "acme")
	if code != 0 || strings.TrimSpace(out) != "acme is parked" {
		t.Fatalf("park: %d %q", code, out)
	}
	out, _, _ = exec(t, home, "ctx", "list")
	if strings.Contains(out, "acme") {
		t.Fatal("parked context listed")
	}
	out, _, _ = exec(t, home, "ctx", "list", "--all", "--json")
	var cs []domain.Context
	if json.Unmarshal([]byte(out), &cs) != nil || len(cs) != 6 {
		t.Fatalf("list --all: %q", out)
	}
	if _, errOut, code := exec(t, home, "switch", "acme"); code != ExitError || !strings.Contains(errOut, "ctx activate acme") {
		t.Fatalf("switch parked: %d %q", code, errOut)
	}
	out, _, _ = exec(t, home, "ctx", "activate", "acme", "--json")
	if !strings.Contains(out, `"active": true`) {
		t.Fatalf("activate: %q", out)
	}
	out, _, _ = exec(t, home, "ctx", "list", "--kind", "personal")
	if !strings.Contains(out, "break") || strings.Contains(out, "planning") {
		t.Fatalf("list --kind: %q", out)
	}
}

func TestBackupAndLegacyCommands(t *testing.T) {
	home := t.TempDir()
	out, _, code := exec(t, home, "backup")
	p := strings.TrimSpace(out)
	if code != 0 || filepath.Dir(p) != filepath.Join(home, "backups") {
		t.Fatalf("backup: %d %q", code, out)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	out, _, _ = exec(t, home, "backup", "--json")
	if !strings.Contains(out, `"path"`) {
		t.Fatalf("backup --json: %q", out)
	}

	legacyHome := t.TempDir()
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "tasklog.json"), []byte("[]"), 0o600)
	out, _, code = exec(t, legacyHome, "import-legacy", src)
	if code != 0 || !strings.Contains(out, "archived 1 file(s)") || !strings.Contains(out, "tasklog.json") {
		t.Fatalf("import-legacy: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(legacyHome, "stkr.db")); err == nil {
		t.Fatal("import-legacy opened the database")
	}
	out, _, _ = exec(t, t.TempDir(), "import-legacy", filepath.Join(src, "tasklog.json"), "--json")
	if !strings.Contains(out, `"files"`) {
		t.Fatalf("import-legacy --json: %q", out)
	}
}
