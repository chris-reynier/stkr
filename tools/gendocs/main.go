// Command gendocs writes the CLI reference as markdown into docs/user/cli.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra/doc"

	"github.com/chris-reynier/stkr/internal/cli"
)

func main() {
	out := filepath.Join("docs", "user", "cli")
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fail(err)
	}
	// Remove stale pages so deleted commands do not linger.
	old, _ := filepath.Glob(filepath.Join(out, "stkr*.md"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	root := cli.NewRootCmd()
	root.DisableAutoGenTag = true
	if err := doc.GenMarkdownTree(root, out); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gendocs:", err)
	os.Exit(1)
}
