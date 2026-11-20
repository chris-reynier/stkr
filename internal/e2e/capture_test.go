package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSmokeP06Capture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	if out, errOut, code := run(t, home, "idea", "try htmx swaps"); code != 0 || !strings.Contains(out, "Saved idea #1") {
		t.Fatalf("idea: %d %s %s", code, out, errOut)
	}
	if _, errOut, code := run(t, home, "idea", "dentist", "--personal"); code != 0 {
		t.Fatalf("personal idea: %d %s", code, errOut)
	}
	out, errOut, code := run(t, home, "ideas", "--json")
	if code != 0 {
		t.Fatalf("ideas: %d %s", code, errOut)
	}
	var list struct {
		Ideas []struct {
			ID      int64  `json:"id"`
			RawText string `json:"raw_text"`
			Tag     string `json:"tag"`
			Status  string `json:"status"`
			AIState string `json:"ai_state"`
		} `json:"ideas"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	tags := map[string]string{}
	for _, i := range list.Ideas {
		tags[i.RawText] = i.Tag
		if i.Status != "new" || i.AIState != "pending" {
			t.Errorf("idea %+v", i)
		}
	}
	if len(list.Ideas) != 2 || tags["try htmx swaps"] != "pro" || tags["dentist"] != "personal" {
		t.Errorf("ideas = %v", list.Ideas)
	}
	out, _, _ = run(t, home, "ideas", "--tag", "personal")
	if !strings.Contains(out, "dentist") || strings.Contains(out, "htmx") {
		t.Errorf("tag filter: %s", out)
	}

	// Meeting capture and invalid input.
	if out, _, code = run(t, home, "idea", "send the budget", "--type", "action"); code != 0 || !strings.Contains(out, "action") {
		t.Errorf("meeting action: %d %s", code, out)
	}
	if _, _, code = run(t, home, "idea", "x", "--type", "todo"); code != 2 {
		t.Errorf("bad type exit = %d, want 2", code)
	}
	if _, _, code = run(t, home, "idea"); code != 2 {
		t.Errorf("no text exit = %d, want 2", code)
	}

	// Bookmarks: invalid URLs never touch the network and exit 2.
	for _, bad := range []string{"not a url", "ftp://example.com/x", "javascript:alert(1)"} {
		if _, errOut, code := run(t, home, "bookmark", bad); code != 2 || !strings.Contains(errOut, "http") {
			t.Errorf("bookmark %q: exit %d, stderr %q", bad, code, errOut)
		}
	}
	if out, _, code := run(t, home, "links"); code != 0 || !strings.Contains(out, "No links") {
		t.Errorf("links: %d %s", code, out)
	}
}

func TestSmokeP06ServeCapture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	inbox := filepath.Join(t.TempDir(), "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(inbox, "p_call-mum.txt")
	if err := os.WriteFile(note, []byte("call mum on sunday"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(note, old, old); err != nil {
		t.Fatal(err)
	}
	if _, _, code := run(t, home, "config", "set", "phone_inbox", inbox); code != 0 {
		t.Fatal("config set phone_inbox")
	}
	base, stop := startServe(t, home)
	defer func() { _ = stop() }()

	code, body := postText(t, base+"/api/idea", `{"text":"over http","type":"decision"}`)
	if code != 201 || !strings.Contains(string(body), `"origin":"meeting"`) {
		t.Errorf("POST /api/idea: %d %s", code, body)
	}
	if code, body = call(t, "GET", base+"/api/capture/health"); code != 200 || !strings.Contains(string(body), `"phone_inbox":"ok"`) {
		t.Errorf("health: %d %s", code, body)
	}
	if code, body = call(t, "GET", base+"/img/key/ideas-inbox.png"); code != 200 || len(body) < 100 {
		t.Errorf("ideas-inbox key: %d", code)
	}
	if code, _ = call(t, "GET", base+"/ui/ideas"); code != 200 {
		t.Errorf("/ui/ideas: %d", code)
	}
	if code, _ = call(t, "GET", base+"/ui/links"); code != 200 {
		t.Errorf("/ui/links: %d", code)
	}

	// The watcher takes the phone file on its first run and moves it away.
	deadline := time.Now().Add(15 * time.Second)
	for {
		out, _, _ := run(t, home, "ideas", "--tag", "personal")
		if strings.Contains(out, "call mum on sunday") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("phone inbox file not ingested; ideas:\n%s", out)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(inbox, "processed", "p_call-mum.txt")); err != nil {
		t.Errorf("file not moved to processed: %v", err)
	}
}
