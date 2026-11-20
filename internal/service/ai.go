package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/ai"
	"github.com/chris-reynier/stkr/internal/capture"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

// AI limits and kv keys.
const (
	// AIBatchSize is the most ideas one batch run sorts.
	AIBatchSize = 20
	// AIPlanBudget is the most time Start Day waits for the AI plan.
	AIPlanBudget = 5 * time.Second
	// MaxAIPlanRunes caps the plan text added to the Start Day prompt.
	MaxAIPlanRunes = 1500

	kvAIOK  = "ai.last_ok"
	kvAIErr = "ai.last_err" // "<RFC3339>|<op>|<error>"

	aiProbeTimeout = 3 * time.Second
	aiTestSample   = "remind me to review the quarterly roadmap draft before thursday"
)

// planBudget is AIPlanBudget; tests shorten it.
var planBudget = AIPlanBudget

type aiState struct {
	setup ai.Setup
	mu    sync.Mutex // one batch at a time
}

func newAIState(s *Service, cfg *config.Config, deps Deps) *aiState {
	if deps.AI != nil {
		return &aiState{setup: ai.Setup{
			Provider: deps.AI, Configured: deps.AI.Name(), Active: true,
			Probe: func(context.Context) (string, error) { return "test", nil },
		}}
	}
	return &aiState{setup: ai.New(cfg, ai.Options{Logger: s.log, Clock: s.clk, OnResult: s.aiResult})}
}

// aiResult records the time of the last successful and failed call in kv.
func (s *Service) aiResult(ctx context.Context, op string, _ time.Duration, err error) {
	ctx = context.WithoutCancel(ctx)
	ts := s.clk.Now().UTC().Format(time.RFC3339)
	if err == nil {
		_ = s.st.SetKV(ctx, kvAIOK, ts)
		return
	}
	_ = s.st.SetKV(ctx, kvAIErr, ts+"|"+op+"|"+err.Error())
}

// AIActive reports whether a provider is configured, enabled and usable.
func (s *Service) AIActive() bool { return s.ai.setup.Active }

// AIStatus describes the AI provider.
type AIStatus struct {
	// Provider is the configured ai.provider.
	Provider string `json:"provider"`
	// Active is false when nothing will be called (none, disabled, unusable).
	Active    bool   `json:"active"`
	Local     bool   `json:"local"`
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	Warning   string `json:"warning,omitempty"`
	Error     string `json:"error,omitempty"`
	LastOK    string `json:"last_ok,omitempty"`
	LastError string `json:"last_error,omitempty"`
	// Pending counts ideas waiting for the AI batch.
	Pending int `json:"pending"`
}

// AIStatus probes the provider (2-3 s budget) and reports it.
func (s *Service) AIStatus(ctx context.Context) (AIStatus, error) {
	set := s.ai.setup
	st := AIStatus{Provider: set.Configured, Active: set.Active, Local: set.Provider.Local(), Warning: set.Warning}
	if st.Provider == "" {
		st.Provider = "none"
	}
	if set.Active && set.Probe != nil {
		pctx, cancel := context.WithTimeout(ctx, aiProbeTimeout)
		v, err := set.Probe(pctx)
		cancel()
		if err != nil {
			st.Error = err.Error()
		} else {
			st.Reachable, st.Version = true, v
		}
	}
	if v, err := s.st.GetKV(ctx, kvAIOK); err == nil {
		st.LastOK = v
	}
	if v, err := s.st.GetKV(ctx, kvAIErr); err == nil {
		st.LastError = strings.ReplaceAll(v, "|", " ")
	}
	pending, err := s.st.ListIdeasByAIState(ctx, domain.AIPending)
	if err != nil {
		return st, err
	}
	st.Pending = len(pending)
	return st, nil
}

// AITestResult is the outcome of AITest.
type AITestResult struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Millis   int64  `json:"ms"`
	// Title is what the provider made of the fixed sample text.
	Title string `json:"title,omitempty"`
}

// AITest sends a fixed harmless sample to the provider. It never sends user
// data.
func (s *Service) AITest(ctx context.Context) (AITestResult, error) {
	set := s.ai.setup
	res := AITestResult{Provider: set.Configured}
	if !set.Active {
		return res, aiUnavailable(set.Warning)
	}
	start := s.clk.Now()
	c, err := set.Provider.Cleanup(ctx, ai.Input{Text: aiTestSample})
	res.Millis = s.clk.Now().Sub(start).Milliseconds()
	if err != nil {
		return res, err
	}
	res.OK, res.Title = true, c.Title
	return res, nil
}

func aiUnavailable(warning string) error {
	msg := "no AI provider is active"
	if warning != "" {
		msg += ": " + warning
	}
	return domain.New(domain.ErrUnavailable, msg)
}

// AIBatch counts what one batch run did.
type AIBatch struct {
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	// Waiting counts pending ideas that were not touched (empty text, still
	// transcribing, or beyond the batch size).
	Waiting int `json:"waiting"`
}

