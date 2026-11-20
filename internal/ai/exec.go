package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ProbeTimeout is the budget of `bin --version`.
const ProbeTimeout = 2 * time.Second

// Exec runs a local program (Claude Code, GitHub Copilot CLI): argv is
// Bin plus Args, the prompt goes over stdin, stdout is the answer. No shell
// is involved.
type Exec struct {
	Provider string
	Bin      string
	Args     []string
}

// Name implements Backend.
func (e *Exec) Name() string { return e.Provider }

// Local implements Backend. These programs send the prompt to a cloud service.
func (e *Exec) Local() bool { return false }

// capWriter keeps up to max bytes and cancels the command when exceeded.
type capWriter struct {
	buf    bytes.Buffer
	max    int
	cancel context.CancelFunc
	over   bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		w.over = true
		w.cancel()
		return len(p), nil
	}
	return w.buf.Write(p)
}

// Complete implements Backend.
func (e *Exec) Complete(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(e.Bin) == "" {
		return "", fmt.Errorf("%w: no program configured", ErrUnreachable)
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cctx, e.Bin, e.Args...) //nolint:gosec // program and args are the user's own config
	cmd.Stdin = strings.NewReader(prompt)
	out := &capWriter{max: MaxOutputBytes, cancel: cancel}
	cmd.Stdout = out
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	switch {
	case out.over:
		return "", invalidf("output is larger than 1 MB")
	case ctx.Err() != nil:
		return "", fmt.Errorf("%w: %w", ErrTimeout, ctx.Err())
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("%w: program exited with code %d", ErrUnreachable, ee.ExitCode())
		}
		return "", fmt.Errorf("%w: cannot run the program", ErrUnreachable)
	}
	return unwrapEnvelope(out.buf.String())
}

// unwrapEnvelope returns the model text from the {"result": "..."} output of
// claude --output-format json. Any other output is returned as it is.
func unwrapEnvelope(s string) (string, error) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "{") {
		return s, nil
	}
	var env struct {
		Result  *string `json:"result"`
		IsError bool    `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(t), &env); err != nil || env.Result == nil {
		return s, nil
	}
	if env.IsError {
		return "", fmt.Errorf("%w: the program reported an error", ErrUnreachable)
	}
	return *env.Result, nil
}

// Probe implements Backend by running `bin --version`.
func (e *Exec) Probe(ctx context.Context) (string, error) {
	if strings.TrimSpace(e.Bin) == "" {
		return "", fmt.Errorf("%w: no program configured", ErrUnreachable)
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Bin, "--version") //nolint:gosec // user's own config
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s not found or not working", ErrUnreachable, e.Bin)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line), nil
}
