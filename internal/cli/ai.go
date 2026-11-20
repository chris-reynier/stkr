package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAICmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Check and use the optional AI provider",
		Long: "The AI provider only tidies up captured ideas and can suggest a plan for the day. " +
			"Choose it with `stkr config set ai.provider ollama|claude_code|github_copilot` and enable that provider " +
			"(for example `stkr config set ollama.enabled true`). Restart `stkr serve` after changing it.",
	}
	cmd.AddCommand(newAIStatusCmd(opts), newAITestCmd(opts), newAIRunOnceCmd(opts))
	return cmd
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func newAIStatusCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show the AI provider, whether it is reachable and its version",
		Example: "  stkr ai status\n  stkr ai status --json",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			st, err := svc.AIStatus(cmd.Context())
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), st)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "provider:  %s\n", st.Provider)
			fmt.Fprintf(out, "active:    %s\n", yesNo(st.Active))
			fmt.Fprintf(out, "local:     %s\n", yesNo(st.Local))
			if st.Active {
				fmt.Fprintf(out, "reachable: %s\n", yesNo(st.Reachable))
				if st.Version != "" {
					fmt.Fprintf(out, "version:   %s\n", st.Version)
				}
				if st.Error != "" {
					fmt.Fprintf(out, "problem:   %s\n", st.Error)
				}
			}
			if st.Warning != "" {
				fmt.Fprintf(out, "warning:   %s\n", st.Warning)
			}
			fmt.Fprintf(out, "last ok:   %s\n", orDash(st.LastOK))
			fmt.Fprintf(out, "last fail: %s\n", orDash(st.LastError))
			fmt.Fprintf(out, "pending ideas: %d\n", st.Pending)
			return nil
		},
	}
}

func newAITestCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Send a fixed harmless sample to the AI provider",
		Long: "Sends one fixed sentence (never your data) to the configured provider and reports whether a valid answer came back. " +
			"Exits with code 2 when the provider is not active or did not answer.",
		Example: "  stkr ai test",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.AITest(cmd.Context())
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s answered in %d ms (title: %q).\n", res.Provider, res.Millis, res.Title)
			return nil
		},
	}
}

func newAIRunOnceCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "run-once",
		Short: "Sort pending ideas now",
		Long: "Runs one AI batch: up to 20 pending ideas get a cleaned text, a title and a suggested project. " +
			"Raw text is never changed. Personal ideas are skipped unless the provider runs on this computer.",
		Example: "  stkr ai run-once",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.AIRunOnce(cmd.Context())
			if err != nil && res.Done+res.Failed+res.Skipped+res.Waiting == 0 {
				return err
			}
			if opts.json {
				if jerr := printJSON(cmd.OutOrStdout(), res); jerr != nil {
					return jerr
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "done %d, failed %d, skipped %d, waiting %d\n", res.Done, res.Failed, res.Skipped, res.Waiting)
			return err
		},
	}
}
