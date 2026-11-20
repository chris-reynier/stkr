package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/update"
)

// ExitUpdateAvailable is the exit code of `stkr update --check` when a newer
// release exists (0 = up to date).
const ExitUpdateAvailable = 10

// exitError carries a specific exit code without an error message.
type exitError struct{ code int }

func (e exitError) Error() string { return "" }

type updateOutput struct {
	update.Result
	Applied  bool   `json:"applied,omitempty"`
	Restart  bool   `json:"restart_serve,omitempty"`
	Executed string `json:"executable,omitempty"`
}

func newUpdateCmd(opts *options) *cobra.Command {
	var check, yes, force bool
	var version string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for a new release and update this program",
		Long: "Update stkr from GitHub Releases. The download is verified against the release's checksums.txt (SHA-256); " +
			"on a mismatch nothing is changed.\n\n" +
			"--check only reports: exit code 0 means up to date, 10 means an update is available.\n" +
			"Without --check the new version is installed after you confirm (use --yes in scripts). " +
			"A newer release is never installed over a development build without --force, and older versions only with an explicit --version.",
		Example: "  stkr update --check\n  stkr update\n  stkr update --yes\n  stkr update --version v0.1.0",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()
			out := cmd.OutOrStdout()
			cur := svc.CurrentVersion()

			if check {
				if version != "" {
					return usageError{errors.New("--check and --version cannot be combined")}
				}
				cctx, ccancel := context.WithTimeout(ctx, 30*time.Second)
				defer ccancel()
				res, err := svc.UpdateCheck(cctx)
				if err != nil {
					return err
				}
				if opts.json {
					if err := printJSON(out, updateOutput{Result: res}); err != nil {
						return err
					}
				} else {
					fmt.Fprintf(out, "current: %s\nlatest:  %s\n", res.Current, res.Latest)
					if res.Available {
						fmt.Fprintf(out, "update available: %s\nrelease notes: %s\nrun `stkr update` to install it\n", res.Latest, res.URL)
					} else {
						fmt.Fprintln(out, "stkr is up to date")
					}
				}
				if res.Available {
					return exitError{ExitUpdateAvailable}
				}
				return nil
			}

			if update.IsDev(cur) && !force {
				return usageError{errors.New("this is a development build; refusing to replace it (use --force, or run `stkr update --check`)")}
			}
			u := svc.Updater()
			var rel update.Release
			if version != "" {
				rel, err = u.Find(ctx, version)
			} else {
				rel, err = u.Latest(ctx, svc.Config().Update.Channel)
			}
			if err != nil {
				return err
			}
			if version == "" && update.Compare(rel.Tag, cur) <= 0 {
				if opts.json {
					return printJSON(out, updateOutput{Result: update.Result{Current: cur, Latest: rel.Tag, URL: rel.URL}})
				}
				fmt.Fprintf(out, "stkr %s is up to date (latest: %s)\n", cur, rel.Tag)
				return nil
			}
			if version != "" && update.Compare(rel.Tag, cur) == 0 && !force {
				fmt.Fprintf(out, "stkr is already %s\n", cur)
				return nil
			}
			if !yes {
				if !isTerminal(cmd.InOrStdin()) {
					return usageError{errors.New("not a terminal: pass --yes to install without asking")}
				}
				fmt.Fprintf(out, "Update stkr %s -> %s? Release notes: %s\nInstall? [y/N] ", cur, rel.Tag, rel.URL)
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
					fmt.Fprintln(out, "cancelled")
					return nil
				}
			}
			exe, err := opts.executable()
			if err != nil {
				return fmt.Errorf("locate executable: %w", err)
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			if err := u.Apply(ctx, rel, exe); err != nil {
				return err
			}
			running := serveRunning(ctx, svc)
			if opts.json {
				return printJSON(out, updateOutput{Result: update.Result{Current: cur, Latest: rel.Tag, URL: rel.URL},
					Applied: true, Restart: running, Executed: exe})
			}
			fmt.Fprintf(out, "updated stkr %s -> %s\n", cur, rel.Tag)
			if running {
				fmt.Fprintln(out, "stkr serve is running: restart `stkr serve` to use the new version")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only check; exit 10 if an update is available")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "install without asking")
	cmd.Flags().StringVar(&version, "version", "", "install this release (also allows going back to an older one)")
	cmd.Flags().BoolVar(&force, "force", false, "allow replacing a development build")
	return cmd
}

// serveRunning reports whether a stkr serve answers /healthz on the
// configured port.
func serveRunning(ctx context.Context, svc *service.Service) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/healthz", svc.Config().Port), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// isTerminal reports whether r is an interactive terminal.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func (o *options) executable() (string, error) {
	if o.exePath != "" {
		return o.exePath, nil
	}
	return os.Executable()
}
