package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A fake Ollama server drives the real binary: capture stays instant, then
// `stkr ai run-once` sorts the idea and personal ideas are skipped only when
// the provider is not local (here it is, so both are processed).
func TestSmokeP07AI(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = io.WriteString(w, `{"version":"0.0.1"}`)
			return
		}
		calls.Add(1)
		inner, _ := json.Marshal(map[string]string{"title": "Try htmx", "text": "Try htmx swaps.", "suggested_project": ""})
		out, _ := json.Marshal(map[string]any{"message": map[string]string{"content": string(inner)}})
		_, _ = w.Write(out)
	}))
	defer srv.Close()
	home := filepath.Join(t.TempDir(), "home")

	if out, errOut, code := run(t, home, "ai", "status"); code != 0 || !strings.Contains(out, "active:    no") {
		t.Fatalf("status: %d %s %s", code, out, errOut)
	}
	if _, errOut, code := run(t, home, "idea", "try htmx swaps"); code != 0 {
		t.Fatalf("idea: %d %s", code, errOut)
	}
	if _, _, code := run(t, home, "ai", "run-once"); code != 2 {
		t.Fatalf("run-once without provider: exit %d, want 2", code)
	}
	for _, kv := range [][2]string{{"ai.provider", "ollama"}, {"ollama.enabled", "true"}, {"ollama.url", srv.URL}} {
		if _, errOut, code := run(t, home, "config", "set", kv[0], kv[1]); code != 0 {
			t.Fatalf("config set %s: %d %s", kv[0], code, errOut)
		}
	}
	if out, errOut, code := run(t, home, "ai", "status"); code != 0 || !strings.Contains(out, "reachable: yes") || !strings.Contains(out, "0.0.1") {
		t.Fatalf("status: %d %s %s", code, out, errOut)
	}
	if out, errOut, code := run(t, home, "ai", "test"); code != 0 || !strings.Contains(out, "answered") {
		t.Fatalf("test: %d %s %s", code, out, errOut)
	}
	if out, errOut, code := run(t, home, "ai", "run-once"); code != 0 || !strings.Contains(out, "done 1") {
		t.Fatalf("run-once: %d %s %s", code, out, errOut)
	}
	out, _, _ := run(t, home, "ideas", "--json")
	var list struct {
		Ideas []struct {
			RawText   string `json:"raw_text"`
			CleanText string `json:"clean_text"`
			Title     string `json:"title"`
			AIState   string `json:"ai_state"`
		} `json:"ideas"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Ideas) != 1 {
		t.Fatalf("ideas: %v %s", err, out)
	}
	if i := list.Ideas[0]; i.AIState != "done" || i.RawText != "try htmx swaps" || i.CleanText != "Try htmx swaps." || i.Title != "Try htmx" {
		t.Errorf("idea = %+v", i)
	}

	// Switching provider back needs no rebuild.
	if _, _, code := run(t, home, "config", "set", "ai.provider", "none"); code != 0 {
		t.Fatal("config set none")
	}
	if out, _, _ := run(t, home, "ai", "status"); !strings.Contains(out, "active:    no") {
		t.Errorf("status after switch: %s", out)
	}
}
