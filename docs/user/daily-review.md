# Daily review

## What it does

The daily review looks back at your previous working day with the help of an assistant of your choice (Copilot M365, Claude, ChatGPT, a local model). STKR prepares the facts it has, you paste them into the assistant, and you import its answer back. STKR never calls the assistant itself and needs no account or sign-in for this.

The facts are bounded and private by default:

- the tracked blocks, untracked gaps and time per context of that day,
- the items recorded that day, the open items and the new (untriaged) ideas,
- the coding agents that need you,
- local activity sent by the Stream Deck plugin, if you use it: which kind of application was in front and when your session was locked, idle or on a break,
- the evidence STKR does not have: meeting minutes and email commitments always, lock history and application activity when no plugin sent any.

Notes of personal and system time (for example a break), items under personal contexts and personal ideas are never included. Work folders are left out unless you turn them on.

The answer comes back as a `stkr-review v1` block. Its records (achievements, actions, decisions, risks, gaps, follow-ups, notes) become items dated on the reviewed day.

## How to use

### From the command line

1. Build the prompt and copy it to the clipboard:

   ```
   stkr review
   ```

   Without `--date` it reviews the previous working day: yesterday, or the last weekday with tracked time before it (Monday reviews Friday). Pick a day with `--date 2026-11-20`. `--print --no-clipboard` prints the prompt instead.
2. Paste it into your assistant. If you have meeting notes or emails for that day, paste them after the prompt: the assistant reconciles them with your time, but it is told not to invent anything that is not there.
3. Copy the assistant's whole answer and check what it would import:

   ```
   stkr import --dry-run
   ```

   Each record is listed as `new`, `existing` or `duplicate`. Nothing is written.
4. Import it:

   ```
   stkr import
   ```

To see the raw facts as JSON: `stkr review context`. To see the activity of a day: `stkr activity --date yesterday`.

### With the Stream Deck

Use a **Previous Day Review** key that calls `GET http://127.0.0.1:7878/api/review/prompt` and copies the `prompt` field, or `POST http://127.0.0.1:7878/api/review/prompt` to let STKR copy it. A plugin can instead fetch `GET /api/review/context`, add its own telemetry and build its own prompt. Use the **Import** key (`POST /api/import/clipboard`) for the answer. See [Stream Deck](stream-deck.md#daily-review-keys).

### From an MCP agent

Ask the agent to review your previous working day: it calls `get_review_context`, writes the review, checks its block with `list_unapproved_review_records`, and you import it. See [MCP server](mcp.md).

### What gets imported

| Record | Stored as |
|---|---|
| `achievement` | an item marked done |
| `action`, `followup`, `risk`, `gap` | an open item |
| `decision`, `note` | an item |

- A block with a malformed line (missing field, bad or future date) is rejected as a whole, so a half-copied answer never imports half a review. Other blocks in the same text still import.
- A line with an unknown type is skipped with a warning.
- A record that names an unknown context is filed in the inbox log with a warning, so you can fix it on the Today page.
- A record under a personal or system context is stored on this computer but never written to the project log files.
- Importing the same answer twice adds nothing.

### Local activity

The Stream Deck plugin can send activity with `POST /api/activity/events`: application name and category, workspace name, lock state and duration. STKR accepts nothing else: no window titles, URLs, keystrokes, screen or clipboard content. Activity never changes your tracked time; it only appears in the review. It is kept for 90 days. Details: [activity events contract](../contracts/activity-events.md).

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `activity.enabled` | `true` | Accept activity from local clients. `false` makes `POST /api/activity/events` answer `503`. |
| `activity.retention_days` | `90` | Days of activity kept (1-3650). Older activity is deleted and not accepted. |
| `review.include_workspace_paths` | `false` | Add the work folders of contexts (and agent workspaces) to the review context. |

The prompt is the template `review-day.md.tmpl`. Run `stkr prompts init` to copy it to the `prompts` folder of your data directory and edit it there.

## Troubleshooting

| Problem | Fix |
|---|---|
| It reviews the wrong day | Days without any tracked time are skipped. Pass `--date YYYY-MM-DD` (or `?date=` over HTTP). |
| `invalid date` | Use `YYYY-MM-DD`; dates after today are refused. |
| Import says the review block was rejected | A line has fewer than four `|`-separated fields or a bad date. Fix it, or ask the assistant to repeat the block, and import again. Nothing from that block was stored. |
| `503 unavailable` from the activity route | `activity.enabled` is off: `stkr config set activity.enabled true`. |
| `400` from the activity route | The plugin sent a field STKR does not accept (such as a window title) or a path as the workspace. The error names the event number. |