// aiAllowed is the privacy gate: personal ideas and ideas of personal or
// system contexts only go to local providers.
func aiAllowed(p ai.Provider, idea domain.Idea, ctxs map[string]domain.Context) bool {
	if p.Local() {
		return true
	}
	if idea.Tag == capture.TagPersonal {
		return false
	}
	if c, ok := ctxs[idea.ContextID]; ok && (c.Kind == domain.KindPersonal || c.Kind == domain.KindSystem) {
		return false
	}
	return true
}

// aiProjects lists the active project contexts (id and label only).
func aiProjects(ctxs map[string]domain.Context) []ai.Project {
	var out []ai.Project
	for _, c := range ctxs {
		if c.Kind == domain.KindProject && c.Active {
			out = append(out, ai.Project{ID: c.ID, Label: c.Label})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AIRunOnce sorts up to AIBatchSize pending ideas: the cleaned text, a title
// and a suggested project are stored next to the untouched raw text. Ideas
// that must not leave the computer are marked skipped. When the provider is
// unreachable the run stops, leaves the ideas pending and returns the error.
func (s *Service) AIRunOnce(ctx context.Context) (AIBatch, error) {
	set := s.ai.setup
	if !set.Active {
		return AIBatch{}, aiUnavailable(set.Warning)
	}
	s.ai.mu.Lock()
	defer s.ai.mu.Unlock()
	var res AIBatch
	pending, err := s.st.ListIdeasByAIState(ctx, domain.AIPending)
	if err != nil {
		return res, err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return res, err
	}
	projects := aiProjects(ctxs)
	handled := 0
	for _, idea := range pending {
		if strings.TrimSpace(idea.RawText) == "" || handled >= AIBatchSize || ctx.Err() != nil {
			continue
		}
		handled++
		if !aiAllowed(set.Provider, idea, ctxs) {
			if err := s.aiFinish(ctx, idea.ID, domain.AISkipped, nil); err != nil {
				return res, err
			}
			res.Skipped++
			continue
		}
		cleaned, err := set.Provider.Cleanup(ctx, ai.Input{Text: idea.RawText, Projects: projects})
		switch {
		case err == nil:
			if err := s.aiFinish(ctx, idea.ID, domain.AIDone, &cleaned); err != nil {
				return res, err
			}
			res.Done++
		case errors.Is(err, ai.ErrUnreachable):
			res.Waiting = len(pending) - res.Done - res.Failed - res.Skipped
			s.log.Warn("ai batch stopped", "provider", set.Provider.Name(), "done", res.Done)
			return res, err
		default:
			if err := s.aiFinish(ctx, idea.ID, domain.AIFailed, nil); err != nil {
				return res, err
			}
			res.Failed++
		}
	}
	res.Waiting = len(pending) - res.Done - res.Failed - res.Skipped
	s.log.Info("ai batch", "done", res.Done, "failed", res.Failed, "skipped", res.Skipped, "waiting", res.Waiting)
	return res, nil
}

// AIBatchTick is the serve worker job: it runs a batch when a provider is
// active. An unreachable provider is reported as an error for the log.
func (s *Service) AIBatchTick(ctx context.Context) error {
	if !s.AIActive() {
		return nil
	}
	_, err := s.AIRunOnce(ctx)
	return err
}

// aiFinish stores the result of one idea if it is still pending. A title or
// project the user set meanwhile is kept; the raw text is never changed.
func (s *Service) aiFinish(ctx context.Context, id int64, state domain.AIState, c *ai.Cleaned) error {
	cur, err := s.st.GetIdea(ctx, id)
	if err != nil {
		return err
	}
	if cur.AIState != domain.AIPending {
		return nil
	}
	cur.AIState = state
	if c != nil {
		cur.CleanText = c.Text
		if cur.Title == "" {
			cur.Title = c.Title
		}
		if cur.SuggestedProject == "" {
			cur.SuggestedProject = c.SuggestedProject
		}
	}
	return s.st.UpdateIdea(ctx, cur)
}

// withAIPlan appends an AI plan to the digest when ai.plan_start_day is on
// and a provider is active. The digest holds no personal data (it is built
// for prompts), and the call has a 5 s budget: on any failure the digest is
// returned unchanged with a warning.
func (s *Service) withAIPlan(ctx context.Context, date, digest string) (string, string) {
	if !s.cfg.AI.PlanStartDay || !s.AIActive() {
		return digest, ""
	}
	pctx, cancel := context.WithTimeout(ctx, planBudget)
	defer cancel()
	plan, err := s.ai.setup.Provider.Plan(pctx, ai.PlanInput{Date: date, Digest: digest})
	if err != nil || plan == "" {
		s.log.Warn("ai plan skipped", "err", err)
		return digest, "AI plan skipped: the provider did not answer in time"
	}
	if utf8.RuneCountInString(plan) > MaxAIPlanRunes {
		plan = string([]rune(plan)[:MaxAIPlanRunes])
	}
	return digest + "\n\n## Suggested plan (AI)\n" + plan, ""
}

// AIStatusWarning explains why the configured provider is not active ("" when
// it is active or none was asked for).
func (s *Service) AIStatusWarning() string { return s.ai.setup.Warning }
