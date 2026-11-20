package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// postForm posts a form with a same-origin Origin header and does not follow
// the redirect.
func postForm(t *testing.T, base, path string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp
}

func TestSmokeP03WebUI(t *testing.T) {
	now := time.Now()
	if now.Hour() == 0 && now.Minute() < 5 {
		t.Skip("needs a few minutes after local midnight to split a block")
	}
	home := filepath.Join(t.TempDir(), "home")
	if _, _, code := run(t, home, "ctx", "add", "acme", "--label", "ACME", "--kind", "project"); code != 0 {
		t.Fatal("ctx add failed")
	}
	if _, _, code := run(t, home, "ctx", "set", "acme", "--tempo-ticket", "ACME-1"); code != 0 {
		t.Fatal("ctx set failed")
	}
	base, stop := startServe(t, home)

	for _, p := range []struct{ path, want string }{
		{"/ui/today", "Timeline"}, {"/ui/week", "Hours per context"}, {"/ui/settings", "Add a context"},
		{"/static/app.css", "--bg"},
	} {
		code, b := call(t, "GET", base+p.path)
		if code != 200 || !strings.Contains(string(b), p.want) {
			t.Fatalf("%s: %d", p.path, code)
		}
	}
	code, b := call(t, "GET", base+"/api/week/2026-W47/tempo.csv")
	if code != 200 || string(b) != "date,ticket,hours,context,description\r\n" {
		t.Fatalf("tempo.csv: %d %q", code, b)
	}

	// Track acme, then use the UI to move the start to 00:00 and split at 00:02.
	if code, _ := call(t, "POST", base+"/api/switch/acme"); code != 200 {
		t.Fatalf("switch: %d", code)
	}
	date := now.Format("2006-01-02")
	var before struct {
		Events []struct {
			ID int64 `json:"id"`
		} `json:"events"`
	}
	out, _, _ := run(t, home, "day", date, "--json")
	if err := json.Unmarshal([]byte(out), &before); err != nil || len(before.Events) != 1 {
		t.Fatalf("day: %v %q", err, out)
	}
	id := before.Events[0].ID
	next := "/ui/today?d=" + date
	idPath := "/ui/event/" + itoa(id)
	resp := postForm(t, base, idPath+"/edit", url.Values{"next": {next}, "date": {date}, "orig_time": {"x"}, "time": {"00:00"}, "context": {"acme"}, "note": {"e2e"}})
	if resp.StatusCode != 303 || resp.Header.Get("Location") != next {
		t.Fatalf("edit: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp = postForm(t, base, idPath+"/split", url.Values{"next": {next}, "date": {date}, "time": {"00:02"}})
	if resp.StatusCode != 303 {
		t.Fatalf("split: %d", resp.StatusCode)
	}
	out, _, _ = run(t, home, "day", date, "--json")
	var after struct {
		Events []struct {
			TS   string `json:"ts"`
			Note string `json:"note"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &after); err != nil || len(after.Events) != 2 || !strings.Contains(after.Events[1].TS, "T00:02:00") || after.Events[0].Note != "e2e" {
		t.Fatalf("after split: %v %q", err, out)
	}
	code, b = call(t, "GET", base+"/ui/today")
	if code != 200 || !strings.Contains(string(b), "e2e") {
		t.Fatalf("today after split: %d", code)
	}
	if err := stop(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
