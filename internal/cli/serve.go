package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/app"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/httpapi"
	"github.com/chris-reynier/stkr/internal/web"
)

func newServeCmd(opts *options) *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local HTTP server and background jobs",
		Long: "Start the HTTP API on 127.0.0.1 and the background jobs (daily backup). " +
			"The server only accepts connections from this computer. Stop it with Ctrl+C.\n\n" +
			"The port comes from the `port` config key (default 7878); --port overrides it for this run. " +
			"--port 0 picks a free port.",
		Example: "  stkr serve\n  stkr serve --port 7999",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			cfg := svc.Config()
			if cmd.Flags().Changed("port") {
				if port < 0 || port > 65535 {
					return usageError{fmt.Errorf("invalid --port %d", port)}
				}
			} else {
				port = cfg.Port
			}
			ln, err := httpapi.Listen(port)
			if err != nil {
				return usageError{fmt.Errorf("cannot listen on 127.0.0.1:%d (is another stkr serve running? choose another port with --port): %w", port, err)}
			}
			realPort := ln.Addr().(*net.TCPAddr).Port

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			lg := slog.Default()
			runner := app.NewRunner(lg)
			runner.Add("daily-backup", time.Hour, func(ctx context.Context) error {
				_, _, err := svc.DailyBackup(ctx)
				return err
			})
			runner.Start(ctx)

			srv := httpapi.New(svc, httpapi.Options{Port: realPort, AllowedOrigins: cfg.AllowedOrigins, Logger: lg})
			if _, err := web.Mount(srv.Mux(), svc, web.Options{Logger: lg}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stkr serving on http://127.0.0.1:%d\n", realPort)
			lg.Info("serve started", "port", realPort)
			err = srv.Serve(ctx, ln)
			stop()
			runner.Wait()
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", config.Default().Port, "port to listen on (default from config)")
	return cmd
}
