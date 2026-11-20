package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/ai"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

// fakeAI records every text it is given.
type fakeAI struct {
	mu      sync.Mutex
	local   bool
	texts   []string
	cleanup func(text string) (ai.Cleaned, error)
	plan    string
	planErr error
	block   bool
}

func (f *fakeAI) Name() string { return "fake" }
func (f *fakeAI) Local() bool  { return f.local }
func (f *fakeAI) Classify(context.Context, ai.Input) (ai.Classification, error) {
	return ai.Classification{}, ai.ErrDisabled
}
func (f *fakeAI) SplitMemo(context.Context, ai.Input) ([]string, error) { return nil, ai.ErrDisabled }
func (f *fakeAI) FollowUps(context.Context, []ai.ItemRef) ([]ai.FollowUp, error) {
	return nil, ai.ErrDisabled
}
func (f *fakeAI) Cleanup(_ context.Context, in ai.Input) (ai.Cleaned, error) {
	f.mu.Lock()
	f.texts = append(f.texts, in.Text)
	f.mu.Unlock()
	if f.cleanup != nil {
		return f.cleanup(in.Text)
	}
	return ai.Cleaned{Title: "Title", Text: "clean: " + in.Text, SuggestedProject: "proja"}, nil
}
func (f *fakeAI) Plan(ctx context.Context, in ai.PlanInput) (string, error) {
	f.mu.Lock()
	f.texts = append(f.texts, in.Digest)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.plan, f.planErr
}

func withAI(p ai.Provider) func(*config.Config, *Deps) {
	return func(_ *config.Config, d *Deps) { d.AI = p }
}

func (f *fakeAI) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

func addIdea(t *testing.T, f *capFix, text, tag string) domain.Idea {
	t.Helper()
	res, err := f.svc.AddIdea(context.Background(), IdeaInput{Text: text, Tag: tag})
	if err != nil {
		t.Fatal(err)
	}
	return *res.Idea
}

func TestAIRunOnceCleansIdeas(t *testing.T) {
	fk := &fakeAI{}
	f := newCapFixture(t, withAI(fk))
	f.addProject(t, "proja")
	ctx := context.Background()
	idea := addIdea(t, f, "um so try the new thing", "")
	res, err := f.svc.AIRunOnce(ctx)
	if err != nil || res.Done != 1 || res.Waiting != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	got, _ := f.svc.GetIdea(ctx, idea.ID)
	if got.AIState != domain.AIDone || got.CleanText != "clean: um so try the new thing" || got.Title != "Title" ||
		got.SuggestedProject != "proja" || got.RawText != "um so try the new thing" {
		t.Fatalf("idea = %+v", got)
	}
	// A second run finds nothing to do.
	if res, _ := f.svc.AIRunOnce(ctx); res.Done != 0 || len(fk.seen()) != 1 {
		t.Errorf("second run = %+v, calls %d", res, len(fk.seen()))
	}
	if strings.Contains(f.logs.String(), "try the new thing") {
		t.Error("idea text was logged")
	}
}

func TestAIPrivacyGate(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "cloud", true: "local"}[local], func(t *testing.T) {
			fk := &fakeAI{local: local}
			f := newCapFixture(t, withAI(fk))
			f.addProject(t, "proja")
			ctx := context.Background()
			pro := addIdea(t, f, "work idea", "")
			personal := addIdea(t, f, "dentist appointment", "personal")
			// An idea captured while a personal context is active.
			f.switchTo(t, "2026-11-16 09:30", "break")
			inBreak := addIdea(t, f, "lunch plans", "")
			res, err := f.svc.AIRunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			seen := strings.Join(fk.seen(), "|")
			for _, tc := range []struct {
				idea domain.Idea
				text string
			}{{pro, "work idea"}, {personal, "dentist appointment"}, {inBreak, "lunch plans"}} {
				got, _ := f.svc.GetIdea(ctx, tc.idea.ID)
				sawIt := strings.Contains(seen, tc.text)
				isPro := tc.idea.ID == pro.ID
				switch {
				case local || isPro:
					if !sawIt || got.AIState != domain.AIDone {
						t.Errorf("%s: seen=%v state=%s", tc.text, sawIt, got.AIState)
					}
				default:
					if sawIt || got.AIState != domain.AISkipped || got.CleanText != "" {
						t.Errorf("%s reached a non-local provider: seen=%v state=%s", tc.text, sawIt, got.AIState)
					}
				}
			}
			if !local && res.Skipped != 2 {
				t.Errorf("skipped = %d", res.Skipped)
			}
		})
	}
}

