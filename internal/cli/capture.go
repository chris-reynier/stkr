package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func newIdeaCmd(opts *options) *cobra.Command {
	var personal bool
	var typ string
	cmd := &cobra.Command{
		Use:   "idea <text>",
		Short: "Capture an idea, or a meeting action, decision or note",
		Long: "Write an idea down. It is saved first; transcription and AI sorting happen later. " +
			"Use --personal for ideas that must stay out of AI prompts and exports.\n\n" +
			"With --type action or decision the text becomes an item of the current project " +
			"(the last project you tracked before the meeting, else `other`). " +
			"With --type note it is saved as a meeting idea.",
		Example: "  stkr idea \"try htmx swaps\"\n  stkr idea \"dentist\" --personal\n  stkr idea \"send the budget to Sam\" --type action",
		Args:    func(cmd *cobra.Command, args []string) error { return minArgs(1, cmd, args) },
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			tag := ""
			if personal {
				tag = "personal"
			}
			res, err := svc.AddIdea(cmd.Context(), service.IdeaInput{Text: strings.Join(args, " "), Tag: tag, Type: typ, Via: service.ViaCLI})
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), res)
			}
			out := cmd.OutOrStdout()
			switch {
			case res.Item != nil && res.Created:
				fmt.Fprintf(out, "Saved %s #%d for %s.\n", res.Item.Type, res.Item.ID, orDash(res.Item.Project))
			case res.Item != nil:
				fmt.Fprintf(out, "Already saved as %s #%d.\n", res.Item.Type, res.Item.ID)
			default:
				fmt.Fprintf(out, "Saved idea #%d (%s).\n", res.Idea.ID, res.Idea.Tag)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&personal, "personal", false, "tag the idea as personal")
	cmd.Flags().StringVar(&typ, "type", "", "meeting capture: action, decision or note")
	return cmd
}

func minArgs(n int, cmd *cobra.Command, args []string) error {
	if err := cobra.MinimumNArgs(n)(cmd, args); err != nil {
		return usageError{err}
	}
	return nil
}

func newIdeasCmd(opts *options) *cobra.Command {
	var status, tag string
	cmd := &cobra.Command{
		Use:     "ideas",
		Short:   "List captured ideas",
		Long:    "List ideas, newest first. Filter with --status (new, triaged, done, archived) and --tag (pro, personal).",
		Example: "  stkr ideas\n  stkr ideas --status new --tag pro --json",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			list, err := svc.ListIdeas(cmd.Context(), service.IdeaFilter{Status: domain.IdeaStatus(status), Tag: tag})
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), map[string]any{"ideas": list})
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No ideas.")
				return nil
			}
			tw := newTable(cmd.OutOrStdout())
			row(tw, "ID", "WHEN", "TAG", "STATUS", "TEXT")
			for _, i := range list {
				text := i.RawText
				if text == "" && i.AudioPath != "" {
					text = "(voice note, no transcript)"
				}
				row(tw, fmt.Sprint(i.ID), i.TS.Format("2006-01-02 15:04"), i.Tag, string(i.Status), oneLine(text, 70))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only ideas with this status")
	cmd.Flags().StringVar(&tag, "tag", "", "only ideas with this tag (pro or personal)")
	return cmd
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "..."
	}
	return s
}

func newBookmarkCmd(opts *options) *cobra.Command {
	var tags string
	cmd := &cobra.Command{
		Use:   "bookmark <url>",
		Short: "Save a link",
		Long: "Save an http or https link. The page title is read from the page (2 second limit); " +
			"if that fails, or `bookmarks.fetch_title` is off, the URL is the title. A saved URL is reported and exits with code 1.",
		Example: "  stkr bookmark https://go.dev/doc/ --tags go,docs",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			b, err := svc.AddBookmark(cmd.Context(), args[0], []string{tags})
			var dup *service.DuplicateBookmarkError
			if errors.As(err, &dup) {
				if opts.json {
					_ = printJSON(cmd.OutOrStdout(), map[string]any{"error": "conflict", "bookmark": dup.Existing})
				}
				return fmt.Errorf("already saved as #%d: %s", dup.Existing.ID, dup.Existing.Title)
			}
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), b)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved #%d: %s\n", b.ID, b.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	return cmd
}

func newLinksCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "links [query]",
		Short:   "List or search saved links",
		Long:    "List bookmarks, pinned first. A query matches the title, URL and tags, ignoring case.",
		Example: "  stkr links\n  stkr links go",
		Args:    maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			list, err := svc.ListBookmarks(cmd.Context(), q)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), map[string]any{"bookmarks": list})
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No links.")
				return nil
			}
			tw := newTable(cmd.OutOrStdout())
			row(tw, "ID", "PIN", "TITLE", "URL", "TAGS")
			for _, b := range list {
				pin := ""
				if b.Pinned {
					pin = "*"
				}
				row(tw, fmt.Sprint(b.ID), pin, oneLine(b.Title, 50), b.URL, strings.Join(b.Tags, ","))
			}
			return tw.Flush()
		},
	}
}
