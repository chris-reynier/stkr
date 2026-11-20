# STKR

STKR is a local-first task stack manager. It keeps track of what you are working on, collects ideas and follow-ups, and exports your time. It is a single Go binary. Everything is stored on your machine in `~/.stkr` (or the folder named by `STKR_HOME`). There is no telemetry.

Status: early development (0.x). Windows first, with Linux and macOS builds.

## Features

| Feature | Status |
|---|---|
| CLI skeleton: `version`, `config show/path/set` | available |
| Configuration file with validation | available |
| Context switching, time blocks, idle and gap detection (`switch`, `stop`, `now`, `day`, `week`) | available |
| Contexts (`ctx list/add/set/park/activate`) | available |
| Backups (`backup`) and legacy archive (`import-legacy`) | available |
| Local HTTP server (`serve`): switch, stop, now and contexts API, key image, daily backup job | available |
| Local web UI: Today, Week, Settings, Tempo CSV export (`/ui/today`) | available |
| Stream Deck context keys and current-context image | available |
| Agents hub (`agents`, `/ui/agents`): next instance, window focus, agents key image | available |
| Daily loop: `start-day`, `close`, `import`, `prompts init`, project log export, close reminder | available |
| Capture: ideas, voice notes, phone inbox, meeting actions, bookmarks (`idea`, `ideas`, `bookmark`, `links`) | available |
| AI cleanup of ideas and Start Day plan (Ollama, Claude Code, GitHub Copilot; `ai status/test/run-once`) | available |
| MCP server | planned |
| Self-update | planned |

## Quick start

Build from source (Go 1.27 or newer):

```
go build -o stkr ./cmd/stkr
./stkr version
./stkr config show
./stkr ctx add acme --label "ACME"
./stkr switch acme
./stkr now
./stkr stop
./stkr day
./stkr serve      # local HTTP server for Stream Deck and scripts
```

On Windows the binary is `stkr.exe`. To keep test data away from your real data, set `STKR_HOME` to another folder.

## Documentation

- [User guide](docs/user/index.md)
- [Command reference](docs/user/cli/stkr.md)
- [Architecture](docs/dev/architecture.md) and [testing](docs/dev/testing.md)
- [Interface contracts](docs/contracts/README.md)
- [Contributing](CONTRIBUTING.md) and [security policy](SECURITY.md)

## License

MIT. See [LICENSE](LICENSE).
