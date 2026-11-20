package ai

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
)

// Setup is the provider chosen by the configuration.
type Setup struct {
	// Provider is never nil (Noop when nothing is active).
	Provider Provider
	// Probe checks reachability and returns the version (nil for Noop).
	Probe func(ctx context.Context) (string, error)
	// Configured is the value of ai.provider.
	Configured string
	// Active is false when the provider is none, disabled or unusable.
	Active bool
	// Warning explains why the configured provider is not active.
	Warning string
}

// Options for New.
type Options struct {
	Logger   *slog.Logger
	Clock    clock.Clock
	OnResult func(ctx context.Context, op string, d time.Duration, err error)
}

// New builds the provider named by cfg.AI.Provider when its enabled flag is
// set. Otherwise it returns the no-op provider with a warning.
func New(cfg *config.Config, o Options) Setup {
	s := Setup{Configured: cfg.AI.Provider}
	var b Backend
	switch cfg.AI.Provider {
	case "none", "":
	case "ollama":
		if !cfg.Ollama.Enabled {
			s.Warning = "ai.provider is ollama but ollama.enabled is false"
			break
		}
		ob, err := NewOllama(cfg.Ollama.URL, cfg.Ollama.Model)
		if err != nil {
			s.Warning = err.Error()
			break
		}
		b = ob
	case "claude_code":
		b, s.Warning = execBackend("claude_code", cfg.ClaudeCode)
	case "github_copilot":
		b, s.Warning = execBackend("github_copilot", cfg.GitHubCopilot)
	default:
		s.Warning = fmt.Sprintf("unknown ai.provider %q", cfg.AI.Provider)
	}
	if b == nil {
		b = Noop{}
	} else {
		s.Active = true
		s.Probe = b.Probe
	}
	s.Provider = &Guarded{
		B: b, Timeout: time.Duration(cfg.AI.TimeoutSeconds) * time.Second,
		Log: o.Logger, Clock: o.Clock, OnResult: o.OnResult,
	}
	return s
}

func execBackend(name string, a config.Agent) (Backend, string) {
	if !a.Enabled {
		return nil, fmt.Sprintf("ai.provider is %s but %s.enabled is false", name, name)
	}
	if a.Bin == "" {
		return nil, name + ".bin is empty"
	}
	return &Exec{Provider: name, Bin: a.Bin, Args: append([]string(nil), a.Args...)}, ""
}
