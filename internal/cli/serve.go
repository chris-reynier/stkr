package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/server"
)

func newServeCmd(opts *options) *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local HTTP server and background jobs",
		Long: "Start the HTTP API on 127.0.0.1 and the background jobs (daily backup, idle agent sweeper, close reminder). " +
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
			if cmd.Flags().Changed("port") {
				if port < 0 || port > 65535 {
					return usageError{fmt.Errorf("invalid --port %d", port)}
				}
			} else {
				port = svc.Config().Port
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			h, err := server.Start(ctx, svc, server.Options{Port: port, Logger: slog.Default(), HostedBy: server.HostedByServe})
			var le *server.ListenError
			if errors.As(err, &le) {
				return usageError{fmt.Errorf("cannot listen on 127.0.0.1:%d (is another stkr serve running? choose another port with --port): %w", port, le.Err)}
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stkr serving on %s\n", h.URL())
			<-h.Done()
			return h.Err()
		},
	}
	cmd.Flags().IntVar(&port, "port", config.Default().Port, "port to listen on (default from config)")
	return cmd
}
