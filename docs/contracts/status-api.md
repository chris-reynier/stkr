# Status API contract (v1)

`GET /api/status` returns, in one cheap call, everything a tray icon or a small status widget needs: what is running, the switch list, whether the close of the day is due, the cached update check, and two counters. The Windows desktop app polls it every few seconds (`desktop.poll_seconds`). It is served by `stkr serve` and by the server the desktop app runs itself.

All rules of [http-api.md](http-api.md) apply: `127.0.0.1` only, the Host guard (`421 invalid_host`), JSON errors. It is a `GET` route, so the Origin guard does not apply; `POST /api/status` is not served (`404`).

## Request

```
GET http://127.0.0.1:<port>/api/status
```

No parameters, no body. It answers in well under 20 ms (p95) on a normal day of data.

## Response `200`

```json
{
  "now": {
    "context_id": "acme",
    "label": "Acme portal",
    "kind": "project",
    "since": "2026-11-16T09:10:00+01:00",
    "elapsed_s": 4320,
    "idle_suspect": false
  },
  "contexts": [
    {"id": "acme", "label": "Acme portal", "kind": "project"},
    {"id": "meeting", "label": "Meeting", "kind": "mode"},
    {"id": "break", "label": "Break", "kind": "personal"}
  ],
  "close_due": false,
  "update": {"available": true, "version": "v0.2.1"},
  "agents_waiting": 1,
  "ideas_new": 3,
  "version": "v0.2.0",
  "hosted_by": "desktop"
}
```

## Schema

| Field | Type | Meaning |
|---|---|---|
| `now` | object or `null` | The context being tracked. `null` when nothing runs (no event yet, or the last event is `off`). |
| `now.context_id` | string | Context id. |
| `now.label` | string | Context label (may be empty; show the id then). |
| `now.kind` | string | `project`, `mode`, `personal` or `system`. |
| `now.since` | string | RFC3339 time of the switch, with the configured time zone offset. |
| `now.elapsed_s` | integer | Seconds since `since`. |
| `now.idle_suspect` | boolean | The block is longer than `idle_gap_minutes` and not confirmed, so it counts 0 until confirmed. |
| `contexts` | array | Active (not parked) contexts except `off`, most recently used first (by the events of the last 14 days), then in the usual list order. At most 15. Never `null`. |
| `contexts[].id`, `.label`, `.kind` | string | As above. |
| `close_due` | boolean | Today is a working day, the time is after `close_reminder` and no close was imported today. |
| `update` | object or `null` | The cached result of the last update check. `null` when `update.check` is `false`, nothing has been checked yet, or this is a development build. The check itself is never run by this route. |
| `update.available` | boolean | `version` is newer than the running version. |
| `update.version` | string | The latest release found, such as `v0.2.1`. |
| `agents_waiting` | integer | Agent instances in state `action_required` (waiting for your approval or answer). See [agent-events.md](agent-events.md). |
| `ideas_new` | integer | Untriaged (`new`) ideas tagged `pro`, the same number as the ideas inbox key. |
| `version` | string | Version of the running server. |
| `hosted_by` | string | `serve` when `stkr serve` runs the server, `desktop` when the Windows desktop app runs it. |

Workdirs, notebook URLs, notes, idea text and agent summaries are never part of this answer.

## Errors

| Status | `error` | When |
|---|---|---|
| `421` | `invalid_host` | The `Host` header is not `127.0.0.1:<port>` or `localhost:<port>`. |
| `404` | `not_found` | `POST`, `PUT`, `PATCH` or `DELETE` (only `GET` and `HEAD` are served). |
| `500` | `internal` | The database could not be read. Details are in the server log. |

## Client rules (desktop app)

- Probe `GET /healthz` with a 500 ms timeout. `{"ok": true, "version": "..."}` means a STKR server runs on the port; any other answer means the port is used by another program.
- Use a 2 s timeout for `/api/status`. On failure, show the "not reachable" state and probe again at the next poll.
- Send `Host: 127.0.0.1:<port>`, and on `POST` requests `Origin: http://127.0.0.1:<port>`.
- Raise notifications only on transitions (close becomes due, a new update version becomes available, `agents_waiting` goes up), never once per poll.
