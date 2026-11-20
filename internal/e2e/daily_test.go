package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runIn is run with standard input.
func runIn(t *testing.T, home, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "STKR_HOME="+home)
	cmd.Stdin = strings.NewReader(stdin)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func postText(t *testing.T, url, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(url, "text/plain", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

const answer = "Here is the plan.\n```stkr-close v1\ntop | 1 | Ship P05\nitem | action | other | Write docs | \n```\n"

func TestSmokeP05DailyLoop(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	od := filepath.Join(t.TempDir(), "onedrive")
	if _, _, code := run(t, home, "config", "set", "onedrive_dir", od); code != 0 {
		t.Fatal("config set onedrive_dir")
	}

	// Start Day: digest and prompt on stdout, the digest file in the export folder.
	out, errOut, code := run(t, home, "start-day", "--no-clipboard", "--print")
	if code != 0 {
		t.Fatalf("start-day: %d %s", code, errOut)
	}
	for _, want := range []string{"```stkr-brief v1", "Nothing recorded yet.", "top | 1 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("start-day output lacks %q:\n%s", want, out)
		}
	}
	if len(out) > 2600+1500 { // digest limit plus the instructions of the template
		t.Errorf("prompt too long: %d bytes", len(out))
	}
	if b, err := os.ReadFile(filepath.Join(od, "_daily", "daily-digest.md")); err != nil || !strings.Contains(string(b), "Nothing recorded yet.") {
		t.Errorf("digest file: %v %s", err, b)
	}
	if o, _, c := run(t, home, "now"); c != 0 || !strings.Contains(o, "Planning") {
		t.Errorf("planning not logged: %d %q", c, o)
	}

	// Import from stdin; the second import adds nothing.
	out, errOut, code = runIn(t, home, answer, "import", "-")
	if code != 0 || !strings.Contains(out, "Imported: 1 new item(s)") {
		t.Fatalf("import: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(out, "1 of the top 3 saved") {
		t.Errorf("top line not saved: %q", out)
	}
	out, _, code = runIn(t, home, answer, "--json", "import", "-")
	var res struct {
		Created  int      `json:"items_created"`
		Existing int      `json:"items_existing"`
		State    string   `json:"state"`
		Exported []string `json:"exported"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Created != 0 || res.Existing != 1 {
		t.Fatalf("re-import: %d %q", code, out)
	}
	inbox := filepath.Join(od, "_daily", "inbox.md")
	b, err := os.ReadFile(inbox)
	if err != nil || !strings.Contains(string(b), "- [ ] action: Write docs") || !strings.Contains(string(b), "## Notes") {
		t.Fatalf("inbox.md: %v\n%s", err, b)
	}

	// The user's notes survive later exports.
	notes := "## Notes\nhand written, keep me\n"
	if err := os.WriteFile(inbox, []byte(strings.Replace(string(b), "## Notes\n\n", notes, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code = runIn(t, home, "```stkr-close v1\nitem | note | other | second import |\n```", "import", "-"); code != 0 {
		t.Fatal("second answer failed")
	}
	if b, _ = os.ReadFile(inbox); !strings.HasSuffix(string(b), notes) || !strings.Contains(string(b), "second import") {
		t.Fatalf("notes not preserved:\n%s", b)
	}

	// An answer without a block: exit 2, nothing written.
	_, errOut, code = runIn(t, home, "I could not do that", "import", "-")
	if code != 2 || !strings.Contains(errOut, "nothing was imported") || !strings.Contains(errOut, "/ui/today#import") {
		t.Fatalf("no block: %d %q", code, errOut)
	}

	// Prompt templates.
	out, _, code = run(t, home, "prompts", "init")
	if code != 0 || !strings.Contains(out, "start-day.md.tmpl") {
		t.Fatalf("prompts init: %d %q", code, out)
	}
	if err := os.WriteFile(filepath.Join(home, "prompts", "start-day.md.tmpl"), []byte("MY TEMPLATE {{.Date}}\n{{.Digest}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code = run(t, home, "start-day", "--no-clipboard", "--print")
	if code != 0 || !strings.HasPrefix(out, "MY TEMPLATE ") {
		t.Fatalf("custom template: %d %q", code, out)
	}
	out, _, code = run(t, home, "close", "--no-clipboard", "--print")
	if code != 0 || !strings.Contains(out, "stkr-close v1") || !strings.Contains(out, "Write docs") {
		t.Fatalf("close: %d %q", code, out)
	}
}

func TestSmokeP05DailyLoopHTTP(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	od := filepath.Join(t.TempDir(), "onedrive")
	if _, _, code := run(t, home, "config", "set", "onedrive_dir", od); code != 0 {
		t.Fatal("config set")
	}
	base, _ := startServe(t, home)

	code, b := call(t, "POST", base+"/api/day/start?clipboard=0&src=deck")
	if code != 200 || !strings.Contains(string(b), `"ok":true`) || !strings.Contains(string(b), `"chars"`) {
		t.Fatalf("day/start: %d %s", code, b)
	}
	code, b = postText(t, base+"/api/import", "```stkr-brief v1\ntop | 1 | Ship P05\nitem | action | other | Write docs |\n```")
	if code != 200 || !strings.Contains(string(b), `"items_created":1`) {
		t.Fatalf("import: %d %s", code, b)
	}
	code, b = postText(t, base+"/api/import", "```stkr-brief v1\ntop | 1 | Ship P05\nitem | action | other | Write docs |\n```")
	if code != 200 || !strings.Contains(string(b), `"items_created":0`) {
		t.Fatalf("re-import: %d %s", code, b)
	}
	code, b = postText(t, base+"/api/import", "no block")
	if code != 422 || !strings.Contains(string(b), `"redirect":"/ui/today#import"`) {
		t.Fatalf("bad import: %d %s", code, b)
	}
	for _, p := range []string{"/img/key/import.png", "/img/key/close.png"} {
		code, b = call(t, "GET", base+p)
		img, err := png.Decode(bytes.NewReader(b))
		if code != 200 || err != nil || img.Bounds().Dx() != 144 {
			t.Fatalf("%s: %d %v", p, code, err)
		}
	}
	// No window title configured: a clear error, never a paste into the wrong place.
	if code, b = call(t, "POST", base+"/api/paste"); code != 400 || !strings.Contains(string(b), "agent_window_title") {
		t.Fatalf("paste: %d %s", code, b)
	}
	// The Today page shows the imported plan.
	code, b = call(t, "GET", base+"/ui/today")
	if code != 200 || !strings.Contains(string(b), "Ship P05") || !strings.Contains(string(b), "Write docs") {
		t.Fatalf("today page: %d", code)
	}
	if _, err := os.Stat(filepath.Join(od, "_daily", "inbox.md")); err != nil {
		t.Errorf("export missing: %v", err)
	}
}
