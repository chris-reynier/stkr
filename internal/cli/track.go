package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func newSwitchCmd(opts *options) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "switch <context>",
		Short: "Start working on a context",
		Long: "Record that you are now working on <context>. The previous block ends now. " +
			"Switching to the context that is already active within 2 seconds does nothing, so double clicks are safe. " +
			"The context must exist: see `stkr ctx list` and `stkr ctx add`.",
		Example: "  stkr switch acme\n  stkr switch meeting -n \"weekly sync\"",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.Switch(cmd.Context(), args[0], domain.SourceCLI, note)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), res)
			}
			verb := "switched to"
			if !res.Created {
				verb = "already on"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s since %s\n", verb, res.Event.ContextID, hm(res.Event.TS))
			return nil
		},
	}
	cmd.Flags().StringVarP(&note, "note", "n", "", "short note stored with the switch")
	return cmd
}

func newStopCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop tracking (switch to off)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.Stop(cmd.Context(), domain.SourceCLI)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), res)
			}
			if !res.Created {
				fmt.Fprintln(cmd.OutOrStdout(), "not tracking")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stopped at %s\n", hm(res.Event.TS))
			return nil
		},
	}
}

func newNowCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "now",
		Short: "Show the current context and today's total",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			st, err := svc.Now(cmd.Context())
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), st)
			}
			w := cmd.OutOrStdout()
			if !st.Tracking {
				fmt.Fprintln(w, "not tracking")
			} else {
				since := hm(st.Since)
				if st.Since.Format("2006-01-02") != st.Since.Add(st.Elapsed.D()).Format("2006-01-02") {
					since = st.Since.Format("2006-01-02 15:04")
				}
				flag := ""
				if st.IdleSuspect {
					flag = "  (idle-suspect: switch or confirm to count it)"
				}
				fmt.Fprintf(w, "%s (%s) since %s, %s%s\n", st.Context.ID, st.Context.Label, since, dur(st.Elapsed), flag)
			}
			fmt.Fprintf(w, "today %s\n", dur(st.Today))
			return nil
		},
	}
}

func newDayCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "day [date]",
		Short: "Show the blocks, gaps and totals of a day",
		Long: "Show one day: each block (from an event to the next), idle-suspect blocks, timesheet gaps and totals per context. " +
			"date is YYYY-MM-DD, today (default) or yesterday.",
		Example: "  stkr day\n  stkr day 2026-11-16\n  stkr day confirm 42",
		Args:    maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			date := ""
			if len(args) == 1 {
				date = args[0]
			}
			rep, err := svc.Day(cmd.Context(), date)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), rep)
			}
			printDay(cmd.OutOrStdout(), rep)
			return nil
		},
	}
	var undo bool
	confirm := &cobra.Command{
		Use:   "confirm <event-id>",
		Short: "Confirm an idle-suspect block so it counts",
		Long:  "Mark the block started by <event-id> as worked. Idle-suspect blocks count only once confirmed. Use --undo to remove the mark.",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id < 1 {
				return usageError{fmt.Errorf("invalid event id %q", args[0])}
			}
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			e, err := svc.ConfirmEvent(cmd.Context(), id, !undo)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), e)
			}
			state := "confirmed"
			if undo {
				state = "unconfirmed"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "event %d (%s at %s) %s\n", e.ID, e.ContextID, e.TS.Format("2006-01-02 15:04"), state)
			return nil
		},
	}
	confirm.Flags().BoolVar(&undo, "undo", false, "remove the confirmation")
	cmd.AddCommand(confirm)
	return cmd
}

