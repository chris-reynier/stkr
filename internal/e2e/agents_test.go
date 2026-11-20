package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func postJSON(t *testing.T, url, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestSmokeP04Agents(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	base, _ := startServe(t, home)

	for _, ev := range []string{
		`{"instance":"w","event":"state","state":"working","slot":1}`,
		`{"instance":"d","event":"state","state":"done","slot":2}`,
		`{"instance":"boom","event":"state","state":"error","slot":3,"summary":"tests failed"}`,
	} {
		if code, b := postJSON(t, base+"/api/agents/event", ev); code != 200 {
			t.Fatalf("event: %d %s", code, b)
		}
	}
	var next struct {
		Instance struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"instance"`
	}
	code, b := call(t, "GET", base+"/api/agents/next")
	if code != 200 || json.Unmarshal(b, &next) != nil || next.Instance.ID != "boom" {
		t.Fatalf("next: %d %s", code, b)
	}

	// queue, ack
	if code, b := call(t, "POST", base+"/api/agents/w/queue"); code != 200 {
		t.Fatalf("queue: %d %s", code, b)
	}
	if code, b := call(t, "POST", base+"/api/agents/ack-done"); code != 200 || !strings.Contains(string(b), `"acknowledged":1`) {
		t.Fatalf("ack: %d %s", code, b)
	}
	code, b = call(t, "GET", base+"/api/agents")
	if code != 200 || !strings.Contains(string(b), `"idle":1`) || !strings.Contains(string(b), `"queued":true`) {
		t.Fatalf("list: %d %s", code, b)
	}

	// next/go for an instance with nothing to focus or open fails cleanly
	code, b = call(t, "POST", base+"/api/agents/next/go", "X-STKR-Source", "deck")
	if code != 200 || !strings.Contains(string(b), `"result":"failed"`) || !strings.Contains(string(b), `"id":"boom"`) {
		t.Fatalf("go: %d %s", code, b)
	}

	// image and UI
	if code, b := call(t, "GET", base+"/img/agents.png"); code != 200 || len(b) < 100 {
		t.Fatalf("image: %d len=%d", code, len(b))
	}
	if code, b := call(t, "GET", base+"/ui/agents"); code != 200 || !strings.Contains(string(b), "tests failed") {
		t.Fatalf("ui: %d", code)
	}

	// CLI reads the same database
	out, _, rc := run(t, home, "agents")
	if rc != 0 || !strings.Contains(out, "boom") || !strings.Contains(out, "tests failed") {
		t.Fatalf("agents: rc=%d %q", rc, out)
	}
	out, _, rc = run(t, home, "agents", "next", "--json")
	if rc != 0 || !strings.Contains(out, `"id": "boom"`) {
		t.Fatalf("agents next: rc=%d %q", rc, out)
	}

	// close removes
	postJSON(t, base+"/api/agents/event", `{"instance":"boom","event":"closed"}`)
	postJSON(t, base+"/api/agents/event", `{"instance":"w","event":"closed"}`)
	postJSON(t, base+"/api/agents/event", `{"instance":"d","event":"closed"}`)
	code, b = call(t, "GET", base+"/api/agents/next")
	if code != 200 || !strings.Contains(string(b), `"instance":null`) {
		t.Fatalf("empty next: %d %s", code, b)
	}
}
