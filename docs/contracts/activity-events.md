# Activity events contract (v1)

Local activity evidence that a client on the same computer (the Stream Deck plugin) sends to STKR: which kind of application was in front, in which workspace, for how long, and whether the session was active, idle, locked or on a break. STKR stores it in its own table, separate from time tracking: activity never creates or changes time events. It is shown in the [review context](review-context.md) and by `stkr activity`.

STKR itself collects nothing. It does not read windows, keystrokes, the screen, the clipboard, browsers, mail or meetings.

## Routes

| Route | Purpose |
|---|---|
| `POST /api/activity/events` | Store a set of events (body below). |
| `GET /api/activity/summary[?date=YYYY-MM-DD\|today\|yesterday]` | Summary of one day (default today). |

The MCP tools `report_activity_event` and `get_activity_summary` do the same with the same rules.

## Request

```json
{
  "events": [
    {
      "ts": "2026-11-20T09:10:00+01:00",
      "app": "Code",
      "category": "development",
      "workspace": "stream-deck",
      "context_id": "stream-deck",
      "duration_seconds": 320,
      "lock_state": "active"
    }
  ]
}
```

Only these fields exist. A request with any other field (for example `window_title`, `url`, `title`, `command`, `clipboard`) is rejected with `400`, at the top level and inside an event.

| Field | Required | Rule |
|---|---|---|
| `ts` | yes | Start time, RFC 3339 with an offset or `Z`. |
| `app` | yes | Application or executable name, 1-64 printable characters, no `/`, `\` or `:` (so no path or URL). |
| `category` | yes | `development`, `meeting`, `email`, `administration`, `browser`, `break`, `lock` or `other`. |
| `workspace` | no | Workspace name or id, up to 128 printable characters, no `/`, `\` or `:` (not a path). |
| `context_id` | no | STKR context id. When missing, STKR maps `workspace` to the context with that id, or to the context whose work folder ends in that name. |
| `duration_seconds` | yes | Whole number, 1 to 86400. |
| `lock_state` | yes | `active`, `idle`, `locked` or `break`. |

Limits: at most 5,000 events and 1 MiB per request. A day of 1,000 events fits easily.

## Behaviour

- **All or nothing for bad input.** Every event is checked first; one malformed event rejects the request (`400`, the message names the event number, never its values) and nothing is stored.
- **Identity.** An event is identified by `ts`, `app` (ignoring case), `category`, `workspace` (ignoring case) and `lock_state`. Sending the same identity again is safe: STKR keeps the larger `duration_seconds` and the latest non-empty `context_id`. A client may resend its whole day; late and out-of-order events give the same result.
- **Range.** Events that start before local midnight `activity.retention_days` days ago (default 90), or more than 5 minutes after STKR's clock, are skipped and counted in `out_of_range` (not an error).
- **Unknown context.** A `context_id` that names no context is dropped (the event is stored without one) and counted in `unmapped`.
- **Retention.** Each request deletes stored events older than the retention window.
- **Off switch.** With `activity.enabled: false` the route answers `503 unavailable`; stored events are still shown.
- STKR logs only counts, never event content.

## Response

```json
{"received": 2, "inserted": 1, "merged": 1, "unchanged": 0, "out_of_range": 0, "unmapped": 0, "pruned": 0, "warnings": []}
```

| Field | Meaning |
|---|---|
| `inserted` | New identities. |
| `merged` | Known identities whose duration grew or whose context was set. |
| `unchanged` | Exact or shorter replays. |
| `out_of_range` | Skipped events (see Range). |
| `unmapped` | Events whose `context_id` was unknown. |
| `pruned` | Stored events deleted for being past retention. |
| `warnings` | Short notes about skipped or unmapped events. |

## Summary

`GET /api/activity/summary?date=2026-11-20` (the `activity` object of the review context has the same shape):

```json
{
  "date": "2026-11-20", "events": 6,
  "first": "2026-11-20T09:00:00+01:00", "last": "2026-11-20T13:01:00+01:00",
  "active_seconds": 2460, "idle_seconds": 1200, "locked_seconds": 630, "break_seconds": 0, "private_seconds": 900,
  "categories": [{"name": "meeting", "seconds": 1800}, {"name": "development", "seconds": 660}],
  "apps": [{"name": "Teams", "seconds": 1800}, {"name": "Code", "seconds": 660}],
  "workspaces": [{"name": "stream-deck", "context_id": "deck", "seconds": 660}],
  "periods": [{"start": "2026-11-20T09:40:00+01:00", "end": "2026-11-20T09:50:30+01:00", "state": "locked", "duration_seconds": 630}]
}
```

- Events belong to the local day of their `ts`.
- `categories`, `apps` (top 10) and `workspaces` split active time only, largest first, ties by name.
- `periods` are idle, locked and break stretches, oldest first. Two events of the same state are joined when the second starts at most 60 seconds after the first ends and no active event came between them. At most 50 are listed; `periods_dropped` counts the rest.
- Active time in personal and system contexts is only counted in `private_seconds`.

## Errors

| Case | Status | `error` |
|---|---|---|
| Not JSON, more than one JSON value, unknown field, missing `events`, a malformed event | 400 | `invalid` |
| More than 5,000 events, or a body over 1 MiB | 413 | `too_large` |
| `activity.enabled` is `false` | 503 | `unavailable` |
| Foreign Origin | 403 | `forbidden_origin` |
| Summary `date` not valid | 400 | `invalid` |