func TestAIFailureKeepsRawText(t *testing.T) {
	fk := &fakeAI{cleanup: func(string) (ai.Cleaned, error) {
		return ai.Cleaned{}, &ai.Error{Provider: "fake", Op: "cleanup", Kind: ai.ErrInvalidOutput}
	}}
	f := newCapFixture(t, withAI(fk))
	idea := addIdea(t, f, "raw words", "")
	res, err := f.svc.AIRunOnce(context.Background())
	if err != nil || res.Failed != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	got, _ := f.svc.GetIdea(context.Background(), idea.ID)
	if got.AIState != domain.AIFailed || got.RawText != "raw words" || got.CleanText != "" {
		t.Fatalf("idea = %+v", got)
	}
}

func TestAIUnreachableLeavesPending(t *testing.T) {
	fk := &fakeAI{cleanup: func(string) (ai.Cleaned, error) {
		return ai.Cleaned{}, &ai.Error{Provider: "fake", Op: "cleanup", Kind: ai.ErrUnreachable}
	}}
	f := newCapFixture(t, withAI(fk))
	a, b := addIdea(t, f, "one", ""), addIdea(t, f, "two", "")
	res, err := f.svc.AIRunOnce(context.Background())
	if !errors.Is(err, domain.ErrUnavailable) || res.Waiting != 2 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if len(fk.seen()) != 1 {
		t.Errorf("provider called %d times, want 1 (batch must stop)", len(fk.seen()))
	}
	for _, id := range []int64{a.ID, b.ID} {
		if g, _ := f.svc.GetIdea(context.Background(), id); g.AIState != domain.AIPending {
			t.Errorf("idea %d state = %s", id, g.AIState)
		}
	}
}

