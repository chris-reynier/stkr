# STKR

STKR is a local-first task stack manager. It keeps track of what you are working on, collects ideas and follow-ups, and exports your time. It is a single Go binary. Everything is stored on your machine in `~/.stkr` (or the folder named by `STKR_HOME`). There is no telemetry.

Status: early development (0.x). Windows first, with Linux and macOS builds.

## Features

| Feature | Status |
|---|---|
| CLI skeleton: `version`, `config show/path/set` | available |
| Configuration file with validation | available |
| Context switching and time blocks | planned |
| Local web UI | planned |
| Stream Deck and agent integration | planned |
| Daily brief and close | planned |
| Voice and phone-inbox capture | planned |
| AI cleanup and classification (local or CLI-based) | planned |
| MCP server | planned |
| Self-update | planned |

## Quick start

Build from source (Go 1.27 or newer):

```
go build -o stkr ./cmd/stkr
./stkr version
./stkr config show
./stkr config set port 8080
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
