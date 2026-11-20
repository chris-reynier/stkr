package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// LaunchTimeout bounds how long the launch command may run.
const LaunchTimeout = 5 * time.Second

type cmdLauncher struct {
	command string
	args    []string
}

// NewLauncher returns a Launcher that runs command with args. The placeholder
// {path} in args is replaced by the workspace folder; when no argument has it,
// the path is appended. No shell is involved.
func NewLauncher(command string, args []string) Launcher {
	return cmdLauncher{command: command, args: args}
}

func (l cmdLauncher) OpenWorkspace(ctx context.Context, path string) error {
	if strings.TrimSpace(l.command) == "" {
		return domain.New(domain.ErrUnavailable, "no launch command configured (config key launch.command)")
	}
	if path == "" || strings.HasPrefix(path, "-") || strings.ContainsRune(path, 0) {
		return domain.New(domain.ErrInvalid, "invalid workspace path")
	}
	argv := make([]string, 0, len(l.args)+1)
	hasPath := false
	for _, a := range l.args {
		if strings.Contains(a, "{path}") {
			hasPath = true
			a = strings.ReplaceAll(a, "{path}", path)
		}
		argv = append(argv, a)
	}
	if !hasPath {
		argv = append(argv, path)
	}
	ctx, cancel := context.WithTimeout(ctx, LaunchTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, l.command, argv...).Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("launch %s: timed out", l.command)
		}
		return fmt.Errorf("launch %s: %w", l.command, err)
	}
	return nil
}
