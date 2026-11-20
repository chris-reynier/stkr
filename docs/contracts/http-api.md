# HTTP API contract (v1)

The HTTP API is served by `stkr serve` on `127.0.0.1` only. It is used by Stream Deck actions, scripts and the web UI. Later features append routes to this page.

## Rules for every route

- Base URL: `http://127.0.0.1:<port>` (port from the `port` config key, default 7878). `http://localhost:<port>` works too.
- **Host guard**: the `Host` header must be `127.0.0.1:<port>` or `localhost:<port>`. Anything else gets `421` with error `invalid_host`.
- **Origin guard**: on `POST`, `PUT`, `PATCH` and `DELETE`, an `Origin` header, if present, must be `http://127.0.0.1:<port>`, `http://localhost:<port>` or one of the `allowed_origins` config entries. Otherwise `403` with error `forbidden_origin`. A missing `Origin` is allowed (curl, Stream Deck, scripts). `GET` is not checked.
- Mutations are **POST only**. A `GET` on a mutating route is not served (`404`). This keeps a web page from changing state with an image or link.
- Request bodies are limited to 1 MiB (`413`, error `too_large`).
- Responses are JSON (`Content-Type: application/json`) except images. All responses carry `Cache-Control: no-store`.
- Times are RFC3339 with the configured timezone offset. Durations are integer seconds in `*_seconds` fields (same shapes as [time-reports.md](time-reports.md)).
- Requests are logged as method, path, status and duration. Query strings and bodies are never logged.

## Errors

Every error is JSON:

```json
{"error": "not_found", "message": "context \"nope\" not found"}
```

| HTTP status | `error` | Meaning |
|---|---|---|
| 400 | `invalid` | Bad input (for example an invalid context id or filter). |
| 403 | `forbidden_origin` | Origin guard rejected the request. |
| 404 | `not_found` | Unknown route or unknown item. |
| 409 | `conflict` | Conflicts with current state (for example switching to a parked context). |
| 413 | `too_large` | Body over 1 MiB. |
| 421 | `invalid_host` | Host guard rejected the request. |
| 500 | `internal` | Unexpected error. The message is generic; details are in the log. |
| 503 | `unavailable` | An optional tool or feature is not available. |

## Routes

### `GET /healthz`

Response `200`: `{"ok": true, "version": "0.1.0"}`

### `POST /api/switch/{ctx}`

Start working on context `{ctx}`. Query parameters (all optional):

| Parameter | Meaning |
|---|---|
| `note` | Short note stored with the event. |
| `src` | `deck` marks the event as coming from a Stream Deck. |

The header `X-STKR-Source: deck` does the same as `src=deck`. Without either, the source is `api`.

Response `200` (`SwitchResult`):

```json
{"event": {"id": 12, "ts": "2026-11-16T09:00:00+01:00", "context_id": "other", "source": "deck", "confirmed": false}, "created": true}
```

`created` is `false` when the same context was already switched to within the last 2 seconds (double click): the existing event is returned and nothing is recorded.
Errors: `404` unknown context, `400` invalid id, `409` context is parked.

### `POST /api/stop`

Stop tracking (switch to `off`). Same `src` / header rules and response shape as switch. `created` is `false` when nothing was being tracked.

### `GET /api/now`

Response `200` (`NowStatus`):

```json
{"tracking": true, "context": {"id": "other", "label": "Other", "kind": "project", "active": true, "sort": 0},
 "event_id": 12, "since": "2026-11-16T09:00:00+01:00", "elapsed_seconds": 1800,
 "idle_suspect": false, "today_counted_seconds": 1800}
```

When not tracking: `{"tracking": false, "elapsed_seconds": 0, "idle_suspect": false, "today_counted_seconds": 0}`.

### `GET /api/contexts`

Query parameters: `parked=1` includes parked (inactive) contexts; `kind=project|mode|personal|system` filters by kind.

Response `200`: `{"contexts": [<Context>, ...]}` ordered by `sort`, then `id`. Context fields: `id`, `label`, `kind`, `tempo_ticket`, `active`, `notebook_url`, `workdir`, `icon`, `sort` (empty strings are omitted).

### `GET /img/now.png`

A 144x144 PNG for a Stream Deck key. Content-Type `image/png`, `Cache-Control: no-store`.

| State | Look |
|---|---|
| Not tracking | Dark grey, "OFF" and today's total. |
| Tracking | Context label, elapsed time, today's total. Background by kind: project blue, mode purple, personal green, system grey. |
| Idle-suspect | Orange with a "?" badge. |

