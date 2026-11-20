# Data model

All data lives in one SQLite database, `stkr.db`, in the data folder. The driver is `modernc.org/sqlite` (pure Go). Every connection uses WAL mode, `busy_timeout=5000`, `foreign_keys=on` and `synchronous=NORMAL`. Write transactions start with `BEGIN IMMEDIATE`. A process uses one connection.

Only `internal/store` contains SQL, and only `internal/service` imports `internal/store`.

## Migrations

- Files: `internal/store/migrations/NNNN_name.sql`, embedded in the binary. `NNNN` is the version.
- `schema_migrations(version, applied_at)` records applied versions. Each pending migration runs in its own transaction together with its `schema_migrations` row, so a failed migration leaves nothing behind.
- Before applying any pending migration to a database that already has tables, the store writes `backups/pre-migrate-<current version>-<UTC time>.db` with `VACUUM INTO`.
- Opening an up-to-date database applies nothing and writes no backup.
- `0001_init.sql` creates every table below and the seed contexts. New migrations must be additive where possible.

## Timestamps

Stored as TEXT, RFC 3339 in UTC with milliseconds: `2026-11-16T08:30:00.000Z`. This format sorts correctly as text. Days are computed in the configured time zone (`timezone`, default: system).

## Tables

| Table | Key | Purpose |
|---|---|---|
| `contexts` | `id` (slug) | What time is tracked against. `kind` ∈ project, mode, personal, system. `active=0` means parked. |
| `events` | `id` autoincrement | One row per switch: `ts`, `context_id` (FK), `source` ∈ deck, cli, api, hook, mcp, ui, system, `note`, `confirmed`. Indexed on `ts`. |
| `agent_instances` | `id` | Coding-agent windows and their state. |
| `ideas` | `id` autoincrement | Captured ideas. `status` ∈ new, triaged, done, archived. `ai_state` ∈ pending, done, failed, skipped. |
| `bookmarks` | `id`; `url` unique | Saved links. `tags` is a comma-separated list. |
| `items` | `id`; `fingerprint` unique | Actions, decisions, notes, follow-ups and risks. Inserting a known fingerprint is a no-op, so repeated imports never duplicate items. |
| `days` | `date` | Daily-loop state: three priorities, brief and close markers. |
| `kv` | `key` | Small runtime flags, for example `backup.last_date`. |
| `schema_migrations` | `version` | Applied migrations. |

Seed contexts: `off` (system), `break` (personal), `planning` (mode), `meeting` (mode), `other` (project). `off` cannot be parked and its kind cannot change.

## Time rules (`internal/timeline`)

Pure functions, no I/O, no wall clock:

- `Blocks(events, from, to, now, cfg)`: a non-`off` event starts a block. It ends at the next event if that is on the same local day; otherwise at `day_end`, or at local midnight if it started after `day_end`. A block never crosses midnight and never passes `now`. A block longer than `idle_gap_minutes` is idle-suspect and counts only if its event is confirmed. Blocks are then clipped to `[from, to)`; flags are kept.
- `Gaps(...)`: on working days (Mon-Fri with a non-`off` event), working-hours time not covered by any block, up to `now`.
- `Aggregate` sums counted time per context. `ExportTotals` also drops personal, system and unknown kinds. `RoundForExport` is the only rounding (nearest step, halves up).

Because no block crosses midnight, a day's report only needs that day's events.

## Switch semantics

- Switching to an unknown context fails (not found); parked contexts fail (conflict). Contexts are never created implicitly.
- A switch to the context of the latest event within 2 seconds returns that event and records nothing.
- `stop` records an `off` event unless the latest event already is `off` (or there is none).

## Backups

- `Store.Backup(dst)` uses `VACUUM INTO`, which is safe while the database is in use.
- Regular backups are `backups/stkr-<UTC time>.db`. The newest 14 are kept. `DailyBackup` makes at most one per local day, tracked in `kv` (`backup.last_date`).
- Restore is manual: stop all processes and copy a backup over `stkr.db` (see the user guide).
