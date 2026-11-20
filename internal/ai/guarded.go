package ai

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/clock"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

var prompts = template.Must(template.ParseFS(promptFS, "prompts/*.tmpl"))

// Kinds of a Classification.
var Kinds = []string{"idea", "action", "decision", "note", "followup", "risk"}

// Guarded turns a Backend into a Provider. It renders the prompts, applies a
// timeout per attempt, decodes the answer strictly, retries once on invalid
// output or a timeout, and reports every call to OnResult. It logs the
// provider, the operation, the duration and the outcome only.
type Guarded struct {
	B       Backend
	Timeout time.Duration
	Log     *slog.Logger
	Clock   clock.Clock
	// OnResult is called after every operation (success or failure).
	OnResult func(ctx context.Context, op string, d time.Duration, err error)
}

// Name implements Provider.
func (g *Guarded) Name() string { return g.B.Name() }

// Local implements Provider.
func (g *Guarded) Local() bool { return g.B.Local() }

// Probe forwards to the backend.
func (g *Guarded) Probe(ctx context.Context) (string, error) { return g.B.Probe(ctx) }

func (g *Guarded) log() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.New(slog.DiscardHandler)
}

func (g *Guarded) now() time.Time {
	if g.Clock == nil {
		g.Clock = clock.Real{}
	}
	return g.Clock.Now()
}

func (g *Guarded) timeout() time.Duration {
	if g.Timeout <= 0 {
		return 2 * time.Minute
	}
	return g.Timeout
}

func render(name string, data any) (string, error) {
	var b bytes.Buffer
	if err := prompts.ExecuteTemplate(&b, name, data); err != nil {
		return "", fmt.Errorf("render prompt %s: %w", name, err)
	}
	return b.String(), nil
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= MaxInputRunes {
		return s
	}
	return string([]rune(s)[:MaxInputRunes])
}

// call runs one operation with the retry policy and decodes the answer.
func call[T any](ctx context.Context, g *Guarded, op, prompt string, decode func(string) (T, error)) (T, error) {
	var zero T
	start := g.now()
	var kind error
	var detail string
	attempts := 0
	for attempts < 2 {
		attempts++
		actx, cancel := context.WithTimeout(ctx, g.timeout())
		raw, err := g.B.Complete(actx, prompt)
		timedOut := actx.Err() != nil && ctx.Err() == nil
		cancel()
		if err == nil {
			var v T
			if v, err = decode(raw); err == nil {
				g.done(ctx, op, start, attempts, nil)
				return v, nil
			}
		}
		kind, detail = classify(err, timedOut), ""
		if kind == ErrUnreachable {
			detail = strings.TrimPrefix(err.Error(), ErrUnreachable.Error()+": ")
		}
		if ctx.Err() != nil || (kind != ErrInvalidOutput && kind != ErrTimeout) {
			break
		}
	}
	e := &Error{Provider: g.B.Name(), Op: op, Kind: kind, Msg: detail}
	g.done(ctx, op, start, attempts, e)
	return zero, e
}

// classify maps a backend or decode error to one of the failure kinds.
func classify(err error, timedOut bool) error {
	switch {
	case errors.Is(err, ErrInvalidOutput), errors.Is(err, ErrDisabled):
		return unwrapKind(err)
	case timedOut, errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return ErrTimeout
	default:
		return ErrUnreachable
	}
}

func unwrapKind(err error) error {
	if errors.Is(err, ErrDisabled) {
		return ErrDisabled
	}
	return ErrInvalidOutput
}

func (g *Guarded) done(ctx context.Context, op string, start time.Time, attempts int, err error) {
	if errors.Is(err, ErrDisabled) {
		return
	}
	d := g.now().Sub(start)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	g.log().Info("ai call", "provider", g.B.Name(), "op", op, "ms", d.Milliseconds(), "attempts", attempts, "outcome", outcome)
	if g.OnResult != nil {
		g.OnResult(ctx, op, d, err)
	}
}

type promptData struct {
	Text     string
	Projects []Project
}

// Classify implements Provider. The fallback is a plain note.
func (g *Guarded) Classify(ctx context.Context, in Input) (Classification, error) {
	fallback := Classification{Kind: "note"}
	p, err := render("classify.tmpl", promptData{Text: clip(in.Text), Projects: in.Projects})
	if err != nil {
		return fallback, err
	}
	v, err := call(ctx, g, "classify", p, func(s string) (Classification, error) { return decodeClassification(s, in.Projects) })
	if err != nil {
		return fallback, err
	}
	return v, nil
}

