package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSmokeReviewWorkflow drives the Stream Deck review workflow against a
// real `stkr serve`: activity sync, review context, prompt, preview and
// import, then the same from the command line.
func TestSmokeReviewWorkflow(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	base, stop := startServe(t, home)
	defer func() { _ = stop() }()

	if code, b := call(t, "POST", base+"/api/switch/other"); code != 200 {
		t.Fatalf("switch: %d %s", code, b)
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	ev := fmt.Sprintf(`{"events":[{"ts":%q,"app":"Code","category":"development","workspace":"stkr","duration_seconds":120,"lock_state":"active"},`+
		`{"ts":%q,"app":"LockApp","category":"lock","duration_seconds":60,"lock_state":"locked"}]}`,
		now.Add(-10*time.Minute).Format(time.RFC3339), now.Add(-5*time.Minute).Format(time.RFC3339))
	resp, err := http.Post(base+"/api/activity/events", "application/json", strings.NewReader(ev))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("activity: %d", resp.StatusCode)
	}
	resp, err = http.Post(base+"/api/activity/events", "application/json", strings.NewReader(`{"events":[{"ts":"x","window_title":"secret"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("window_title accepted: %d", resp.StatusCode)
	}

	code, b := call(t, "GET", base+"/api/review/context?date="+today)
	var rc struct {
		Date     string   `json:"date"`
		Basis    string   `json:"basis"`
		Missing  []string `json:"missing"`
		Activity struct {
			Events int `json:"events"`
		} `json:"activity"`
		Blocks []any `json:"blocks"`
	}
	if err := json.Unmarshal(b, &rc); err != nil || code != 200 || rc.Date != today || rc.Basis != "requested" || rc.Activity.Events != 2 || len(rc.Blocks) == 0 {
		t.Fatalf("review context: %d %s", code, b)
	}
	if strings.Contains(strings.Join(rc.Missing, ","), "lock_history") {
		t.Fatalf("missing: %v", rc.Missing)
	}
	if code, b := call(t, "GET", base+"/api/review/context"); code != 200 || !strings.Contains(string(b), `"previous_working_day"`) {
		t.Fatalf("default date: %d %s", code, b)
	}
	if code, _ := call(t, "GET", base+"/api/review/context?date=2026-02-30"); code != 400 {
		t.Fatalf("invalid date: %d", code)
	}
	if code, _ := call(t, "POST", base+"/api/review/context"); code != 404 && code != 405 {
		t.Fatalf("POST context: %d", code)
	}
	if code, b := call(t, "GET", base+"/api/review/prompt?date="+today); code != 200 || !strings.Contains(string(b), "stkr-review v1") {
		t.Fatalf("prompt: %d %s", code, b)
	}

	answer := "Review done.\n```stkr-review v1\ndate|type|context|text\n" + today + "|achievement|other|Shipped the review endpoint\n" +
		today + "|gap|meeting|No minutes for the morning call\n```\n"
	if code, b := postText(t, base+"/api/import/preview", answer); code != 200 || !strings.Contains(string(b), `"status":"new"`) {
		t.Fatalf("preview: %d %s", code, b)
	}
	if code, b := postText(t, base+"/api/import", answer); code != 200 || !strings.Contains(string(b), `"items_created":2`) {
		t.Fatalf("import: %d %s", code, b)
	}
	if code, b := postText(t, base+"/api/import", answer); code != 200 || !strings.Contains(string(b), `"items_existing":2`) {
		t.Fatalf("re-import: %d %s", code, b)
	}

	// The same from the command line.
	out, errOut, code := run(t, home, "review", "--date", today, "--no-clipboard", "--print")
	if code != 0 || !strings.Contains(out, "stkr-review v1") || !strings.Contains(errOut, "Review of "+today) {
		t.Fatalf("stkr review: %d %q %q", code, out, errOut)
	}
	if out, _, code := run(t, home, "review", "context", "--date", today); code != 0 || !strings.Contains(out, `"basis": "requested"`) {
		t.Fatalf("stkr review context: %d %s", code, out)
	}
	if out, _, code := run(t, home, "activity", "--date", today); code != 0 || !strings.Contains(out, "2 events") {
		t.Fatalf("stkr activity: %d %s", code, out)
	}
	if out, _, code := runIn(t, home, answer, "import", "--dry-run", "-"); code != 0 || !strings.Contains(out, "existing") || !strings.Contains(out, "Nothing was written") {
		t.Fatalf("dry run: %d %s", code, out)
	}
	if _, _, code := runIn(t, home, "```stkr-review v1\n"+today+"|note|other\n```", "import", "--dry-run", "-"); code != 2 {
		t.Fatalf("dry run of a bad block: exit %d", code)
	}
}
