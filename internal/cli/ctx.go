package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// ctxFlags are the editable fields of a context.
type ctxFlags struct {
	label, kind, ticket, notebook, workdir, icon string
	sort                                         int
}

func (f *ctxFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	// --tempo-ticket is accepted as an alias of --ticket.
	fl.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "tempo-ticket" {
			name = "ticket"
		}
		return pflag.NormalizedName(name)
	})
	fl.StringVar(&f.label, "label", "", "display name")
	fl.StringVar(&f.kind, "kind", "", "project, mode, personal or system (personal and system are never exported)")
	fl.StringVar(&f.ticket, "ticket", "", "timesheet ticket key used for exports")
	fl.StringVar(&f.notebook, "notebook", "", "notebook URL to open for this context")
	fl.StringVar(&f.workdir, "workdir", "", "working directory of this context")
	fl.StringVar(&f.icon, "icon", "", "icon name")
	fl.IntVar(&f.sort, "sort", 0, "sort order in lists (lower first)")
}

func newCtxCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ctx",
		Short: "List and manage contexts (what time is tracked against)",
	}
	var all bool
	var kind string
	list := &cobra.Command{
		Use:   "list",
		Short: "List contexts",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			cs, err := svc.ListContexts(cmd.Context(), service.ContextFilter{IncludeParked: all, Kind: domain.ContextKind(kind)})
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), cs)
			}
			tw := newTable(cmd.OutOrStdout())
			row(tw, "ID", "LABEL", "KIND", "TICKET", "STATUS")
			for _, c := range cs {
				status := "active"
				if !c.Active {
					status = "parked"
				}
				row(tw, c.ID, c.Label, string(c.Kind), orDash(c.TempoTicket), status)
			}
			return tw.Flush()
		},
	}
	list.Flags().BoolVar(&all, "all", false, "include parked contexts")
	list.Flags().StringVar(&kind, "kind", "", "only contexts of this kind")

	var addF ctxFlags
	add := &cobra.Command{
		Use:     "add <id>",
		Short:   "Create a context",
		Long:    "Create a context. The id is 1-40 characters a-z, 0-9, '-' or '_'. The label defaults to the id and the kind to project.",
		Example: "  stkr ctx add acme --label \"ACME rollout\" --kind project --ticket ACME-12",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			c, err := svc.AddContext(cmd.Context(), domain.Context{
				ID: args[0], Label: addF.label, Kind: domain.ContextKind(addF.kind), TempoTicket: addF.ticket,
				NotebookURL: addF.notebook, Workdir: addF.workdir, Icon: addF.icon, Sort: addF.sort,
			})
			if err != nil {
				return err
			}
			return printContext(cmd, opts, c, "added")
		},
	}
	addF.register(add)

	var setF ctxFlags
	set := &cobra.Command{
		Use:     "set <id>",
		Short:   "Change fields of a context",
		Long:    "Change the fields given as flags; other fields are kept. Pass an empty value (--ticket \"\") to clear a field.",
		Example: "  stkr ctx set acme --ticket ACME-99",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			str := func(name, v string) *string {
				if fl.Changed(name) {
					return &v
				}
				return nil
			}
			p := service.ContextPatch{
				Label: str("label", setF.label), TempoTicket: str("ticket", setF.ticket),
				NotebookURL: str("notebook", setF.notebook), Workdir: str("workdir", setF.workdir), Icon: str("icon", setF.icon),
			}
			if fl.Changed("kind") {
				k := domain.ContextKind(setF.kind)
				p.Kind = &k
			}
			if fl.Changed("sort") {
				p.Sort = &setF.sort
			}
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			c, err := svc.UpdateContext(cmd.Context(), args[0], p)
			if err != nil {
				return err
			}
			return printContext(cmd, opts, c, "updated")
		},
	}
	setF.register(set)

	cmd.AddCommand(list, add, set,
		activeCmd(opts, "park", "Park a context: hide it and block switching to it (history is kept)", false),
		activeCmd(opts, "activate", "Make a parked context available again", true),
	)
	return cmd
}

func activeCmd(opts *options, name, short string, active bool) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <id>",
		Short: short,
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			if active {
				err = svc.ActivateContext(cmd.Context(), args[0])
			} else {
				err = svc.ParkContext(cmd.Context(), args[0])
			}
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), map[string]any{"id": args[0], "active": active})
			}
			state := "parked"
			if active {
				state = "active"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is %s\n", args[0], state)
			return nil
		},
	}
}

func printContext(cmd *cobra.Command, opts *options, c domain.Context, verb string) error {
	if opts.json {
		return printJSON(cmd.OutOrStdout(), c)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s, %s, ticket %s, sort %s)\n", verb, c.ID, c.Label, c.Kind, orDash(c.TempoTicket), strconv.Itoa(c.Sort))
	return nil
}
