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


### The desktop app (Windows)

| Problem | What to do |
|---|---|
| I do not see the STKR icon | Windows hides new icons behind the `^` arrow next to the clock. Click it and drag the STKR icon onto the taskbar, or turn STKR on under Settings, Personalization, Taskbar, Other system tray icons. If it is not there either, look for `stkrw.exe` in Task Manager and read `logs\desktop.log` in the data folder. See [Desktop app](desktop.md#troubleshooting). |
| Windows says "Windows protected your PC" (SmartScreen) when I run setup | The setup is not code-signed yet. Choose **More info**, then **Run anyway**. If there is no such button your organisation blocks unsigned programs: ask your administrator, or use the zip ([Install](install.md)). The update check verifies the SHA-256 of what it downloads against `checksums.txt` before anything runs. |
| A notification says "Microsoft Edge WebView2 is not installed" | The panel and the app window need Microsoft's WebView2 Runtime (part of Windows 11 and current Windows 10). Install the "Evergreen Standalone Installer" from Microsoft, then restart STKR. Until then STKR opens its pages in your browser, so nothing is lost. |
| A notification says "The STKR window could not be opened" | The WebView2 Runtime is there but a window failed to start (for example the `desktop\webview` folder in the data folder cannot be written). STKR keeps running and opens its pages in the browser. Restart STKR; if it repeats, delete the `desktop\webview` folder and send the last lines of `logs\desktop.log`. |
| "Port 7878 is in use" | Another program uses the port. STKR runs on a free port for now and opens Settings; set `port` to a free number (`stkr config set port 7979`), update your Stream Deck keys and restart STKR. See [Desktop app](desktop.md#troubleshooting). |
| The hotkey does nothing | Another program uses Ctrl+Alt+Space (STKR shows a notification at start). Choose another key in Settings, Desktop app, "Panel hotkey", for example `Ctrl+Shift+K`. Keys such as Win+L belong to Windows. |
| The update failed or was rolled back | STKR shows the reason and keeps running the version you had. If the install step failed, the old files are put back and STKR starts again. Nothing is lost; your data is not touched by an update. Try again later, or run the setup file of the release by hand ([Update](update.md#troubleshooting)). |
| I updated from v0.1.0 with `stkr update` and have no icon | `stkr update` in v0.1.0 replaces only `stkr.exe`. Download the setup file of the current release and run it once; later updates are one click ([Update](update.md#from-v010)). |

## Configuration keys

None for this page. See [Configuration](configuration.md).

## Troubleshooting

If a problem is not listed, open an issue and include the output of `stkr version` and the last lines of `logs/stkr.log` (remove anything private).
