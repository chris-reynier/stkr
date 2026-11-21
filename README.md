# STKR

STKR is a local-first task stack manager. It keeps track of what you are working on, collects ideas and follow-ups, and exports your time to a timesheet. It is a single Go binary with a command line, a local web page, a local HTTP API for Stream Deck keys, and an MCP server for coding agents. Everything is stored on your machine in `~/.stkr` (or the folder named by `STKR_HOME`). There is no telemetry.

Status: 0.x, early but complete for daily use. Windows first, with Linux and macOS builds.

## Features

| Feature | Where to read more |
|---|---|
| Switch contexts with one command or key; time blocks, idle and gap detection, day and week reports | [Time tracking](docs/user/time-tracking.md), [Contexts](docs/user/contexts.md) |
| Local web UI: Today timeline with editing, Week totals, Tempo CSV export, Settings | [Web UI](docs/user/web-ui.md) |
| Local HTTP API and Stream Deck keys with live key images (listens on 127.0.0.1 only) | [Server](docs/user/server.md), [Stream Deck](docs/user/stream-deck.md) |
| Windows desktop app (`stkrw.exe`): notification-area icon showing what runs, menu for switch, stop, capture and the daily loop | [Desktop app](docs/user/desktop.md) |
| Agents hub: see all coding-agent windows and jump to the one that needs you | [Agents](docs/user/agents.md) |
| Daily loop: Start Day and Close Day prompts, import of the AI answer, project log files, close reminder | [Daily loop](docs/user/daily-loop.md) |
| Capture: text and voice ideas, phone inbox, meeting actions and decisions, bookmarks | [Capture](docs/user/capture.md), [Bookmarks](docs/user/bookmarks.md) |
| Optional AI cleanup of ideas and a Start Day plan (Ollama, Claude Code or GitHub Copilot) | [AI providers](docs/user/ai-providers.md) |
| MCP server (`stkr mcp`) for Claude Code and GitHub Copilot | [MCP server](docs/user/mcp.md) |
| Daily backups, self-update from GitHub Releases | [Backup and restore](docs/user/backup-restore.md), [Update](docs/user/update.md) |

Everything works without AI, without a Stream Deck and without a network.

## Install

Download the archive for your system from the [releases page](https://github.com/chris-reynier/stkr/releases), unpack it and put `stkr` on your PATH. Details, including the Windows SmartScreen note, are in [Install](docs/user/install.md). Update later with `stkr update` ([Update](docs/user/update.md)).

To build from source you need Go 1.27 or newer: `go build -o stkr ./cmd/stkr`. The Windows desktop app: `go build -ldflags "-H windowsgui" -o stkrw.exe ./cmd/stkrw`.

## Quick start (5 minutes)

1. Check the install: `stkr version`.
2. Start the server and leave it running: `stkr serve` (it prints `http://127.0.0.1:7878`).
3. Open <http://127.0.0.1:7878/ui/today> in your browser.
4. In a second terminal, create a project and switch to it:

   ```
   stkr ctx add acme --label "ACME"
   stkr switch acme
   stkr now
   ```

   Reload the Today page: your running context and its start time are there. Stop with `stkr stop`.
5. Start your day: `stkr start-day --no-clipboard --print` shows the prompt for your AI chat (without `--no-clipboard --print` it goes to the clipboard). Paste the chat's answer back with `stkr import` ([Daily loop](docs/user/daily-loop.md)).

To keep test data away from your real data, set `STKR_HOME` to another folder.

## Documentation

- [User guide](docs/user/index.md)
- [Command reference](docs/user/cli/stkr.md)
- [Architecture](docs/dev/architecture.md), [testing](docs/dev/testing.md), [acceptance matrix](docs/dev/acceptance.md), [security review](docs/dev/security-review.md) and [performance numbers](docs/dev/nfr.md)
- [Interface contracts](docs/contracts/README.md)
- [Release notes](docs/dev/release-notes-v0.1.0.md)
- [Contributing](CONTRIBUTING.md) and [security policy](SECURITY.md)

## License

MIT. See [LICENSE](LICENSE).
