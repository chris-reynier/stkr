package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

// printResult writes the outcome of start-day and close. The prompt goes to
// stdout alone when --print is given, so it can be piped; messages then go to
// stderr.
func printResult(cmd *cobra.Command, opts *options, res service.DayResult, what string, printOut bool) error {
	out := cmd.OutOrStdout()
	if opts.json {
		v := struct {
			service.DayResult
			Prompt string `json:"prompt,omitempty"`
		}{DayResult: res}
		if printOut {
			v.Prompt = res.Prompt
		}
		return printJSON(out, v)
	}
	msg := cmd.ErrOrStderr()
	if printOut {
		fmt.Fprint(out, res.Prompt)
	} else {
		msg = out
	}
	fmt.Fprintf(msg, "%s: %d characters", what, res.Chars)
	switch {
	case res.Pasted:
		fmt.Fprint(msg, ", copied and pasted")
	case res.Clipboard:
		fmt.Fprint(msg, ", copied to the clipboard")
	}
	if res.Exported != "" {
		fmt.Fprintf(msg, ", digest written to %s", res.Exported)
	}
	fmt.Fprintln(msg)
	warn(cmd.ErrOrStderr(), res.Warnings)
	return nil
}

func newStartDayCmd(opts *options) *cobra.Command {
	var noClip, printOut bool
	cmd := &cobra.Command{
		Use:   "start-day",
		Short: "Log planning and copy the Start Day prompt to the clipboard",
		Long: "Switch to the planning context, build a digest of the last five working days " +
			"(at most 2,500 characters), render the Start Day prompt and copy it to the clipboard. " +
			"When onedrive_dir is set, the digest is also written to _daily/daily-digest.md there. " +
			"Paste the prompt into your AI chat, then run `stkr import` on the answer.\n\n" +
			"The prompt template can be customised: see `stkr prompts init`.",
		Example: "  stkr start-day\n  stkr start-day --no-clipboard --print",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.StartDay(cmd.Context(), service.StartDayOptions{Source: domain.SourceCLI, Clipboard: !noClip})
			if err != nil && res.Prompt == "" {
				return err
			}
			if perr := printResult(cmd, opts, res, "Start Day", printOut); perr != nil {
				return perr
			}
			return err // a clipboard failure is reported after the prompt was printed
		},
	}
	cmd.Flags().BoolVar(&noClip, "no-clipboard", false, "do not touch the clipboard")
	cmd.Flags().BoolVar(&printOut, "print", false, "print the prompt to standard output")
	return cmd
}

func newCloseCmd(opts *options) *cobra.Command {
	var noClip, printOut, paste, enter bool
	var window string
	cmd := &cobra.Command{
		Use:   "close",
		Short: "Copy the Close Day prompt to the clipboard (and optionally paste it)",
		Long: "Build the Close Day prompt from today's time, items, ideas, agent state and the morning top 3, " +
			"and copy it to the clipboard. With --paste, also paste it into the AI window whose title " +
			"contains --window (default: the agent_window_title setting); this works on Windows only. " +
			"Then run `stkr import` on the answer.",
		Example: "  stkr close\n  stkr close --paste\n  stkr close --paste --window \"Claude\" --enter",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			res, err := svc.ClosePrompt(cmd.Context(), service.CloseOptions{Clipboard: !noClip, Paste: paste, Window: window, Enter: enter})
			if err != nil && res.Prompt == "" {
				return err
			}
			if perr := printResult(cmd, opts, res, "Close Day", printOut); perr != nil {
				return perr
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&noClip, "no-clipboard", false, "do not touch the clipboard (ignored with --paste)")
	cmd.Flags().BoolVar(&printOut, "print", false, "print the prompt to standard output")
	cmd.Flags().BoolVar(&paste, "paste", false, "paste the prompt into the AI window (Windows)")
	cmd.Flags().StringVar(&window, "window", "", "part of the window title to paste into (default: agent_window_title)")
	cmd.Flags().BoolVar(&enter, "enter", false, "press Enter after pasting")
	return cmd
}

func newImportCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "import [file|-]",
		Short: "Import the AI answer (stkr-brief / stkr-close block) from the clipboard, a file or stdin",
		Long: "Read the answer of your AI chat and store it: items are added (never twice), the top 3 and " +
			"the saved flags of today are updated, and the project logs under onedrive_dir are rewritten.\n\n" +
			"Without an argument the text comes from the clipboard; `-` reads standard input. " +
			"The text must contain a ```stkr-brief v1 or ```stkr-close v1 fenced block; plain " +
			"`top | ...` / `item | ...` / `done | ...` lines are accepted with a warning. " +
			"When nothing usable is found, nothing is written and the command exits with code 2.",
		Example: "  stkr import\n  stkr import answer.txt\n  cat answer.txt | stkr import -",
		Args:    maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			var res service.ImportResult
			if len(args) == 0 {
				res, err = svc.ImportClipboard(cmd.Context())
			} else {
				var text string
				if text, err = readInput(cmd.InOrStdin(), args[0]); err != nil {
					return err
				}
				res, err = svc.Import(cmd.Context(), text, service.ViaCLI)
			}
			var nb *service.NoValidBlockError
			switch {
			case err == nil:
			case errors.As(err, &nb):
				res = nb.Result
			default:
				return err
			}
			if opts.json {
				if perr := printJSON(cmd.OutOrStdout(), res); perr != nil {
					return perr
				}
			} else {
				if err == nil {
					fmt.Fprintln(cmd.OutOrStdout(), res.Summary())
					for _, f := range res.Exported {
						fmt.Fprintf(cmd.OutOrStdout(), "exported %s\n", f)
					}
				}
				warn(cmd.ErrOrStderr(), res.Warnings)
			}
			if nb != nil {
				return fmt.Errorf("%w; paste the text into the Import box at http://127.0.0.1:%d%s instead", nb, svc.Config().Port, service.ImportRedirect)
			}
			return err
		},
	}
}

// readInput reads the import text from stdin ("-") or a file, bounded.
func readInput(stdin io.Reader, name string) (string, error) {
	const limit = 256<<10 + 1
	r := stdin
	if name != "-" {
		f, err := os.Open(name) //nolint:gosec // the user names the file to import
		if err != nil {
			return "", fmt.Errorf("open input: %w", err)
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return string(b), nil
}

func newPromptsCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Manage the Start Day and Close Day prompt templates",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Copy the default prompt templates to ~/.stkr/prompts for editing",
		Long: "Copy start-day.md.tmpl and close-day.md.tmpl into the prompts folder of the data directory. " +
			"A file there replaces the built-in template. Existing files are kept unless --force is given. " +
			"Delete a file to go back to the default.",
		Example: "  stkr prompts init",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := opts.resolveHome()
			if err != nil {
				return err
			}
			dir := filepath.Join(home, "prompts")
			written, err := daily.InitPrompts(dir, force)
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), map[string]any{"dir": dir, "written": written})
			}
			if len(written) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Nothing to do: the templates already exist in %s (use --force to overwrite).\n", dir)
				return nil
			}
			for _, p := range written {
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", p)
			}
			return nil
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite existing templates")
	cmd.AddCommand(initCmd)
	return cmd
}
