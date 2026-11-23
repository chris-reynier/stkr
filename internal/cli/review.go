package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/domain"
)

func newReviewCmd(opts *options) *cobra.Command {
	var date string
	var noClip, printOut bool
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Copy the daily review prompt of the previous working day to the clipboard",
		Long: "Build the review prompt of one working day and copy it to the clipboard. Without --date it is the " +
			"previous working day: yesterday, or the last weekday with tracked time before it. The prompt holds the " +
			"tracked blocks, gaps, totals, open items, new ideas, local activity and the evidence STKR does not have " +
			"(meeting minutes, emails), and asks the AI to answer with one ```stkr-review v1 block. Paste the prompt " +
			"into your assistant, then run `stkr import` on the answer.\n\n" +
			"Personal and system contexts keep their time but never their notes or items.",
		Example: "  stkr review\n  stkr review --date 2026-11-20 --print --no-clipboard\n  stkr review context --json",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.ReviewPrompt(cmd.Context(), date, !noClip)
			if err != nil && res.Prompt == "" {
				return err
			}
			out := cmd.OutOrStdout()
			if opts.json {
				v := res
				if !printOut {
					v.Prompt = ""
				}
				if perr := printJSON(out, v); perr != nil {
					return perr
				}
				return err
			}
			msg := out
			if printOut {
				fmt.Fprint(out, res.Prompt)
				msg = cmd.ErrOrStderr()
			}
			fmt.Fprintf(msg, "Review of %s (%s): %d characters", res.Date, res.Basis, res.Chars)
			if res.Clipboard {
				fmt.Fprint(msg, ", copied to the clipboard")
			}
			fmt.Fprintln(msg)
			return err
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "day to review, YYYY-MM-DD (default: the previous working day)")
	cmd.Flags().BoolVar(&noClip, "no-clipboard", false, "do not touch the clipboard")
	cmd.Flags().BoolVar(&printOut, "print", false, "print the prompt to standard output")

	var ctxDate string
	ctxCmd := &cobra.Command{
		Use:   "context",
		Short: "Print the review metadata of a day as JSON",
		Long: "Print the same JSON as GET /api/review/context: the selected date and why it was chosen, " +
			"tracked blocks, gaps, totals, contexts, open items, new ideas, agents, local activity, close state " +
			"and missing evidence. Nothing is changed.",
		Example: "  stkr review context\n  stkr review context --date 2026-11-20",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			rc, err := svc.ReviewContext(cmd.Context(), ctxDate)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), rc)
		},
	}
	ctxCmd.Flags().StringVar(&ctxDate, "date", "", "day, YYYY-MM-DD (default: the previous working day)")
	cmd.AddCommand(ctxCmd)
	return cmd
}

func newActivityCmd(opts *options) *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Show the local activity evidence of a day",
		Long: "Summarise the activity events a local client (the Stream Deck plugin) sent for one day: time " +
			"per lock state, category, application and workspace, and the locked, idle and break periods. " +
			"Activity in personal and system contexts is counted as private time only.",
		Example: "  stkr activity\n  stkr activity --date yesterday --json",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			sum, err := svc.ActivityDay(cmd.Context(), date)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), sum)
			}
			out := cmd.OutOrStdout()
			if sum.Events == 0 {
				fmt.Fprintf(out, "%s: no activity recorded.\n", sum.Date)
				return nil
			}
			fmt.Fprintf(out, "%s: %d events, %s-%s\n", sum.Date, sum.Events, hm(sum.First), hm(sum.Last))
			fmt.Fprintf(out, "active %s, idle %s, locked %s, break %s, private %s\n",
				dur(sum.Active), dur(sum.Idle), dur(sum.Locked), dur(sum.Break), dur(sum.Private))
			for _, c := range sum.Categories {
				fmt.Fprintf(out, "  %-15s %s\n", c.Name, dur(c.Seconds))
			}
			for _, w := range sum.Workspaces {
				fmt.Fprintf(out, "  workspace %s %s\n", w.Name, dur(w.Seconds))
			}
			for _, p := range sum.Periods {
				fmt.Fprintf(out, "  %s-%s %s (%s)\n", hm(p.Start), hm(p.End), p.State, domain.FormatDuration(p.Duration.D()))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "today", "day: YYYY-MM-DD, today or yesterday")
	return cmd
}
