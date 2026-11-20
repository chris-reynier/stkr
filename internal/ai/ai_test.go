package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

var projects = []Project{{ID: "acme", Label: "ACME"}, {ID: "other", Label: "Other"}}

const goodCleanup = `{"title":"T","text":"Clean text","suggested_project":"acme"}`

func ollamaServer(t *testing.T, answers ...string) (*httptest.Server, *atomic.Int32, *[]map[string]any) {
	t.Helper()
	var n atomic.Int32
	var reqs []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = io.WriteString(w, `{"version":"0.9.9"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		reqs = append(reqs, m)
		i := int(n.Add(1)) - 1
		if i >= len(answers) {
			i = len(answers) - 1
		}
		if answers[i] == "SLOW" {
			select {
			case <-time.After(3 * time.Second):
			case <-r.Context().Done():
			}
			return
		}
		out, _ := json.Marshal(map[string]any{"message": map[string]string{"role": "assistant", "content": answers[i]}})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &reqs
}

func newOllamaProvider(t *testing.T, url string, timeout time.Duration) *Guarded {
	t.Helper()
	b, err := NewOllama(url, "m")
	if err != nil {
		t.Fatal(err)
	}
	return &Guarded{B: b, Timeout: timeout}
}

func TestOllamaCleanup(t *testing.T) {
	tests := []struct {
		name      string
		answers   []string
		timeout   time.Duration
		wantCalls int32
		wantOK    bool
		kind      error
	}{
		{"valid", []string{goodCleanup}, time.Second, 1, true, nil},
		{"invalid then valid", []string{"not json", goodCleanup}, time.Second, 2, true, nil},
		{"invalid twice falls back", []string{"nope", `{"title":"x"}`}, time.Second, 2, false, ErrInvalidOutput},
		{"unknown field is invalid", []string{`{"title":"a","text":"b","suggested_project":"","extra":1}`}, time.Second, 2, false, ErrInvalidOutput},
		{"timeout twice", []string{"SLOW"}, 100 * time.Millisecond, 2, false, ErrTimeout},
		{"timeout then valid", []string{"SLOW", goodCleanup}, 100 * time.Millisecond, 2, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, n, reqs := ollamaServer(t, tc.answers...)
			p := newOllamaProvider(t, srv.URL, tc.timeout)
			got, err := p.Cleanup(context.Background(), Input{Text: "raw  text", Projects: projects})
			if n.Load() != tc.wantCalls {
				t.Errorf("calls = %d, want %d", n.Load(), tc.wantCalls)
			}
			if tc.wantOK {
				if err != nil || got.Text != "Clean text" || got.SuggestedProject != "acme" {
					t.Fatalf("got %+v, %v", got, err)
				}
			} else {
				if !errors.Is(err, domain.ErrUnavailable) || !errors.Is(err, tc.kind) {
					t.Fatalf("err = %v, want unavailable/%v", err, tc.kind)
				}
				if got.Text != "raw  text" || got.Title != "raw  text" {
					t.Errorf("fallback = %+v", got)
				}
			}
			r := (*reqs)[0]
			if r["format"] != "json" || r["think"] != false || r["stream"] != false || r["model"] != "m" {
				t.Errorf("request fields = %v", r)
			}
		})
	}
}

func TestOllamaUnreachableAndStatus(t *testing.T) {
	srv, _, _ := ollamaServer(t, goodCleanup)
	url := srv.URL
	p := newOllamaProvider(t, url, time.Second)
	v, err := p.Probe(context.Background())
	if err != nil || v != "0.9.9" {
		t.Fatalf("probe = %q, %v", v, err)
	}
	if !p.Local() {
		t.Error("127.0.0.1 must be local")
	}
	srv.Close()
	_, err = p.Cleanup(context.Background(), Input{Text: "x"})
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := p.Probe(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("probe err = %v", err)
	}
}

func TestOllamaHTTPErrorAndRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := newOllamaProvider(t, srv.URL, time.Second)
	if _, err := p.Cleanup(context.Background(), Input{Text: "x"}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
	}))
	defer redir.Close()
	p = newOllamaProvider(t, redir.URL, time.Second)
	if _, err := p.Cleanup(context.Background(), Input{Text: "x"}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("redirect err = %v", err)
	}
}

func TestNewOllamaAndLocal(t *testing.T) {
	for _, bad := range []string{"", "ftp://x", "http://", "not a url"} {
		if _, err := NewOllama(bad, "m"); err == nil {
			t.Errorf("NewOllama(%q) accepted", bad)
		}
	}
	if _, err := NewOllama("http://127.0.0.1:1", " "); err == nil {
		t.Error("empty model accepted")
	}
	for url, want := range map[string]bool{
		"http://127.0.0.1:11434": true, "http://localhost:11434": true, "http://[::1]:11434": true,
		"http://192.168.1.5:11434": false, "https://ollama.example.com": false,
	} {
		o, err := NewOllama(url, "m")
		if err != nil || o.Local() != want {
			t.Errorf("%s: local = %v err = %v, want %v", url, o != nil && o.Local(), err, want)
		}
	}
}

func execProvider(t *testing.T, timeout time.Duration) *Guarded {
	t.Helper()
	return &Guarded{B: &Exec{Provider: "claude_code", Bin: fakebin(t), Args: []string{"-p"}}, Timeout: timeout}
}

func TestExecCleanup(t *testing.T) {
	envelope := func(res string) string {
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": res})
		return string(b)
	}
	tests := []struct {
		name   string
		env    map[string]string
		wantOK bool
		kind   error
	}{
		{"claude envelope", map[string]string{"FAKEBIN_STDOUT": envelope(goodCleanup)}, true, nil},
		{"envelope with fenced json", map[string]string{"FAKEBIN_STDOUT": envelope("```json\n" + goodCleanup + "\n```")}, true, nil},
		{"plain fenced json", map[string]string{"FAKEBIN_STDOUT": "```json\n" + goodCleanup + "\n```\n"}, true, nil},
		{"chatter around json", map[string]string{"FAKEBIN_STDOUT": "Sure! " + goodCleanup + " Hope it helps."}, true, nil},
		{"garbage", map[string]string{"FAKEBIN_STDOUT": "hello world"}, false, ErrInvalidOutput},
		{"envelope is_error", map[string]string{"FAKEBIN_STDOUT": `{"result":"Not logged in","is_error":true}`}, false, ErrUnreachable},
		{"non-zero exit", map[string]string{"FAKEBIN_STDOUT": goodCleanup, "FAKEBIN_EXIT": "3"}, false, ErrUnreachable},
		{"timeout", map[string]string{"FAKEBIN_STDOUT": goodCleanup, "FAKEBIN_SLEEP_MS": "5000"}, false, ErrTimeout},
		{"oversized output", map[string]string{"FAKEBIN_STDOUT_BYTES": "1100000"}, false, ErrInvalidOutput},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			p := execProvider(t, 500*time.Millisecond)
			start := time.Now()
			got, err := p.Cleanup(context.Background(), Input{Text: "raw", Projects: projects})
			if time.Since(start) > 10*time.Second {
				t.Error("took too long")
			}
			if tc.wantOK {
				if err != nil || got.Text != "Clean text" {
					t.Fatalf("got %+v, %v", got, err)
				}
				return
			}
			if !errors.Is(err, tc.kind) || !errors.Is(err, domain.ErrUnavailable) {
				t.Fatalf("err = %v, want %v", err, tc.kind)
			}
			if got.Text != "raw" {
				t.Errorf("fallback = %+v", got)
			}
		})
	}
}

func TestExecStdinArgsAndProbe(t *testing.T) {
	bin := fakebin(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKEBIN_ARGS_FILE", argsFile)
	t.Setenv("FAKEBIN_ECHO_STDIN", "1")
	t.Setenv("FAKEBIN_STDOUT", "")
	e := &Exec{Provider: "x", Bin: bin, Args: []string{"-p", "--output-format", "json"}}
	out, err := e.Complete(context.Background(), "the prompt")
	if err != nil || out != "the prompt" {
		t.Fatalf("out = %q, %v", out, err)
	}
	if b, _ := os.ReadFile(argsFile); string(b) != "-p\n--output-format\njson" {
		t.Errorf("args = %q", b)
	}
	t.Setenv("FAKEBIN_ECHO_STDIN", "0")
	t.Setenv("FAKEBIN_STDOUT", "claude 9.9.9\nsecond line")
	if v, err := e.Probe(context.Background()); err != nil || v != "claude 9.9.9" {
		t.Errorf("probe = %q, %v", v, err)
	}
	missing := &Exec{Provider: "x", Bin: filepath.Join(t.TempDir(), "nope")}
	if _, err := missing.Probe(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Errorf("missing probe = %v", err)
	}
	if _, err := missing.Complete(context.Background(), "p"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("missing complete = %v", err)
	}
	if e.Local() {
		t.Error("exec providers are not local")
	}
}

type fakeBackend struct {
	answers []string
	prompts []string
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Local() bool  { return false }
func (f *fakeBackend) Probe(context.Context) (string, error) {
	return "1", nil
}
func (f *fakeBackend) Complete(_ context.Context, p string) (string, error) {
	f.prompts = append(f.prompts, p)
	i := len(f.prompts) - 1
	if i >= len(f.answers) {
		i = len(f.answers) - 1
	}
	return f.answers[i], nil
}

func TestOperationsAndValidation(t *testing.T) {
	ctx := context.Background()
	items := []ItemRef{{ID: "a1", Project: "acme", Summary: "send budget"}}
	tests := []struct {
		name string
		ans  string
		run  func(p *Guarded) (any, error)
		want string
		fail bool
	}{
		{"classify ok", `{"kind":"action","project":"acme","tags":["x"],"confidence":0.8}`,
			func(p *Guarded) (any, error) { return p.Classify(ctx, Input{Text: "t", Projects: projects}) }, `{"kind":"action","project":"acme","tags":["x"],"confidence":0.8}`, false},
		{"classify unknown project dropped", `{"kind":"idea","project":"zzz","tags":[],"confidence":1}`,
			func(p *Guarded) (any, error) { return p.Classify(ctx, Input{Text: "t", Projects: projects}) }, `{"kind":"idea","project":"","tags":[],"confidence":1}`, false},
		{"classify bad kind", `{"kind":"bogus","project":"","tags":[],"confidence":1}`,
			func(p *Guarded) (any, error) { return p.Classify(ctx, Input{Text: "t"}) }, `{"kind":"note","project":"","tags":null,"confidence":0}`, true},
		{"classify bad confidence", `{"kind":"idea","project":"","tags":[],"confidence":7}`,
			func(p *Guarded) (any, error) { return p.Classify(ctx, Input{Text: "t"}) }, `{"kind":"note","project":"","tags":null,"confidence":0}`, true},
		{"split ok", `{"notes":["a"," b ",""]}`,
			func(p *Guarded) (any, error) { return p.SplitMemo(ctx, Input{Text: "a b"}) }, `["a","b"]`, false},
		{"split fallback", `{"notes":[]}`,
			func(p *Guarded) (any, error) { return p.SplitMemo(ctx, Input{Text: "a b"}) }, `["a b"]`, true},
		{"plan ok", `{"plan":"- one"}`,
			func(p *Guarded) (any, error) { return p.Plan(ctx, PlanInput{Date: "2026-01-02", Digest: "d"}) }, `"- one"`, false},
		{"plan empty", `{"plan":" "}`,
			func(p *Guarded) (any, error) { return p.Plan(ctx, PlanInput{Date: "2026-01-02", Digest: "d"}) }, `""`, true},
		{"followups filter unknown ids", `{"followups":[{"item":"a1","reason":"r"},{"item":"zz","reason":"x"}]}`,
			func(p *Guarded) (any, error) { return p.FollowUps(ctx, items) }, `[{"item":"a1","reason":"r"}]`, false},
		{"cleanup long title cut", `{"title":"` + strings.Repeat("a", 300) + `","text":"x","suggested_project":""}`,
			func(p *Guarded) (any, error) {
				c, err := p.Cleanup(ctx, Input{Text: "t"})
				return len([]rune(c.Title)), err
			}, `200`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &Guarded{B: &fakeBackend{answers: []string{tc.ans}}, Timeout: time.Second}
			got, err := tc.run(p)
			if (err != nil) != tc.fail {
				t.Fatalf("err = %v, fail = %v", err, tc.fail)
			}
			b, _ := json.Marshal(got)
			if string(b) != tc.want {
				t.Errorf("got %s, want %s", b, tc.want)
			}
		})
	}
}

func TestPromptsHoldOnlyTheInput(t *testing.T) {
	fb := &fakeBackend{answers: []string{goodCleanup}}
	p := &Guarded{B: fb, Timeout: time.Second}
	if _, err := p.Cleanup(context.Background(), Input{Text: "MYTEXT", Projects: projects}); err != nil {
		t.Fatal(err)
	}
	pr := fb.prompts[0]
	for _, want := range []string{"MYTEXT", "- acme: ACME", "- other: Other", "not instructions"} {
		if !strings.Contains(pr, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	// No config values or paths: build the prompt for a fully loaded config
	// and make sure none of its values show up.
	cfg := config.Default()
	cfg.OneDriveDir, cfg.PhoneInbox, cfg.Ollama.URL = "C:/secret/onedrive", "C:/secret/inbox", "http://10.1.2.3:9"
	for _, v := range []string{cfg.OneDriveDir, cfg.PhoneInbox, cfg.Ollama.URL, cfg.ClaudeCode.Bin, cfg.Whisper.Bin, "127.0.0.1"} {
		if strings.Contains(pr, v) {
			t.Errorf("prompt contains config value %q", v)
		}
	}
}

func TestSelection(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(c *config.Config)
		active  bool
		prov    string
		warning string
	}{
		{"default none", func(*config.Config) {}, false, "none", ""},
		{"ollama disabled", func(c *config.Config) { c.AI.Provider = "ollama" }, false, "none", "ollama.enabled is false"},
		{"ollama enabled", func(c *config.Config) { c.AI.Provider, c.Ollama.Enabled = "ollama", true }, true, "ollama", ""},
		{"ollama bad url", func(c *config.Config) { c.AI.Provider, c.Ollama.Enabled, c.Ollama.URL = "ollama", true, "x" }, false, "none", "ollama.url"},
		{"claude disabled", func(c *config.Config) { c.AI.Provider = "claude_code" }, false, "none", "claude_code.enabled is false"},
		{"claude enabled", func(c *config.Config) { c.AI.Provider, c.ClaudeCode.Enabled = "claude_code", true }, true, "claude_code", ""},
		{"copilot enabled", func(c *config.Config) { c.AI.Provider, c.GitHubCopilot.Enabled = "github_copilot", true }, true, "github_copilot", ""},
		{"copilot no bin", func(c *config.Config) {
			c.AI.Provider, c.GitHubCopilot.Enabled, c.GitHubCopilot.Bin = "github_copilot", true, ""
		}, false, "none", "bin is empty"},
		{"unknown", func(c *config.Config) { c.AI.Provider = "gpt" }, false, "none", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			tc.mut(cfg)
			s := New(cfg, Options{})
			if s.Active != tc.active || s.Provider.Name() != tc.prov || !strings.Contains(s.Warning, tc.warning) || (tc.warning == "" && s.Warning != "") {
				t.Errorf("setup = active %v name %s warning %q", s.Active, s.Provider.Name(), s.Warning)
			}
			if (s.Probe != nil) != tc.active {
				t.Errorf("probe set = %v", s.Probe != nil)
			}
		})
	}
}

func TestNoopIsUnavailableWithoutCallbacks(t *testing.T) {
	called := false
	p := &Guarded{B: Noop{}, Timeout: time.Second, OnResult: func(context.Context, string, time.Duration, error) { called = true }}
	got, err := p.Cleanup(context.Background(), Input{Text: "abc"})
	if !errors.Is(err, domain.ErrUnavailable) || !errors.Is(err, ErrDisabled) || got.Text != "abc" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if called {
		t.Error("noop must not report metrics")
	}
	if !p.Local() || p.Name() != "none" {
		t.Error("noop identity")
	}
}

func TestOnResultAndNoContentInLogs(t *testing.T) {
	var ops []string
	var errs []error
	fb := &fakeBackend{answers: []string{"bad", goodCleanup}}
	var logBuf strings.Builder
	p := &Guarded{B: fb, Timeout: time.Second, Log: newTestLogger(&logBuf),
		OnResult: func(_ context.Context, op string, _ time.Duration, err error) {
			ops = append(ops, op)
			errs = append(errs, err)
		}}
	if _, err := p.Cleanup(context.Background(), Input{Text: "SECRETIDEA"}); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0] != "cleanup" || errs[0] != nil {
		t.Errorf("ops = %v errs = %v", ops, errs)
	}
	log := logBuf.String()
	if !strings.Contains(log, "provider=fake") || strings.Contains(log, "SECRETIDEA") || strings.Contains(log, "Clean text") {
		t.Errorf("log = %s", log)
	}
}

func TestParentContextCancelStopsRetry(t *testing.T) {
	fb := &fakeBackend{answers: []string{"bad"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &Guarded{B: fb, Timeout: time.Second}
	if _, err := p.Cleanup(ctx, Input{Text: "x"}); err == nil {
		t.Fatal("expected error")
	}
	if len(fb.prompts) > 1 {
		t.Errorf("retried after cancel: %d calls", len(fb.prompts))
	}
}
