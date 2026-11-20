# Architecture

STKR is a single Go binary: a CLI, a localhost HTTP API with a server-rendered UI, an MCP server over stdio, and Stream Deck support through HTTP. Data is stored in SQLite.

## Layers

```
cmd/stkr -> internal/cli --+
            internal/httpapi +--> internal/service --> internal/store (SQLite)
            internal/web ----+          |
            internal/mcpserver          +--> internal/timeline (pure time rules)
                                        +--> internal/ai, capture, daily, keyimg, platform
internal/app       wiring: config -> dependencies -> service; background workers
internal/config    YAML load/save, defaults, validation, redaction
internal/clock     Clock interface, Real, Fake
internal/logx      slog setup, rotating log file, redaction helper
internal/domain    types, enums, errors (no dependencies)
internal/buildinfo version, commit, date
internal/testutil  fake binaries, temp home helpers
internal/e2e       tests that drive the built binary
tools/gendocs      generates docs/user/cli
```

Packages not yet in the repository are added as features land. Tables, migrations and time rules are described in [Data model](data-model.md).

Rules:

- `service.Service` is the only API used by the CLI, HTTP, web and MCP adapters. Adapters parse input, call the service and render output.
- Only `internal/service` imports `internal/store` (enforced by the `depguard` lint rule). The store has no business logic.
- `timeline` is pure functions with no I/O.
- Time comes from an injected `clock.Clock`; only `internal/clock` calls `time.Now()`.
- Domain errors (`ErrNotFound`, `ErrConflict`, `ErrInvalid`, `ErrUnavailable`) are wrapped with `%w`. Adapters map them to exit codes or HTTP status in one place. In the CLI, `ErrInvalid` and `ErrUnavailable` exit with code 2.
- Platform-specific code sits behind interfaces in build-tagged files; other platforms get no-op versions that return `ErrUnavailable`.

## Processes

- `stkr serve` runs the HTTP API and UI plus background workers.
- `stkr mcp` runs the MCP server on stdio and opens the database directly.
- Other commands open the database directly and exit. Commands that need state held by `serve` call `http://127.0.0.1:<port>` and exit with code 2 and a hint if it is not running.
- SQLite uses WAL mode, a 5 second busy timeout and foreign keys.

## Data directory

`STKR_HOME`, or `~/.stkr`: `stkr.db`, `config.yaml`, `audio/`, `prompts/`, `backups/`, `logs/`. Created on first run with user-only permissions. Files are written atomically (temp file, sync, rename).

## Logging

`log/slog` through `internal/logx`: JSON lines in `logs/stkr.log`, rotated at 5 MB with 5 files kept. `--debug` also writes text logs to stderr. User content is never logged; use `logx.Redact`, which returns only the length.

## Build

`CGO_ENABLED=0` always. Version information is injected with `-ldflags -X` into `internal/buildinfo`.
