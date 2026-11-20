// Package cli implements the stkr command line. Commands stay thin: parse
// flags, call into other packages, render output.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/logx"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// annotationNoHome marks commands that must not touch the data directory.
const annotationNoHome = "stkr/no-home"

type usageError struct{ error }

func (u usageError) Unwrap() error { return u.error }

// options holds the global flags.
type options struct {
	home  string
	debug bool
	json  bool

	closer   io.Closer
	closeSvc func() error
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root, opts := newRoot()
	return run(root, opts, os.Args[1:], os.Stdout, os.Stderr)
}

func run(root *cobra.Command, opts *options, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	opts.teardown()
	if err == nil {
		return ExitOK
	}
	fmt.Fprintf(stderr, "stkr: %v\n", err)
	return exitCode(err)
}

func exitCode(err error) int {
	var u usageError
	switch {
	case errors.As(err, &u),
		errors.Is(err, domain.ErrInvalid),
		errors.Is(err, domain.ErrUnavailable):
		return ExitUsage
	}
	return ExitError
}

// NewRootCmd builds the command tree. It is also used by tools/gendocs.
func NewRootCmd() *cobra.Command {
	root, _ := newRoot()
	return root
}

func newRoot() (*cobra.Command, *options) {
	opts := &options{}
	root := &cobra.Command{
		Use:           "stkr",
		Short:         "Local-first task stack manager",
		Long:          "STKR tracks what you are working on, keeps ideas and follow-ups, and exports time. Everything is stored locally in " + "~/.stkr (or $STKR_HOME).",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return opts.setup(cmd)
		},
	}
	root.DisableAutoGenTag = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.PersistentFlags().StringVar(&opts.home, "home", "", "data directory (default $STKR_HOME or ~/.stkr)")
	root.PersistentFlags().BoolVar(&opts.debug, "debug", false, "write debug logs to stderr")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "machine-readable JSON output")

	root.AddCommand(newVersionCmd(opts), newConfigCmd(opts),
		newSwitchCmd(opts), newStopCmd(opts), newNowCmd(opts), newDayCmd(opts), newWeekCmd(opts),
		newCtxCmd(opts), newBackupCmd(opts), newImportLegacyCmd(opts), newAgentsCmd(opts), newAICmd(opts), newServeCmd(opts),
		newStartDayCmd(opts), newCloseCmd(opts), newImportCmd(opts), newPromptsCmd(opts),
		newIdeaCmd(opts), newIdeasCmd(opts), newBookmarkCmd(opts), newLinksCmd(opts))
	markUnknownAsUsage(root)
	return root, opts
}

// markUnknownAsUsage makes "unknown command" errors exit with the usage code.
func markUnknownAsUsage(root *cobra.Command) {
	root.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usageError{fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())}
		}
		return nil
	}
	root.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
}

// setup prepares the data directory and logging for commands that use it.
func (o *options) setup(cmd *cobra.Command) error {
	if cmd.Annotations[annotationNoHome] == "true" {
		return nil
	}
	home, err := config.ResolveHome(o.home)
	if err != nil {
		return err
	}
	if err := config.EnsureHome(home); err != nil {
		return err
	}
	logger, closer, err := logx.New(home+string(os.PathSeparator)+"logs", o.debug)
	if err != nil {
		return err
	}
	o.closer = closer
	slog.SetDefault(logger)
	return nil
}

func (o *options) teardown() {
	if o.closeSvc != nil {
		_ = o.closeSvc()
		o.closeSvc = nil
	}
	if o.closer != nil {
		_ = o.closer.Close()
		o.closer = nil
	}
}

// resolveHome returns the data directory without creating it.
func (o *options) resolveHome() (string, error) { return config.ResolveHome(o.home) }

// printJSON writes v as indented JSON.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func warn(w io.Writer, warnings []string) {
	for _, m := range warnings {
		fmt.Fprintf(w, "warning: %s\n", strings.TrimSpace(m))
	}
}
