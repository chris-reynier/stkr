package ai

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/config"
)

// TestLiveProviders makes one real call per installed CLI to validate the
// default arguments. It never runs in CI: set STKR_AI_LIVE=1 to enable it.
func TestLiveProviders(t *testing.T) {
	if os.Getenv("STKR_AI_LIVE") != "1" {
		t.Skip("set STKR_AI_LIVE=1 to call real providers")
	}
	cfg := config.Default()
	for name, a := range map[string]config.Agent{"claude_code": cfg.ClaudeCode, "github_copilot": cfg.GitHubCopilot} {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(a.Bin); err != nil {
				t.Skipf("%s is not installed", a.Bin)
			}
			g := &Guarded{B: &Exec{Provider: name, Bin: a.Bin, Args: a.Args}, Timeout: 120 * time.Second}
			v, err := g.Probe(context.Background())
			t.Logf("version: %q err: %v", v, err)
			if err != nil {
				t.Skipf("%s does not run here", a.Bin)
			}
			c, err := g.Cleanup(context.Background(), Input{Text: "remind me to review the quarterly roadmap draft before thursday"})
			if err != nil {
				t.Fatalf("cleanup: %v", err)
			}
			t.Logf("title=%q text=%q", c.Title, c.Text)
		})
	}
}
