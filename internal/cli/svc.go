package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// service loads the configuration and opens the database. The service is
// closed in teardown.
func (o *options) service(cmd *cobra.Command) (*service.Service, error) {
	home, err := o.resolveHome()
	if err != nil {
		return nil, err
	}
	cfg, warnings, err := config.Load(config.Path(home))
	if err != nil {
		return nil, err
	}
	warn(cmd.ErrOrStderr(), warnings)
	svc, closeFn, err := app.Open(cmd.Context(), home, cfg, app.Options{Updater: o.updater, Version: o.version})
	if err != nil {
		return nil, err
	}
	o.closeSvc = closeFn
	return svc, nil
}

// exactArgs is cobra.ExactArgs that reports a usage error.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// maxArgs is cobra.MaximumNArgs that reports a usage error.
func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.MaximumNArgs(n)(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

func dur(d domain.Duration) string { return domain.FormatDuration(d.D()) }

func hm(t time.Time) string { return t.Format("15:04") }

func newTable(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0) }

func row(w io.Writer, cols ...string) { fmt.Fprintln(w, strings.Join(cols, "\t")) }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
