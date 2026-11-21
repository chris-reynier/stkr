# Troubleshooting

## What it does

Common problems and what to do about them.

## How to use

Run the failing command again with `--debug` to see log output on stderr. The log file is `logs/stkr.log` in the data folder (`stkr config path` shows the folder). Logs never contain clipboard text, prompts, ideas or secrets; they show lengths and ids instead.

| Problem | What to do |
|---|---|
| `stkr: command not found` | Open a new terminal after installing. If you used the Windows setup, `stkr` is in `%LOCALAPPDATA%\Programs\STKR` (see [Install](install.md#troubleshooting)). Otherwise put the binary on your PATH or run it with its full path. |
| Exit code 2 | The command line or a value was invalid. Read the message on stderr. |
| `invalid configuration` | Fix the listed keys with `stkr config set`, or delete `config.yaml` to reset. |
| `warning: unknown config key` | Check the spelling against [Configuration](configuration.md). |
| Settings go to the wrong place | Check `STKR_HOME` and any `--home` flag. `stkr config path` shows the file in use. |
| Cannot create the data folder | Check you have write permission to the parent folder, or use `--home`. |
| `database is locked` | Another program holds a long write lock on `stkr.db`. Close other STKR processes or database tools and retry. |
| `unknown context` | See [Time tracking](time-tracking.md#troubleshooting). |

## Configuration keys

None for this page. See [Configuration](configuration.md).

## Troubleshooting

If a problem is not listed, open an issue and include the output of `stkr version` and the last lines of `logs/stkr.log` (remove anything private).
