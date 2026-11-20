# Web UI

## What it does

`stkr serve` also serves a small web interface in your browser. It shows and edits your time, and exports a timesheet file. It works offline, needs no login (it only listens on your own computer) and sends nothing to the internet. Every page also works with JavaScript turned off.

Pages:

- **Today** shows the timeline of a day: each event with its start, end, duration and note. Blocks that are too long without a switch are flagged "idle-suspect" and do not count until you confirm them. Below are the gaps in your working hours and the totals per context. You can switch or stop tracking from the top of the page, and move to other days with the links.
- **Week** shows hours per context and day, the hours per ticket (rounded for export), contexts that have time but no ticket, and the Tempo CSV download.
- **Today** also has the Start Day and Close Day buttons, the top 3 of the day, an import box for the AI answer and the items of the day. See [Daily loop](daily-loop.md).
- **Agents** lists your coding agent windows with their state and context, with Go and Queue buttons and a list of folders that match no context. See [Agents](agents.md).
- **Ideas** lists captured ideas with filters and inline triage, and has a capture form and a voice record button. See [Capture](capture.md).
- **Links** saves, searches, pins and deletes bookmarks. See [Bookmarks](bookmarks.md).
- **Settings** edits contexts (label, kind, Tempo ticket, notebook URL, working folder, park or activate) and the configuration (time, paths, AI provider, Claude Code, GitHub Copilot, Ollama, MCP). It also has a **Test provider** button for the AI provider (see [AI providers](ai-providers.md)).

## How to use

```
stkr serve
```

Open <http://127.0.0.1:7878/ui/today> (use the port you configured).

On Today, open **Edit** on a row to change its start time, context or note. In the same box, **Split at** cuts the block in two at a time between its start and the next event (or now). **Merge with next** joins a block with the one after it. **Delete** removes an event, and the block before it grows over the time. **Confirm worked** counts an idle-suspect block. Use the day links to fix earlier days.

On Week, **Download Tempo CSV** saves the file for that week. From a terminal:

```
stkr week 2026-W47 --csv > week.csv
curl -o week.csv http://127.0.0.1:7878/api/week/2026-W47/tempo.csv
```

The columns and rounding rules are in [the Tempo CSV contract](../contracts/tempo-csv.md). Set the ticket of each context in Settings; contexts without a ticket are not exported and are listed on the Week page.

## Configuration keys

Settings saves to `config.yaml`. The running server keeps its old values until you restart `stkr serve`. Settings does not edit `port` or `allowed_origins`; use `stkr config set` for those. The keys are described in [Configuration](configuration.md). The ones that matter most here:

| Key | Effect |
|---|---|
| `rounding_hours` | Exported hours are rounded to a multiple of this (default 0.25). |
| `idle_gap_minutes` | Longer blocks are idle-suspect until confirmed. |
| `day_start`, `day_end` | Working hours, used for gaps. |

## Troubleshooting

- **The page does not load**: check that `stkr serve` is running and that the address uses the right port. Browsers on another computer cannot connect.
- **"split time must be after the event and before the next event and now"**: pick a time inside the block. A block cannot be split in the future.
- **A context is missing from the CSV**: it has no Tempo ticket, or it is a personal or system context. Check the "Unmapped contexts" list on Week.
- **Settings changes do not apply**: restart `stkr serve`.
- **Excel shows odd characters**: the file is UTF-8 without a byte order mark. Import it with "UTF-8" selected, or open it from Excel's Data menu.
