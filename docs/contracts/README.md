# Interface contracts

Every external interface of STKR is documented here: HTTP payloads, file formats, text fences, plugin events and CSV exports. Other tools (Stream Deck plugins, scripts, AI wrappers) can rely on these pages.

## Versioning policy

- Each contract has a version in its title, such as `v1`.
- Within a version, changes are additive only: new optional fields, new endpoints. Existing fields keep their meaning.
- A breaking change creates a new version (`v2`). The old version stays documented and supported for at least one minor release.
- Every contract page has: a version, a schema (JSON or text), an example, and the error cases.

## Index

No contracts have been published yet. They are added with the features that introduce them:

| File | Covers |
|---|---|
| `http-api.md` | HTTP routes: parameters, request and response JSON, errors |
| `tempo-csv.md` | Time export CSV columns, rounding, ticket mapping |
| `agent-events.md` | Agent plugin events and states |
| `streamdeck-keys.md` | Stream Deck key to endpoint map, image states |
| `import-fences.md` | Text blocks used to import daily briefs and closes |
| `onedrive-export.md` | Layout of the exported log |
| `paste-helper.md` | Window title and launch parameters |
| `phone-inbox.md` | File naming and tag rules for phone captures |
| `ai-providers.md` | AI provider protocol and JSON schemas |
| `mcp-tools.md` | MCP tool names and schemas |
