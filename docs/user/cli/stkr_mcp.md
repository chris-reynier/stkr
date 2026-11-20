## stkr mcp

Run the MCP server on stdin/stdout for coding agents

### Synopsis

Runs a Model Context Protocol server over stdio so MCP-capable agents (Claude Code, GitHub Copilot in VS Code) can read and change STKR data. It uses the same database as the other commands and needs no running `stkr serve`. Agents start it themselves; you normally do not run it by hand. Standard output carries only protocol messages; logs go to the log file.

See docs/user/mcp.md. Disable with `stkr config set mcp.enabled false`.

```
stkr mcp [flags]
```

### Examples

```
  claude mcp add stkr -- stkr mcp
```

### Options

```
  -h, --help   help for mcp
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

