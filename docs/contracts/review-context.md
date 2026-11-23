# Review context contract (v1)

The read-only, privacy-bounded metadata of one working day, for an external review. A Stream Deck plugin (or any local client) fetches it, adds its own evidence, builds a prompt for an assistant of the user's choice, and later imports the answer as a [`stkr-review v1`](import-fences.md#review-blocks-stkr-review-v1) block.

STKR calls no assistant and needs no account, SDK or identity configuration. The contract is provider-neutral: Copilot M365, Claude, ChatGPT or a local model are all used the same way, by pasting.

The same data is available as `stkr review context` (CLI) and the MCP tool `get_review_context`.

## Routes

| Route | Side effects | Response |
|---|---|---|
| `GET /api/review/context[?date=YYYY-MM-DD]` | none | the review context (below) |
| `GET /api/review/prompt[?date=YYYY-MM-DD]` | none | `{"date","basis","chars","clipboard":false,"prompt"}` |
| `POST /api/review/prompt[?date=YYYY-MM-DD][&clipboard=0]` | copies the prompt to the clipboard (unless `clipboard=0`) | the same, with `"clipboard":true` |

The prompt is the template `review-day.md.tmpl` (editable after `stkr prompts init`) filled with a text form of the context. It is at most 6,000 characters plus the template.

Every route follows the common rules of [http-api.md](http-api.md): loopback only, Host guard, `Cache-Control: no-store`, JSON errors. Any method other than the ones above answers `404` or `405`; no `GET` changes data.

## Date selection

| `date` | Selected day | `basis` |
|---|---|---|
| a valid `YYYY-MM-DD`, today or earlier | that day | `requested` |
| missing or empty, yesterday is a working day | yesterday | `yesterday` |
| missing or empty, yesterday is not a working day | the most recent working day in the 21 days before today | `previous_working_day` |
| missing or empty, no working day in 21 days | the most recent weekday (Monday-Friday) before today | `last_weekday` |

A working day is a weekday with at least one time event that is not `off`, in the configured `timezone`. `previous_working_day` in the response is always the day the fallback picks (also when a date was requested), so a client can show both.

The selection is deterministic: it depends only on the stored events and the clock.

## Response

```json
{
  "date": "2026-11-20",
  "previous_working_day": "2026-11-20",
  "basis": "previous_working_day",
  "timezone": "Europe/Paris",
  "weekday": "Friday",
  "working_day": true,
  "tracking": {"counted_seconds": 25200, "gap_seconds": 5400, "idle_suspect_seconds": 900},
  "blocks": [
    {"start": "2026-11-20T09:00:00+01:00", "end": "2026-11-20T12:00:00+01:00", "context_id": "acme", "label": "Acme",
     "kind": "project", "duration_seconds": 10800, "counted": true, "idle_suspect": true, "confirmed": true,
     "private": false, "note": "release prep"},
    {"start": "2026-11-20T12:00:00+01:00", "end": "2026-11-20T13:00:00+01:00", "context_id": "break", "label": "Break",
     "kind": "personal", "duration_seconds": 3600, "counted": true, "idle_suspect": false, "confirmed": false, "private": true}
  ],
  "gaps": [{"start": "2026-11-20T16:30:00+01:00", "end": "2026-11-20T17:00:00+01:00", "duration_seconds": 1800}],
  "totals": [{"context_id": "acme", "label": "Acme", "kind": "project", "exportable": true, "counted_seconds": 21600}],
  "contexts": [{"id": "acme", "label": "Acme", "kind": "project", "tempo_ticket": "ACME-1", "private": false}],
  "top": ["Ship the release"],
  "day_items": [],
  "open_items": [{"id": 4, "date": "2026-11-19", "project": "acme", "type": "action", "summary": "Write the rollback plan",
                  "fingerprint": "23f76a72466f42d3", "status": "open"}],
  "new_ideas": [{"id": 9, "ts": "2026-11-20T10:12:00+01:00", "title": "Cache the key images", "text": "Cache the key images"}],
  "agents": [{"label": "Acme", "state": "action_required", "context_id": "acme", "summary": "needs review"}],
  "activity": {"date": "2026-11-20", "events": 412, "active_seconds": 23000, "idle_seconds": 900, "locked_seconds": 2700,
               "break_seconds": 0, "private_seconds": 600, "categories": [], "apps": [], "workspaces": [], "periods": []},
  "close": {"due": false, "saved": true, "saved_at": "2026-11-20T17:05:12+01:00"},
  "missing": ["meeting_minutes", "email_commitments"]
}
```

| Field | Meaning |
|---|---|
| `tracking` | Counted time, untracked working time (gaps) and unconfirmed idle time, in seconds. |
| `blocks` | Tracked blocks of the day, oldest first. `private` is true for personal and system contexts. |
| `gaps`, `totals` | As in the day report ([time-reports.md](time-reports.md)). Totals include personal contexts with `exportable: false`. |
| `contexts` | Active contexts the answer may name. `workdir` only when `review.include_workspace_paths` is on. |
| `top` | The planned top three of that day (may be empty). |
| `day_items` | Items dated that day. |
| `open_items` | Open items of any date, most urgent first, at most 50. |
| `new_ideas` | Untriaged (`new`) `pro` ideas, at most 20, text cut at 300 characters. |
| `agents` | Coding agent instances now: label, state, context, summary. `workspace` only with `review.include_workspace_paths`. |
| `activity` | Summary of the local activity events of that day, see [activity-events.md](activity-events.md#summary). |
| `close` | `saved` and `saved_at` (the first close import of that day, or `null`), `due` (today: after `close_reminder`; earlier days: a working day that was never closed). |
| `missing` | Evidence STKR does not have for that day (below). |

### `missing`

| Value | When |
|---|---|
| `time_tracking` | No time event on that day. |
| `meeting_minutes` | Always: STKR never reads meetings. |
| `email_commitments` | Always: STKR never reads email. |
| `lock_history` | No activity event for that day. |
| `application_activity` | No activity event for that day. |

New values may be added within v1. `missing` never names a context, note or other private data.

## Privacy

- Notes of personal and system blocks are removed (`private: true`, no `note`).
- Items filed under personal or system contexts are never returned. Personal ideas, and ideas captured while a personal or system context was running, are never returned.
- Activity in personal and system contexts is only counted as `private_seconds`; its application, category and workspace are not shown.
- Work folders and agent workspace paths are returned only when `review.include_workspace_paths` is `true` (default `false`).
- No configuration value, secret, file path or environment value is returned.

## Errors

| Case | Status | `error` |
|---|---|---|
| `date` not `YYYY-MM-DD`, not a real date, or after today | 400 | `invalid` |
| Host not `127.0.0.1:<port>` or `localhost:<port>` | 421 | `invalid_host` |
| `POST /api/review/prompt` from a foreign Origin | 403 | `forbidden_origin` |
| Clipboard not available (`POST /api/review/prompt`) | 503 | `unavailable` |
| Any other method on these paths | 404 or 405 | |
