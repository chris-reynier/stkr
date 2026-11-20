package ai

import "context"

// Noop is the backend used when no provider is active. It sends nothing.
type Noop struct{}

// Name implements Backend.
func (Noop) Name() string { return "none" }

// Local implements Backend. Nothing leaves the computer.
func (Noop) Local() bool { return true }

// Complete implements Backend.
func (Noop) Complete(context.Context, string) (string, error) { return "", ErrDisabled }

// Probe implements Backend.
func (Noop) Probe(context.Context) (string, error) { return "", ErrDisabled }