Poll it as often as the key needs; it is cheap (cached per distinct picture).

### `GET /api/week/{yyyy-Www}/tempo.csv`

The Tempo import file of an ISO week. Response `200`, `Content-Type: text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="stkr-tempo-<week>.csv"`. The header `X-STKR-Unmapped` lists the ids of contexts that have counted time but no ticket (comma-separated, empty if none); they are not in the file. The format is in [tempo-csv.md](tempo-csv.md). A malformed week gives `400` (`invalid`).

### Agent routes

Routes for coding agent instances. Payloads, states, ordering rules and the `next/go` results are in [agent-events.md](agent-events.md).

| Route | Purpose |
|---|---|
| `POST /api/agents/event` | Body: one event (`instance`, `event`, `state`, `slot`, `workspace`, `window_title`, `summary`, `ts`). Response `{"instance": ..., "switched": bool}`. |
| `POST /api/agents/sync` | Body `{"instances":[...]}`. Replaces the whole set. Response: the same as `GET /api/agents`. |
| `GET /api/agents` | `{"instances", "counts", "unknown_workspaces", "next"}`. |
| `GET /api/agents/next` | `{"instance": ...}`; `{"instance": null}` when there are none. |
| `POST /api/agents/next/go` | Switch, focus or launch. Response `{"result", "instance", "switched", "message"}`. Answers within about 2 seconds (`result: "pending"` if focusing is slower). |
| `POST /api/agents/{instance}/queue` | Queue an instance. `404` if unknown. |
| `POST /api/agents/ack-done` | All `done` instances become `idle`. Response `{"acknowledged": n}`. |
| `GET /img/agents.png` | 144x144 PNG, see [streamdeck-keys.md](streamdeck-keys.md). |

Errors follow the table above; `invalid` (400) covers bad JSON and bad field values.

## Web UI routes

The web UI is part of the same server and follows the same Host and Origin guards. It answers with HTML, not JSON, and is meant for browsers. Pages work without JavaScript.

| Route | Purpose |
|---|---|
| `GET /` and `GET /ui/` | `303` redirect to `/ui/today` |
| `GET /ui/today?d=YYYY-MM-DD` | Timeline, gaps and totals of a day (default today) |
| `GET /ui/week?w=YYYY-Www` | Hours per context and day, tickets, unmapped contexts (default this week) |
| `GET /ui/agents` | Agent instances, Go/Queue buttons, unknown workspaces |
| `GET /ui/settings` | Contexts and configuration forms |
| `GET /static/...` | Embedded stylesheet and HTMX |

Form actions are `POST` with `application/x-www-form-urlencoded`. Every one answers `303` to the local path in the form field `next` (anything that is not a `/ui/` path becomes `/ui/today`) and shows a one-time message from a `stkr_flash` cookie (HttpOnly, SameSite=Strict).

| Route | Fields |
|---|---|
| `POST /ui/switch` | `ctx` |
| `POST /ui/stop` | none |
| `POST /ui/event/{id}/edit` | `date`, `time` (HH:MM), `orig_time`, `context`, `note`. The time changes only if it differs from `orig_time` |
| `POST /ui/event/{id}/split` | `date`, `time` (HH:MM) |
| `POST /ui/event/{id}/merge` | `other` (id of the following event) |
| `POST /ui/event/{id}/delete` | none |
| `POST /ui/event/{id}/confirm` | `confirmed` (`1` or `0`) |
| `POST /ui/agents/go`, `.../queue` | `id` (instance) |
| `POST /ui/agents/go-next`, `.../ack-done` | none |
| `POST /ui/agents/map` | `workspace`, `context` (sets the context workdir) |
| `POST /ui/settings/ctx` | `id`, `label`, `kind`, `tempo_ticket`, `notebook_url`, `workdir` |
| `POST /ui/settings/ctx/{id}/edit` | `label`, `kind`, `tempo_ticket`, `notebook_url`, `workdir` |
| `POST /ui/settings/ctx/{id}/park`, `.../activate` | none |
| `POST /ui/settings/config` | dotted config keys such as `day_end`, `ollama.url`; checkboxes send a hidden `false` first. Unknown keys and `port` / `allowed_origins` are ignored |

All UI responses carry `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and `X-Frame-Options: DENY`. The UI is not a stable API: use the JSON routes above for scripts.
