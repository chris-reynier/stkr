package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/legacy"
)

func newBackupCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Back up the database now and print the file path",
		Long: "Write a consistent copy of the database to the backups folder and print its path. " +
			"The newest 14 backups are kept. To restore, stop STKR and copy a backup over stkr.db.",
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			p, err := svc.Backup(cmd.Context())
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), map[string]string{"path": p})
			}
			fmt.Fprintln(cmd.OutOrStdout(), p)
			return nil
		},
	}
}

func newImportLegacyCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "import-legacy <path>",
		Short: "Archive files from the earlier Python version (copy only)",
		Long: "Copy the known legacy files (" + fmt.Sprint(legacy.KnownFiles) + ") found in <path> into " +
			"backups/legacy-<timestamp>/. The files are only copied for reference: they are not parsed, " +
			"the originals are not changed and the database is not opened.",
		Example: "  stkr import-legacy ~/old-tool-data",
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.resolveHome()
			if err != nil {
				return err
			}
			res, err := legacy.Archive(args[0], filepath.Join(home, "backups"), clock.Real{}.Now())
			if err != nil {
				return err
			}
			if opts.json {
				return printJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "archived %d file(s) to %s\n", len(res.Files), res.Dir)
			for _, f := range res.Files {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", f)
			}
			return nil
		},
	}
}