// Cleanup implements Provider. The fallback is the raw text with its first
// line as title.
func (g *Guarded) Cleanup(ctx context.Context, in Input) (Cleaned, error) {
	fallback := Cleaned{Text: in.Text, Title: firstLine(in.Text)}
	p, err := render("cleanup.tmpl", promptData{Text: clip(in.Text), Projects: in.Projects})
	if err != nil {
		return fallback, err
	}
	v, err := call(ctx, g, "cleanup", p, func(s string) (Cleaned, error) { return decodeCleaned(s, in.Projects) })
	if err != nil {
		return fallback, err
	}
	return v, nil
}

// SplitMemo implements Provider. The fallback is the whole text as one note.
func (g *Guarded) SplitMemo(ctx context.Context, in Input) ([]string, error) {
	fallback := []string{in.Text}
	p, err := render("split.tmpl", promptData{Text: clip(in.Text)})
	if err != nil {
		return fallback, err
	}
	v, err := call(ctx, g, "split", p, decodeSplit)
	if err != nil {
		return fallback, err
	}
	return v, nil
}

// Plan implements Provider. The fallback is an empty plan.
func (g *Guarded) Plan(ctx context.Context, in PlanInput) (string, error) {
	p, err := render("plan.tmpl", in)
	if err != nil {
		return "", err
	}
	return call(ctx, g, "plan", p, decodePlan)
}

// FollowUps implements Provider. The fallback is no suggestions.
func (g *Guarded) FollowUps(ctx context.Context, items []ItemRef) ([]FollowUp, error) {
	p, err := render("followups.tmpl", struct{ Items []ItemRef }{items})
	if err != nil {
		return nil, err
	}
	return call(ctx, g, "followups", p, func(s string) ([]FollowUp, error) { return decodeFollowUps(s, items) })
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

// Strict decoding.

func decodeStrict(raw string, v any) error {
	dec := json.NewDecoder(strings.NewReader(extractJSON(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalidf("not the expected JSON")
	}
	if dec.More() {
		return invalidf("trailing data")
	}
	return nil
}

// extractJSON strips markdown code fences and any chatter around the object.
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	if !strings.HasPrefix(s, "{") {
		if i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}'); i >= 0 && j > i {
			s = s[i : j+1]
		}
	}
	return s
}

func invalidf(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidOutput, msg) }

func projectOK(id string, projects []Project) bool {
	if id == "" {
		return true
	}
	for _, p := range projects {
		if p.ID == id {
			return true
		}
	}
	return false
}

func decodeClassification(raw string, projects []Project) (Classification, error) {
	var c Classification
	if err := decodeStrict(raw, &c); err != nil {
		return c, err
	}
	known := false
	for _, k := range Kinds {
		known = known || k == c.Kind
	}
	switch {
	case !known:
		return c, invalidf("unknown kind")
	case c.Confidence < 0 || c.Confidence > 1:
		return c, invalidf("confidence out of range")
	case len(c.Tags) > 5:
		return c, invalidf("too many tags")
	}
	if !projectOK(c.Project, projects) {
		c.Project = "" // unknown project: advisory only, drop it
	}
	return c, nil
}

func decodeCleaned(raw string, projects []Project) (Cleaned, error) {
	var c Cleaned
	if err := decodeStrict(raw, &c); err != nil {
		return c, err
	}
	c.Text, c.Title = strings.TrimSpace(c.Text), strings.TrimSpace(c.Title)
	if c.Text == "" {
		return c, invalidf("empty text")
	}
	if c.Title == "" {
		c.Title = firstLine(c.Text)
	}
	if utf8.RuneCountInString(c.Title) > MaxTitleRunes {
		c.Title = string([]rune(c.Title)[:MaxTitleRunes])
	}
	if !projectOK(c.SuggestedProject, projects) {
		c.SuggestedProject = ""
	}
	return c, nil
}

func decodeSplit(raw string) ([]string, error) {
	var v struct {
		Notes []string `json:"notes"`
	}
	if err := decodeStrict(raw, &v); err != nil {
		return nil, err
	}
	var out []string
	for _, n := range v.Notes {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	if len(out) == 0 || len(out) > 50 {
		return nil, invalidf("no notes")
	}
	return out, nil
}

func decodePlan(raw string) (string, error) {
	var v struct {
		Plan string `json:"plan"`
	}
	if err := decodeStrict(raw, &v); err != nil {
		return "", err
	}
	if v.Plan = strings.TrimSpace(v.Plan); v.Plan == "" {
		return "", invalidf("empty plan")
	}
	return v.Plan, nil
}

func decodeFollowUps(raw string, items []ItemRef) ([]FollowUp, error) {
	var v struct {
		FollowUps []FollowUp `json:"followups"`
	}
	if err := decodeStrict(raw, &v); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, it := range items {
		known[it.ID] = true
	}
	var out []FollowUp
	for _, f := range v.FollowUps {
		if known[f.ItemRef] {
			out = append(out, f)
		}
	}
	return out, nil
}
