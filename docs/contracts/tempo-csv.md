# Tempo CSV contract (v1)

The file that `stkr week --csv` prints and `GET /api/week/{yyyy-Www}/tempo.csv` serves. It lists one line per day and ticket, ready to import into a timesheet tool such as Tempo.

## Format

- UTF-8, **no byte order mark**. Lines end with CRLF (RFC 4180). Fields are quoted only when they contain a comma, a quote or a line break; quotes inside a field are doubled.
- One header line, then data lines:

```
date,ticket,hours,context,description
```

| Column | Content |
|---|---|
| `date` | `YYYY-MM-DD` in the configured time zone |
| `ticket` | the `tempo_ticket` of the contexts, for example `ACME-1` |
| `hours` | decimal hours with two decimals, for example `0.75`. Rounded, see below |
| `context` | ids of the contexts that make up the line, sorted and joined with `+` (`acme+beta`) |
| `description` | the notes of the counted blocks in time order, without duplicates, joined with `; `. If no block has a note, the context labels joined with `; `. Cut to 500 characters |

A field that starts with `=`, `+`, `-` or `@` is prefixed with `'` so spreadsheets do not run it as a formula.

## Rules

1. Only counted time is exported. Idle-suspect blocks that were not confirmed and gaps in working hours count as zero.
2. Only `project` and `mode` contexts are exported. `personal` and `system` contexts never appear.
3. Counted time is summed per date and ticket first (several contexts may share a ticket), then rounded to the nearest multiple of the `rounding_hours` config key (default `0.25`; halves round up). Rounding never happens before summing.
4. A line that rounds to zero hours is left out.
5. Contexts with counted time but **no ticket** are not written. They are listed next to the file instead: the web page shows them under "Unmapped contexts", `stkr week --csv` prints them to stderr (`not exported, no tempo ticket: a, b`), and the HTTP response carries them in the `X-STKR-Unmapped` header (comma-separated ids, empty if none). This keeps the file valid for importers that do not accept comment lines.
6. Lines are ordered by date, then ticket. A week without exportable time gives the header line only.

## Example

```
date,ticket,hours,context,description
2026-11-16,ACME-1,0.75,acme+beta,"fix ""auth"", retry"
2026-11-18,ACME-1,1.50,acme,ACME
```

## HTTP

`GET /api/week/{yyyy-Www}/tempo.csv` returns `200` with `Content-Type: text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="stkr-tempo-<week>.csv"` and the `X-STKR-Unmapped` header. A malformed week gives `400` with the JSON error body described in [http-api.md](http-api.md).

## Errors

| Case | Result |
|---|---|
| Invalid week such as `2026-47` | CLI exit code 2; HTTP `400` `invalid` |
| Week without data | header line only, exit code 0 / HTTP `200` |

The unrounded per-context view (`stkr week --csv --raw`) is a different format, described in [time-reports.md](time-reports.md).
