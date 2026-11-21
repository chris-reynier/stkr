# STKR v0.1.0

First public release. STKR is a local-first task stack manager in one Go binary: time tracking by context, a local web UI, Stream Deck support over HTTP, a daily planning loop with your AI chat, idea capture, an MCP server for coding agents, optional AI cleanup, and self-update. Everything is stored in `~/.stkr`. There is no telemetry.

## Highlights

- **Time tracking.** `stkr switch`, `stop`, `now`, `day`, `week`. Blocks run from one switch to the next; long blocks without a switch are held back as idle-suspect until you confirm them; gaps in working hours are flagged and never counted. Export to a Tempo CSV rounded to 0.25 h; personal and system contexts are never exported.
- **Web UI** (`stkr serve`, <http://127.0.0.1:7878/ui/today>): edit, split, merge and confirm events; week totals; agents, ideas, links and settings pages. No JavaScript is required.
- **Stream Deck** over plain HTTP: context keys, a live "now" image, Agents key (jump to the coding agent that needs you), Start Day, Close Day (blinks when due), Import, Idea and Bookmark keys. Double presses are safe.
- **Daily loop.** Start Day builds a short digest and prompt for your AI chat; Import reads the answer (top 3, actions, decisions, follow-ups, risks) without duplicates; Close Day does the reverse. Project log files are written to a folder you choose (for example in OneDrive) and your own notes in them are never touched.
- **Capture.** Text and voice ideas (ffmpeg and whisper, optional), a phone inbox folder, meeting actions and decisions, bookmarks.
- **MCP server** (`stkr mcp`, 12 tools): Claude Code and GitHub Copilot can read your day and week, switch context, capture and triage ideas, and report agent state. Personal content is never returned.
- **Optional AI** (Ollama, Claude Code or GitHub Copilot) cleans up ideas and can add a plan to Start Day. Your original text is always kept; personal ideas never go to a cloud provider.
- **Backups and updates.** Daily backups (newest 14 kept), pre-migration copies, `stkr update` with SHA-256 verification, and an update banner.
- Builds for Windows, Linux and macOS (amd64 and arm64), 19 to 20 MB each, `CGO_ENABLED=0`.

Start with [Install](../user/install.md) and the [README quick start](../../README.md).

## Quality evidence

- [Acceptance matrix](acceptance.md): every acceptance criterion with its test or manual procedure.
- [Security review](security-review.md): loopback bind, Host and Origin checks on every route, no content or secrets in logs, subprocess rules, atomic writes, `govulncheck` clean.
- [Performance and size numbers](nfr.md): deck POST p95 about 1 ms, key image p95 about 2 ms, idle working set about 24 MB, startup under 200 ms.

## Known limitations

- **Unsigned binaries.** Windows SmartScreen or endpoint protection may warn about `stkr.exe`. Verify with `checksums.txt`. Signing and package managers (winget, scoop, Homebrew) are planned.
- **Windows desktop features are tested only with fakes in CI.** Window focus, auto-paste into a chat window and real microphone recording have not been exercised against a real desktop by the automated tests. See the manual checks below.
- **Settings need a restart.** Changes made in the Settings page or with `stkr config set` apply to a running `stkr serve` after it is restarted. This includes the AI provider.
- **GitHub Copilot CLI defaults are unverified.** The default arguments follow the Copilot CLI documentation but were not run against an installed Copilot CLI. If they differ, set `github_copilot.args` ([AI providers](../user/ai-providers.md)).
- **VS Code MCP configuration** (`.vscode/mcp.json`) follows the VS Code documentation and has not been verified on every VS Code release.
- **Stream Deck plugin.** STKR ships the HTTP contract and a forwarder sketch; the plugin itself lives in its own repository.
- **Idle memory** is about 24 MB (working set) on Windows; the process also reserves about 51 MB of private address space.
- **No live refresh** on the Agents and Ideas pages; reload to see changes.
- Voice recording needs `stkr serve` running; there is no command-line toggle. Failed AI cleanups are not retried automatically. A day closed without the AI cannot be marked closed by hand (the Close key keeps blinking until an import).
- Jira integration and parsing of data from the earlier Python tool are not included (`stkr import-legacy` only archives the old files).

## Manual verification before you rely on it

The automated suite does not use hardware, a desktop or real accounts. Run these once; the procedures are in the [acceptance matrix](acceptance.md#manual-checks).

1. Stream Deck keys: context switch, double press, Close key blink, Agents key.
2. Plugin keeps working when `stkr serve` is stopped.
3. Window focus (`stkr agents next --go`) and paste (`stkr close --paste`) on Windows.
4. A real Start Day and Close Day with your AI chat, timed (under three minutes each).
5. Voice note with ffmpeg and whisper; the same with whisper missing.
6. MCP in Claude Code and in VS Code Copilot.
7. Ollama, Claude Code and GitHub Copilot with one normal and one personal idea.
8. Running the unsigned `stkr.exe` under your endpoint protection.
9. `stkr update --check` against the published release.

## Upgrade notes

None: this is the first release. Data created by development builds uses the same database layout (migration 1).
