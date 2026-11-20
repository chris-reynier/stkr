# Interface contracts

Every external interface of STKR is documented here: HTTP payloads, file formats, text fences, plugin events and CSV exports. Other tools (Stream Deck plugins, scripts, AI wrappers) can rely on these pages.

## Versioning policy

- Each contract has a version in its title, such as `v1`.
- Within a version, changes are additive only: new optional fields, new endpoints. Existing fields keep their meaning.
- A breaking change creates a new version (`v2`). The old version stays documented and supported for at least one minor release.
- Every contract page has: a version, a schema (JSON or text), an example, and the error cases.

## Index

Published:

| File | Covers |
|---|---|
| [`time-reports.md`](time-reports.md) | JSON of switch, now, day, week and contexts; week CSV (v1) |
| [`http-api.md`](http-api.md) | HTTP routes: parameters, request and response JSON, errors (v1) |
| [`tempo-csv.md`](tempo-csv.md) | Tempo CSV columns, rounding, ticket mapping (v1) |
| [`agent-events.md`](agent-events.md) | Agent event and sync payloads, states, state map, next-instance order (v1) |
| [`streamdeck-keys.md`](streamdeck-keys.md) | Stream Deck key to endpoint to image table (v1) |
| [`import-fences.md`](import-fences.md) | `stkr-brief` and `stkr-close` text blocks, fingerprints, pipe fallback (v1) |
| [`onedrive-export.md`](onedrive-export.md) | Project log files: section ownership, atomic rewrite (v1) |
| [`paste-helper.md`](paste-helper.md) | `POST /api/paste`: window title, Enter, errors (v1) |
| [`phone-inbox.md`](phone-inbox.md) | Phone inbox file naming, tag rules, stability, processed folder (v1) |
| [`mcp-tools.md`](mcp-tools.md) | MCP tools, resource and prompts of `stkr mcp`: inputs, outputs, errors, privacy (v1, generated) |

Added with the features that introduce them:

| File | Covers |
|---|---|
| `agent-events.md` | Agent plugin events and states |
| `streamdeck-keys.md` | Stream Deck key to endpoint map, image states |
| `ai-providers.md` | AI provider protocol and JSON schemas |
