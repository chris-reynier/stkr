# STKR v0.3.0

STKR can now prepare a review of your previous working day for any assistant you use: Copilot M365, Claude, ChatGPT or a local model. STKR gathers the facts it has, keeps private data out, tells the assistant what evidence it lacks, and imports the reviewed answer back. It needs no account, SDK, sign-in or extra service: the hand-over is copy and paste, and everything stays on your computer.

## Highlights

- **Review context of a day.** `GET /api/review/context` (and `stkr review context`) returns the tracked blocks, gaps, totals, contexts, planned top 3, items, open items, new ideas, agents, local activity, close state and the evidence STKR does not have (`meeting_minutes`, `email_commitments`, and `lock_history` / `application_activity` when no plugin sent any). Without a date it picks the previous working day: yesterday, or the last weekday with tracked time, and says which rule it used.
- **Review prompt.** `stkr review` (or `GET`/`POST /api/review/prompt`) builds a prompt from that context. It asks the assistant to keep facts and assumptions apart, never to invent meetings or emails, to reconcile any notes you paste, and to answer with exactly one `stkr-review v1` block. The template is editable (`stkr prompts init`).
- **Review import.** `stkr import` and the Import key read `stkr-review v1` blocks: `date|type|context|text` records of type achievement, action, decision, risk, gap, follow-up or note, stored as items on the reviewed day. A malformed line rejects its whole block, so a half-copied answer never imports half a review. Unknown types are warned about instead of dropped silently, repeated records are stored once, and records under personal or system contexts stay on your computer and are never exported.
- **Check before importing.** `stkr import --dry-run`, `POST /api/import/preview` and the MCP tool `list_unapproved_review_records` list every record as new, existing or duplicate and write nothing.
- **Local activity evidence.** The Stream Deck plugin can send which kind of application was in front, the workspace name, the lock state (active, idle, locked, break) and the duration: `POST /api/activity/events`. STKR accepts these fields only (a window title, URL or clipboard field is rejected), merges replays and late events deterministically, keeps 90 days, and never lets activity change your tracked time. `stkr activity` shows a day.
- **MCP tools.** `get_review_context`, `report_activity_event`, `get_activity_summary` and `list_unapproved_review_records`, with the same validation and privacy rules as HTTP. The MCP server stays local: stdio only, no network port. The Stream Deck keys do not need it.

## Privacy

- Notes of personal and system time, items under personal and system contexts and personal ideas never appear in the review context, the prompt or the MCP output.
- Activity in personal and system contexts is counted as private time only, without its application or workspace.
- Work folders and agent workspace paths are left out unless you set `review.include_workspace_paths: true`.
- Logs record counts and dates, never record text, prompts or activity content.

## Upgrading

- The database migrates by itself on first start (a backup `backups/pre-migrate-1-<time>.db` is written first). Items get two new types (`achievement`, `gap`), days remember when the close was saved, and a new table holds activity events. Existing data is kept as it is.
- New settings `activity.enabled` (default on), `activity.retention_days` (90) and `review.include_workspace_paths` (off), also on the Settings page under "Daily review".
- `stkr-brief` and `stkr-close` blocks keep working as before. Their `item` lines now also accept the types `achievement` and `gap`. The import result has new fields (`review_blocks`, `blocks_rejected`, `duplicates`, `private`) and `kind` can be `review` or `close+review`.
- No new outbound network calls.

## New and changed interfaces

- Routes: `GET /api/review/context`, `GET`/`POST /api/review/prompt`, `POST /api/activity/events`, `GET /api/activity/summary`, `POST /api/import/preview`.
- Contracts: [review context](../contracts/review-context.md), [activity events](../contracts/activity-events.md), [import fences](../contracts/import-fences.md#review-blocks-stkr-review-v1) (stkr-review v1, preview), [Stream Deck keys](../contracts/streamdeck-keys.md#daily-review-workflow) (review workflow), [MCP tools](../contracts/mcp-tools.md).
- Commands: `stkr review`, `stkr review context`, `stkr activity`, `stkr import --dry-run`.
- User guide: [Daily review](../user/daily-review.md).

## Known limitations

- The Stream Deck plugin side (key definitions, lock and application telemetry, prompt assembly) lives in the plugin. STKR only provides the contracts above.
- `close.saved_at` is only known for closes imported from this version on.
