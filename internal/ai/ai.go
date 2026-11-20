// Package ai holds the advisory AI layer. One Provider interface hides the
// back ends (Ollama over HTTP, Claude Code and GitHub Copilot through a local
// program). AI never blocks capture: every failure is an ErrUnavailable and
// callers keep the raw text.
package ai

import (
	"context"
	"errors"
	"fmt"

	"github.com/chris-reynier/stkr/internal/domain"
)

// Limits shared by the back ends and the wrapper.
const (
	// MaxOutputBytes caps what a provider may answer with.
	MaxOutputBytes = 1 << 20
	// MaxInputRunes caps the text put into one prompt.
	MaxInputRunes = 10000
	// MaxTitleRunes caps a generated title.
	MaxTitleRunes = 200
)

// Project is a project the model may suggest. Only the id and the label are
// ever sent to a provider.
type Project struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Input is the text to work on and the projects it may be filed under.
type Input struct {
	Text     string
	Projects []Project
}

// Classification is the advisory kind and project of a text.
type Classification struct {
	Kind       string   `json:"kind"`
	Project    string   `json:"project"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// Cleaned is a tidied-up capture.
type Cleaned struct {
	Title            string `json:"title"`
	Text             string `json:"text"`
	SuggestedProject string `json:"suggested_project"`
}

// ItemRef names an item for follow-up suggestions.
type ItemRef struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Summary string `json:"summary"`
}

// FollowUp suggests chasing one item.
type FollowUp struct {
	ItemRef string `json:"item"`
	Reason  string `json:"reason"`
}

// PlanInput is the already privacy-filtered digest of the day.
type PlanInput struct {
	Date   string
	Digest string
}

// Provider is the runtime AI interface. Every method returns a usable
// fallback value together with an error that matches domain.ErrUnavailable
// when the provider could not answer.
type Provider interface {
	Name() string
	// Local reports whether prompts stay on this computer.
	Local() bool
	Classify(ctx context.Context, in Input) (Classification, error)
	Cleanup(ctx context.Context, in Input) (Cleaned, error)
	SplitMemo(ctx context.Context, in Input) ([]string, error)
	Plan(ctx context.Context, in PlanInput) (string, error)
	FollowUps(ctx context.Context, items []ItemRef) ([]FollowUp, error)
}

// Backend is the transport behind a Provider: it sends one prompt and returns
// the model's JSON text.
type Backend interface {
	Name() string
	Local() bool
	// Complete returns the raw answer text with any envelope removed.
	Complete(ctx context.Context, prompt string) (string, error)
	// Probe checks that the provider is reachable and returns its version.
	Probe(ctx context.Context) (version string, err error)
}

// Failure kinds, matched with errors.Is.
var (
	// ErrUnreachable means the provider itself is not usable right now (not
	// running, not installed, rejected the call). Retrying later may help.
	ErrUnreachable = errors.New("provider unreachable")
	// ErrInvalidOutput means the answer was not the expected JSON.
	ErrInvalidOutput = errors.New("invalid provider output")
	// ErrTimeout means the provider did not answer in time.
	ErrTimeout = errors.New("provider timed out")
	// ErrDisabled means no provider is active.
	ErrDisabled = errors.New("no AI provider is active")
)

// Error is the failure of one provider operation. It never carries prompt or
// response text.
type Error struct {
	Provider string
	Op       string
	Kind     error
	Msg      string
}

func (e *Error) Error() string {
	s := fmt.Sprintf("ai %s %s: %v", e.Provider, e.Op, e.Kind)
	if e.Msg != "" {
		s += " (" + e.Msg + ")"
	}
	return s
}

// Unwrap lets errors.Is match both domain.ErrUnavailable and the kind.
func (e *Error) Unwrap() []error { return []error{domain.ErrUnavailable, e.Kind} }