func TestAIBatchSizeAndEmptyText(t *testing.T) {
	fk := &fakeAI{}
	f := newCapFixture(t, withAI(fk))
	for i := 0; i < AIBatchSize+3; i++ {
		addIdea(t, f, "idea", "")
	}
	// An idea still waiting for its transcription has no raw text.
	if _, err := f.st.InsertIdea(context.Background(), domain.Idea{Tag: "pro", Source: "voice", TS: f.clk.Now()}); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.AIRunOnce(context.Background())
	if err != nil || res.Done != AIBatchSize || res.Waiting != 4 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestAIKeepsUserTitle(t *testing.T) {
	fk := &fakeAI{}
	f := newCapFixture(t, withAI(fk))
	idea := addIdea(t, f, "text", "")
	idea.Title = "mine"
	if err := f.st.UpdateIdea(context.Background(), idea); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AIRunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g, _ := f.svc.GetIdea(context.Background(), idea.ID); g.Title != "mine" || g.CleanText == "" {
		t.Fatalf("idea = %+v", g)
	}
}

// With no provider the whole flow works and nothing is called.
func TestAINoopAndCaptureLatency(t *testing.T) {
	f := newCapFixture(t)
	if f.svc.AIActive() {
		t.Fatal("default config must not have an active provider")
	}
	if _, err := f.svc.AIRunOnce(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("run-once err = %v", err)
	}
	if err := f.svc.AIBatchTick(context.Background()); err != nil {
		t.Fatalf("tick err = %v", err)
	}
	if _, err := f.svc.AITest(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("test err = %v", err)
	}
	idea := addIdea(t, f, "still captured", "")
	if g, _ := f.svc.GetIdea(context.Background(), idea.ID); g.AIState != domain.AIPending || g.RawText != "still captured" {
		t.Fatalf("idea = %+v", g)
	}
	st, err := f.svc.AIStatus(context.Background())
	if err != nil || st.Provider != "none" || st.Active || st.Pending != 1 || !st.Local {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

func TestAICaptureNeverWaitsForProvider(t *testing.T) {
	fk := &fakeAI{block: true, cleanup: func(string) (ai.Cleaned, error) {
		time.Sleep(2 * time.Second)
		return ai.Cleaned{Text: "x", Title: "x"}, nil
	}}
	f := newCapFixture(t, withAI(fk))
	start := time.Now()
	for i := 0; i < 5; i++ {
		addIdea(t, f, "quick", "")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("capture took %v with a slow provider", d)
	}
	if len(fk.seen()) != 0 {
		t.Error("capture called the provider")
	}
}

func TestAIDisabledProviderWarning(t *testing.T) {
	f := newCapFixture(t, func(c *config.Config, _ *Deps) { c.AI.Provider = "claude_code" })
	st, err := f.svc.AIStatus(context.Background())
	if err != nil || st.Active || !strings.Contains(st.Warning, "claude_code.enabled is false") {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if f.svc.AIStatusWarning() == "" {
		t.Error("expected a warning")
	}
}

func TestAIOllamaEndToEnd(t *testing.T) {
	var answer = `{"title":"T","text":"Clean","suggested_project":"proja"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			_, _ = io.WriteString(w, `{"version":"1.2.3"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "bad idea") {
			_, _ = io.WriteString(w, `{"message":{"content":"nonsense"}}`)
			return
		}
		out := `{"message":{"content":` + quote(answer) + `}}`
		_, _ = io.WriteString(w, out)
	}))
	defer srv.Close()
	f := newCapFixture(t, func(c *config.Config, _ *Deps) {
		c.AI.Provider, c.Ollama.Enabled, c.Ollama.URL = "ollama", true, srv.URL
	})
	f.addProject(t, "proja")
	ctx := context.Background()
	if !f.svc.AIActive() {
		t.Fatal("provider should be active")
	}
	st, err := f.svc.AIStatus(ctx)
	if err != nil || !st.Reachable || st.Version != "1.2.3" || !st.Local {
		t.Fatalf("status = %+v, %v", st, err)
	}
	tr, err := f.svc.AITest(ctx)
	if err != nil || !tr.OK || tr.Title != "T" {
		t.Fatalf("test = %+v, %v", tr, err)
	}
	good := addIdea(t, f, "good idea", "")
	bad := addIdea(t, f, "bad idea", "")
	res, err := f.svc.AIRunOnce(ctx)
	if err != nil || res.Done != 1 || res.Failed != 1 {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if g, _ := f.svc.GetIdea(ctx, good.ID); g.CleanText != "Clean" || g.SuggestedProject != "proja" {
		t.Errorf("good = %+v", g)
	}
	if g, _ := f.svc.GetIdea(ctx, bad.ID); g.AIState != domain.AIFailed {
		t.Errorf("bad = %+v", g)
	}
	st, _ = f.svc.AIStatus(ctx)
	if st.LastOK == "" || st.LastError == "" || strings.Contains(st.LastError, "bad idea") {
		t.Errorf("metrics = %+v", st)
	}
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func TestStartDayAIPlan(t *testing.T) {
	ctx := context.Background()
	t.Run("added to the digest", func(t *testing.T) {
		fk := &fakeAI{plan: "- do the thing"}
		f := newCapFixture(t, withAI(fk), func(c *config.Config, _ *Deps) { c.AI.PlanStartDay = true })
		res, err := f.svc.StartDay(ctx, StartDayOptions{})
		if err != nil || !strings.Contains(res.Prompt, "## Suggested plan (AI)\n- do the thing") {
			t.Fatalf("res = %+v, %v", res, err)
		}
	})
	t.Run("off by default", func(t *testing.T) {
		fk := &fakeAI{plan: "- do the thing"}
		f := newCapFixture(t, withAI(fk))
		res, err := f.svc.StartDay(ctx, StartDayOptions{})
		if err != nil || strings.Contains(res.Prompt, "do the thing") || len(fk.seen()) != 0 {
			t.Fatalf("res = %+v, %v", res, err)
		}
	})
	t.Run("failure keeps the digest", func(t *testing.T) {
		fk := &fakeAI{planErr: errors.New("boom")}
		f := newCapFixture(t, withAI(fk), func(c *config.Config, _ *Deps) { c.AI.PlanStartDay = true })
		res, err := f.svc.StartDay(ctx, StartDayOptions{})
		if err != nil || res.Prompt == "" || len(res.Warnings) != 1 {
			t.Fatalf("res = %+v, %v", res, err)
		}
	})
	t.Run("slow provider is cut off", func(t *testing.T) {
		old := planBudget
		planBudget = 100 * time.Millisecond
		defer func() { planBudget = old }()
		fk := &fakeAI{block: true}
		f := newCapFixture(t, withAI(fk), func(c *config.Config, _ *Deps) { c.AI.PlanStartDay = true })
		start := time.Now()
		res, err := f.svc.StartDay(ctx, StartDayOptions{})
		if err != nil || res.Prompt == "" || time.Since(start) > 3*time.Second {
			t.Fatalf("res = %+v, %v, took %v", res, err, time.Since(start))
		}
	})
}
