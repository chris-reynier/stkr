# Daily loop: Start Day and Close Day

## What it does

STKR helps you plan the day and close it with your AI chat, without retyping anything:

1. **Start Day** logs `planning`, builds a short digest of your recent work and puts a ready-made prompt on the clipboard.
2. You paste the prompt into your AI chat (Claude, Copilot, anything). The chat answers with a small block of lines.
3. **Import** reads that answer: your top 3 priorities and any new actions, decisions, follow-ups, notes and risks are saved. Nothing is added twice.
4. **Close Day** does the same at the end of the day and lets the AI mark items done.
5. STKR rewrites a log file per project in your OneDrive folder (or any folder you choose), and reminds you if you forget to close the day.

Nothing leaves your computer except what you paste into your own chat (and the short plan request, if you turn on `ai.plan_start_day`; see [AI providers](ai-providers.md)). The digest never includes personal or system contexts, and personal ideas are left out.

The whole loop takes under three minutes: press Start Day, paste, copy the answer, press Import.

## How to use

### First setup

```
stkr config set onedrive_dir "C:\Users\you\OneDrive\Projects"   # optional, enables log files
stkr config set agent_window_title "Claude"                    # optional, enables auto-paste
```

### Start Day

```
stkr start-day
```

The Start Day prompt is now on your clipboard. Paste it into your AI chat. The answer ends with a block like this:

````text
```stkr-brief v1
top | 1 | Ship the import feature
top | 2 | Review the open pull requests
item | action | alpha | Write the user guide | 2026-11-20
```
````

Copy the answer (the whole message is fine) and import it:

```
stkr import
```

Without arguments `import` reads the clipboard. You can also use a file (`stkr import answer.txt`) or standard input (`stkr import -`), or paste the answer into the **Import** box on the Today page of the web UI.

The digest holds at most 2,500 characters: the last five working days with time per context (top six), yesterday's top 3, open items, untriaged ideas (count and five titles), agents that need you, timesheet gaps and days you never closed. When there is too much, the least important parts are cut first, in this order: time per day, ideas, gaps, agents, open items, yesterday's top 3, unclosed days.

To see the prompt without touching the clipboard:

```
stkr start-day --no-clipboard --print
```

### Close Day

```
stkr close            # copy the Close Day prompt
stkr close --paste    # also paste it into the AI window (Windows)
```

The prompt lists today's time, items, ideas, agents and your morning top 3, and asks for a `stkr-close v1` block:

````text
```stkr-close v1
item | followup | alpha | Call Bob about the contract | 2026-11-18
done | 0123456789abcdef
```
````

`done` marks an item finished. Use the fingerprint shown after `#` in the prompt, or the item's summary. Then run `stkr import` again. Once a close is imported the day counts as closed.

### Reminder

After `close_reminder` (default 16:45) on a working day, if the close was not imported, the Close key on the Stream Deck blinks yellow and the Today page shows a notice. A day you never closed is listed at the top of the next Start Day digest. A working day is a weekday with tracked time.

### Stream Deck keys

| Key | Press | Image |
|---|---|---|
| Start Day | `POST /api/day/start?src=deck` | none |
| Close Day | `POST /api/close/prompt?paste=1&src=deck` | `/img/key/close.png` |
| Import | `POST /api/import/clipboard` | `/img/key/import.png`: green, yellow or red for 10 seconds |

URLs, request details and images are in [the Stream Deck page](stream-deck.md) and [the key contract](../contracts/streamdeck-keys.md). The keys need `stkr serve` to be running.

### Project log files

When `onedrive_dir` is set, every import rewrites the log of the projects it touched:

- `<onedrive_dir>/<project>/log.md` for each project context.
- `<onedrive_dir>/_daily/inbox.md` for the `other` context and for projects that match no context.
- `<onedrive_dir>/_daily/daily-digest.md`: the last Start Day digest.

STKR rewrites only the `## Current focus` and `## Log` sections. The header, your `## Notes` section and any other section you add are never changed. Details: [the export contract](../contracts/onedrive-export.md).

### Customise the prompts

```
stkr prompts init
```

copies `start-day.md.tmpl` and `close-day.md.tmpl` to `prompts/` in your data folder. Edit them freely; a file there replaces the built-in one. Delete the file to go back to the default. Templates use Go `text/template` syntax:

| Template | Fields |
|---|---|
| `start-day.md.tmpl` | `{{.Date}}` (YYYY-MM-DD), `{{.Weekday}}`, `{{.Digest}}` |
| `close-day.md.tmpl` | `{{.Date}}`, `{{.Weekday}}`, `{{.Context}}` (today's facts) |

Keep the instruction to answer with a `stkr-brief v1` / `stkr-close v1` block and the line format, or the import will not find the answer. The exact grammar is in [the import contract](../contracts/import-fences.md). A broken template stops the command with a message naming the file.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `close_reminder` | `16:45` | Time after which an unsaved close counts as due. |
| `onedrive_dir` | empty | Folder for the project logs and the digest. Empty turns the export off. |
| `agent_window_title` | empty | Part of the title of your AI window; used by `close --paste` and `/api/paste`. |
| `day_start`, `day_end` | `09:00`, `17:00` | Working hours, used for the timesheet gaps in the digest. |

## Troubleshooting

- **"no clipboard tool found"**: on Linux install `xclip`, `xsel` or `wl-clipboard`. Use `--no-clipboard --print` and copy the output yourself.
- **Import says "nothing was imported"**: the answer has no `stkr-brief v1` or `stkr-close v1` block with valid lines. Ask the AI to answer in the format, or paste it into the Import box on the Today page and fix it by hand. A prompt left on the clipboard imports nothing.
- **Import shows a warning**: some lines were not valid (wrong rank, unknown type, empty summary) and were ignored; the rest was imported. The warnings name the line numbers.
- **"no window title"**: set `agent_window_title` or pass `--window`.
- **`window_not_found`**: the AI window did not appear within 10 seconds. Open it first. Paste works only on Windows and only if STKR is not less privileged than the window.
- **A duplicate item appeared**: items are the same when type, project and summary (ignoring case and spacing) match. A reworded summary is a new item.
- **Log files are not written**: check `onedrive_dir` with `stkr config show`. Export errors are shown as warnings after an import and are logged.
