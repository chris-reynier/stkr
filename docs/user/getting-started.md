# Getting started

## What it does

STKR is one program, `stkr`, that stores everything in a local data folder. This version provides the command line skeleton and the configuration file. More features arrive in later releases.

## How to use

Build it (needs Go 1.27 or newer):

```
go build -o stkr ./cmd/stkr
```

Check it works:

```
stkr version
stkr config show
stkr config path
stkr config set port 8080
stkr config set ai.provider ollama
```

Add `--json` to any command for machine-readable output. Add `--debug` to print debug logs to stderr. Use `--home <dir>` to use a different data folder.

### Where your data lives

The data folder is, in order of priority: the `--home` flag, the `STKR_HOME` environment variable, or `~/.stkr`. On first run STKR creates it with these folders: `audio`, `prompts`, `backups`, `logs`. The settings file is `config.yaml` in the same folder. Logs are in `logs/stkr.log` and rotate at 5 MB (5 files kept).

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | error |
| 2 | wrong usage, invalid value, or a feature that is unavailable |

## Configuration keys

See [Configuration](configuration.md). `config set` takes dotted keys such as `ollama.url`. Lists are comma-separated.

## Troubleshooting

See [Troubleshooting](troubleshooting.md).
