package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func newAgentsCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List coding agent instances reported by the Stream Deck plugin",
		Long: "List the agent instances STKR knows about, with their state and context. " +
			"Instances arrive from the Stream Deck plugin or hooks through `stkr serve`.",
		Example: "  stkr agents\n  stkr agents next\n  stkr agents next --go",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			l, err := svc.Agents(cmd.Context())
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), l)
			}
			if len(l.Instances) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No agent instances.")
				return nil
			}
			tw := newTable(cmd.OutOrStdout())
			row(tw, "SLOT", "STATE", "LABEL", "CONTEXT", "UPDATED", "SUMMARY")
			for _, a := range l.Instances {
				slot := "-"
				if a.Slot > 0 {
					slot = fmt.Sprint(a.Slot)
				}
				state := string(a.State)
				if a.Queued {
					state += " (queued)"
				}
				row(tw, slot, state, a.Label, orDash(a.ContextID), a.Updated.In(svc.Location()).Format("15:04"), orDash(a.Summary))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			for _, u := range l.Unknown {
				fmt.Fprintf(cmd.OutOrStdout(), "Unknown workspace: %s (%d instance(s)); map it with `stkr ctx set <id> --workdir <path>`\n", u.Workspace, u.Instances)
			}
			return nil
		},
	}
	cmd.AddCommand(newAgentsNextCmd(opts))
	return cmd
}

func newAgentsNextCmd(opts *options) *cobra.Command {
	var goTo bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show (or go to) the agent instance that needs attention next",
		Long: "Show the next instance: error first, then action_required, then done, then the oldest. " +
			"With --go, also log the switch to its context, focus its window (Windows) " +
			"or open its workspace with the launch command, and clear its done/queued flags.",
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			var res service.GoResult
			if goTo {
				res, err = svc.GoNext(cmd.Context(), service.GoOptions{Source: domain.SourceCLI, Budget: 0})
				if err != nil {
					return err
				}
			} else {
				n, err := svc.Next(cmd.Context())
				if err != nil {
					return err
				}
				res = service.GoResult{Instance: n}
			}
			if opts.json {
				if goTo {
					return printJSON(cmd.OutOrStdout(), res)
				}
				return printJSON(cmd.OutOrStdout(), map[string]any{"instance": res.Instance})
			}
			out := cmd.OutOrStdout()
			if res.Instance == nil {
				fmt.Fprintln(out, "No agent instances.")
				return nil
			}
			a := res.Instance
			fmt.Fprintf(out, "%s: %s", a.Label, a.State)
			if a.Summary != "" {
				fmt.Fprintf(out, " - %s", a.Summary)
			}
			fmt.Fprintf(out, " (updated %s)\n", hm(a.Updated.In(svc.Location())))
			if goTo {
				fmt.Fprintf(out, "Result: %s\n", res.Result)
				if res.Message != "" {
					fmt.Fprintln(out, res.Message)
				}
				if res.Result == service.GoFailed {
					return fmt.Errorf("could not reach the agent window")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&goTo, "go", false, "switch to the instance's context and focus its window")
	return cmd
}