func printDay(w io.Writer, rep domain.DayReport) {
	kind := "not a working day"
	if rep.WorkingDay {
		kind = "working day"
	}
	fmt.Fprintf(w, "%s %s  %s  hours %s-%s\n", rep.Weekday[:3], rep.Date, kind, hm(rep.WindowStart), hm(rep.WindowEnd))
	type line struct {
		start time.Time
		cols  []string
	}
	var lines []line
	for _, b := range rep.Blocks {
		flag := ""
		switch {
		case b.IdleSuspect && b.Confirmed:
			flag = "confirmed"
		case b.IdleSuspect:
			flag = "idle-suspect (not counted)"
		}
		lines = append(lines, line{b.Start, []string{strconv.FormatInt(b.EventID, 10), hm(b.Start), hm(b.End), dur(b.Duration), b.ContextID, flag}})
	}
	for _, g := range rep.Gaps {
		lines = append(lines, line{g.Start, []string{"-", hm(g.Start), hm(g.End), dur(g.Duration), "-", "gap"}})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].start.Before(lines[j].start) })
	if len(lines) == 0 {
		fmt.Fprintln(w, "no time recorded")
	} else {
		tw := newTable(w)
		row(tw, "EVENT", "START", "END", "TIME", "CONTEXT", "FLAGS")
		for _, l := range lines {
			row(tw, l.cols...)
		}
		_ = tw.Flush()
	}
	fmt.Fprintf(w, "counted %s  idle-suspect %s  gaps %s\n", dur(rep.Counted), dur(rep.IdleSuspect), dur(rep.GapTotal))
	if len(rep.Totals) > 0 {
		tw := newTable(w)
		for _, t := range rep.Totals {
			row(tw, "  "+t.ContextID, dur(t.Counted), string(t.Kind))
		}
		_ = tw.Flush()
	}
	if rep.IdleSuspect > 0 {
		fmt.Fprintln(w, "confirm an idle-suspect block with: stkr day confirm <event-id>")
	}
}

func newWeekCmd(opts *options) *cobra.Command {
	var asCSV, raw bool
	cmd := &cobra.Command{
		Use:   "week [yyyy-Www]",
		Short: "Show counted time per context and day for an ISO week",
		Long: "Show counted time per context for each day of an ISO week (default: the current week). " +
			"--csv prints the Tempo import file: one row per day and ticket, hours rounded to rounding_hours after summing; personal and system contexts are left out, and contexts without a ticket are listed on stderr. With --raw the CSV has unrounded hours per day and context instead.",
		Example: "  stkr week\n  stkr week 2026-W47\n  stkr week 2026-W47 --csv > week.csv",
		Args:    maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			week := ""
			if len(args) == 1 {
				week = args[0]
			}
			rep, err := svc.Week(cmd.Context(), week)
			if err != nil {
				return err
			}
			switch {
			case asCSV && !raw:
				tr := svc.TempoFromWeek(rep)
				if len(tr.Unmapped) > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "not exported, no tempo ticket: %s\n", strings.Join(tr.UnmappedIDs(), ", "))
				}
				return tr.WriteCSV(cmd.OutOrStdout())
			case asCSV:
				return writeWeekCSV(cmd.OutOrStdout(), rep)
			case opts.json:
				return printJSON(cmd.OutOrStdout(), rep)
			}
			printWeek(cmd.OutOrStdout(), rep)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asCSV, "csv", false, "print the Tempo CSV (date,ticket,hours,context,description)")
	cmd.Flags().BoolVar(&raw, "raw", false, "with --csv: unrounded hours per context (date,context,label,tempo_ticket,hours)")
	return cmd
}

func writeWeekCSV(w io.Writer, rep domain.WeekReport) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "context", "label", "tempo_ticket", "hours"})
	for _, r := range service.ExportRows(rep) {
		h := r.Total.Counted.D().Hours()
		_ = cw.Write([]string{r.Date, r.Total.ContextID, csvSafe(r.Total.Label), csvSafe(r.Total.TempoTicket), strconv.FormatFloat(h, 'f', 2, 64)})
	}
	cw.Flush()
	return cw.Error()
}

// csvSafe stops spreadsheet formula injection from user-entered text.
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		return "'" + s
	}
	return s
}

func printWeek(w io.Writer, rep domain.WeekReport) {
	fmt.Fprintf(w, "Week %s (%s to %s)\n", rep.Week, rep.Start, rep.End)
	if len(rep.Totals) == 0 {
		fmt.Fprintln(w, "no time recorded")
		return
	}
	tw := newTable(w)
	head := []string{"CONTEXT"}
	for _, d := range rep.Days {
		head = append(head, d.Weekday[:3])
	}
	row(tw, append(head, "TOTAL")...)
	for _, t := range rep.Totals {
		cols := []string{t.ContextID}
		for _, d := range rep.Days {
			cell := "-"
			for _, dt := range d.Totals {
				if dt.ContextID == t.ContextID {
					cell = dur(dt.Counted)
				}
			}
			cols = append(cols, cell)
		}
		row(tw, append(cols, dur(t.Counted))...)
	}
	cols := []string{"TOTAL"}
	for _, d := range rep.Days {
		cols = append(cols, dur(d.Counted))
	}
	row(tw, append(cols, dur(rep.Counted))...)
	_ = tw.Flush()
	fmt.Fprintf(w, "working days %d  idle-suspect %s  gaps %s\n", rep.WorkingDays, dur(rep.IdleSuspect), dur(rep.GapTotal))
}
