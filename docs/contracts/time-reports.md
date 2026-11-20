# Time reports and week CSV — v1

This contract covers the JSON that `stkr switch`, `stop`, `now`, `day`, `day confirm`, `week` and `ctx` print with `--json`, and the raw CSV that `stkr week --csv --raw` prints. The rounded Tempo file is in [tempo-csv.md](tempo-csv.md). The HTTP API returns the same objects.

## Conventions

- Times are RFC 3339 with milliseconds and the offset of the configured time zone, for example `2026-11-16T09:30:00.000+01:00`. In the database they are stored in UTC.
- Dates are `YYYY-MM-DD` in the configured time zone. Weeks are ISO weeks, `YYYY-Www`.
- Durations are whole seconds (rounded down) in fields ending in `_seconds`.
- Durations are **not** rounded to `rounding_hours`. Rounding happens only in timesheet exports.
- Optional string fields are left out when empty.

## Schemas

### Context

```json
{ "id": "acme", "label": "ACME", "kind": "project|mode|personal|system",
  "tempo_ticket": "AC-1", "active": true, "notebook_url": "…", "workdir": "…", "icon": "…", "sort": 0 }
```

`stkr ctx list --json` prints an array of these. `ctx add` and `ctx set` print one. `ctx park` and `ctx activate` print `{"id": "acme", "active": false}`.

### Event

```json
{ "id": 12, "ts": "2026-11-16T09:30:00.000+01:00", "context_id": "acme",
  "source": "deck|cli|api|hook|mcp|ui|system", "note": "optional", "confirmed": false }
```

`stkr day confirm --json` prints one event.

### SwitchResult (`switch`, `stop`)

```json
{ "event": { …Event… }, "created": true }
```

`created` is `false` when nothing was recorded: a repeated switch to the active context within 2 seconds (the existing event is returned), or `stop` while not tracking (the last `off` event, or an empty event with `id` 0).

### NowStatus (`now`)

```json
{ "tracking": true, "context": { …Context… }, "event_id": 12,
  "since": "2026-11-16T09:30:00.000+01:00", "elapsed_seconds": 2700,
  "idle_suspect": false, "note": "optional", "today_counted_seconds": 9000 }
```

When not tracking: `tracking` is `false` and `context`, `event_id`, `since` and `note` are left out.

### Block

```json
{ "event_id": 12, "context_id": "acme", "label": "ACME", "kind": "project",
  "start": "…", "end": "…", "duration_seconds": 3600,
  "idle_suspect": false, "confirmed": false, "counted": true, "note": "optional" }
```

`counted` is `!idle_suspect || confirmed`. `idle_suspect` is decided on the whole block, even when the report shows only part of it.

### Gap

```json
{ "start": "…", "end": "…", "duration_seconds": 1800 }
```

### ContextTotal

```json
{ "context_id": "acme", "label": "ACME", "kind": "project", "tempo_ticket": "AC-1",
  "exportable": true, "counted_seconds": 7200 }
```

`exportable` is `true` only for kinds `project` and `mode`. Totals contain counted time only and are sorted by `counted_seconds`, largest first.

### DayReport (`day`)

```json
{ "date": "2026-11-16", "weekday": "Monday", "working_day": true,
  "window_start": "2026-11-16T09:00:00+01:00", "window_end": "2026-11-16T17:00:00+01:00",
  "events": [ …Event… ], "blocks": [ …Block… ], "gaps": [ …Gap… ], "totals": [ …ContextTotal… ],
  "counted_seconds": 9000, "idle_suspect_seconds": 7200, "gap_seconds": 12600 }
```

`events` holds every event of that date, including `off` events. `idle_suspect_seconds` is idle-suspect time that is not confirmed.

### WeekReport (`week`)

```json
{ "week": "2026-W47", "start": "2026-11-16", "end": "2026-11-22",
  "days": [ …7 DayReport, Monday first… ], "totals": [ …ContextTotal… ],
  "counted_seconds": 9000, "idle_suspect_seconds": 0, "gap_seconds": 0, "working_days": 2 }
```

## Raw week CSV (`stkr week --csv --raw`)

Comma-separated, RFC 4180 quoting, UTF-8, one header line:

```
date,context,label,tempo_ticket,hours
```

| Column | Content |
|---|---|
| `date` | `YYYY-MM-DD` |
| `context` | context id |
| `label` | context label |
| `tempo_ticket` | ticket key, empty if none |
| `hours` | counted hours, **unrounded**, two decimals (`1.42`) |

One row per day and context with counted time. Only `project` and `mode` contexts appear; `personal` and `system` are never written. Rows are ordered by date, then by hours (largest first). A label or ticket that starts with `=`, `+`, `-` or `@` is prefixed with `'` so spreadsheets do not run it as a formula.

Example:

```
date,context,label,tempo_ticket,hours
2026-11-16,acme,ACME,AC-1,1.00
2026-11-18,acme,ACME,AC-1,0.75
```

The rounded timesheet export has its own contract.

## Errors

The CLI prints `stkr: <message>` on stderr and exits with:

| Exit | Cause | Example message |
|---|---|---|
| 1 | Unknown context (not found) | `unknown context "nope": run 'stkr ctx list' …` |
| 1 | Parked context, or context id already exists (conflict) | `context "acme" is parked: run 'stkr ctx activate acme' first` |
| 1 | Unknown event id | `event 42 not found` |
| 2 | Invalid input: context id, kind, date, week, note longer than 1000 characters, event time in the future | `invalid week "2026-47" (want YYYY-Www, …)` |
| 2 | Wrong number of arguments or unknown flag | `accepts 1 arg(s), received 0` |
