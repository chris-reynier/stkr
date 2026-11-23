# MCP server for coding agents

## What it does

`stkr mcp` lets an MCP-capable agent (Claude Code, GitHub Copilot in VS Code, other MCP clients) work with STKR: see what you are tracking, switch or stop a context, capture and triage ideas, list follow-up items, fetch the Start Day or Close Day prompt, read the review context of a day, check a review answer before you import it, and report the state of agent windows and local activity. It uses the same database as the `stkr` commands, so it needs no running `stkr serve`.

The agent starts `stkr mcp` itself and talks to it over standard input and output. You do not run it by hand.

Tools: `switch_context`, `stop`, `get_now`, `get_day`, `get_week`, `add_idea`, `list_ideas`, `triage_idea`, `list_items`, `get_digest`, `list_contexts`, `report_agent_event`, `get_review_context`, `report_activity_event`, `get_activity_summary`, `list_unapproved_review_records`. The resource `stkr://manifest` explains STKR concepts, the tools and the reply format for Start Day, Close Day and the daily review. The prompts `start_day` and `close_day` give the same text as `get_digest`. All inputs and outputs are in the [MCP tools contract](../contracts/mcp-tools.md).

Privacy: personal ideas, ideas captured while a personal or system context was running, items of personal and system projects, and the notes of personal and system time are never returned to the agent. No tool returns settings, file paths or secrets.

Local only: the server runs as a child process of the agent and talks over standard input and output. It opens no network port and makes no network call of its own. MCP is optional: the Stream Deck review keys use the HTTP API and do not need it.

## How to use

`stkr` must be on your PATH (or use its full path in the examples).

### Claude Code

```
claude mcp add stkr -- stkr mcp
```

This adds the server for the current project. Use `--scope user` to make it available everywhere. The same thing as a JSON file (`.mcp.json` in the project folder):

```json
{
  "mcpServers": {
    "stkr": {
      "command": "stkr",
      "args": ["mcp"]
    }
  }
}
```

Check with `claude mcp list`. The command format was checked against Claude Code 2.1.241; if your version differs, run `claude mcp add --help` and verify.

### VS Code with GitHub Copilot

Create `.vscode/mcp.json` in your workspace (or run **MCP: Open User Configuration** for all workspaces):

```json
{
  "servers": {
    "stkr": {
      "type": "stdio",
      "command": "stkr",
      "args": ["mcp"]
    }
  }
}
```

Then open Copilot Chat in agent mode and enable the `stkr` tools. This format has not been verified against every VS Code release; check the current VS Code documentation if the server does not show up.

### Other data folders

If you use a non-default data folder, pass it with `--home`:

```
claude mcp add stkr -- stkr --home D:\stkr-data mcp
```

### Example prompts

- "What am I tracking right now, and how much have I counted today?"
- "Switch to the `acme` context and note that I am fixing the login bug."
- "Capture an idea: try HTMX for the settings page."
- "List my new ideas and file the ones about Acme under the `acme` project."
- "Show my open action items since Monday."
- "Get the close-day prompt, answer it from what we did in this session, and give me the `stkr-close v1` block so I can import it."
- "I am waiting for your approval: report that this agent session needs action." (the agent calls `report_agent_event` with state `action_required`)
- "Review my previous working day from STKR and give me a `stkr-review v1` block; check it with `list_unapproved_review_records` before showing it to me."

An agent that wants to hand you a Start Day or Close Day reply should end it with the fenced block from the manifest; you then import it with `stkr import` (see [Daily loop](daily-loop.md)).

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `mcp.enabled` | `true` | When `false`, `stkr mcp` refuses to start (exit code 2). |

Time events created through MCP have the source `mcp`.

## Troubleshooting

- **The agent says the server failed to start.** Run `stkr mcp` in a terminal. It should wait silently for input (press Ctrl+C to stop). If it prints an error, fix that first; for example `mcp.enabled` is `false`, or `stkr` is not on the PATH of the agent.
- **No output or garbled output.** Standard output carries only protocol messages. Details are written to the log file in your data folder (`logs/stkr.log`); run with `--debug` to also see them on standard error.
- **Errors in tool results.** They start with a code: `[not_found]` (unknown context or idea), `[invalid]` (bad input or a value over the limits: text 10 KB, `limit` 200), `[conflict]`, `[unavailable]`, or `[internal]` (see the log).
- **"database is locked".** STKR waits up to 5 seconds for another process. Retry; do not run two heavy imports at the same time.
- **The agent cannot see a personal idea.** That is by design.
