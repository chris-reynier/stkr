package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/chris-reynier/stkr/internal/buildinfo"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/mcpserver"
)

func newMCPCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server on stdin/stdout for coding agents",
		Long: "Runs a Model Context Protocol server over stdio so MCP-capable agents (Claude Code, GitHub Copilot in VS Code) " +
			"can read and change STKR data. It uses the same database as the other commands and needs no running `stkr serve`. " +
			"Agents start it themselves; you normally do not run it by hand. Standard output carries only protocol messages; logs go to the log file.\n\n" +
			"See docs/user/mcp.md. Disable with `stkr config set mcp.enabled false`.",
		Example: "  claude mcp add stkr -- stkr mcp",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := opts.service(cmd)
			if err != nil {
				return err
			}
			if !svc.Config().MCP.Enabled {
				return domain.New(domain.ErrUnavailable, "the MCP server is disabled (mcp.enabled is false)")
			}
			err = mcpserver.Run(cmd.Context(), svc, mcpserver.Options{Version: buildinfo.Version})
			if errors.Is(err, io.EOF) || errors.Is(err, cmd.Context().Err()) {
				return nil
			}
			return err
		},
	}
}
