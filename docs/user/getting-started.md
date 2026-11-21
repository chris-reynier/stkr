# Getting started

## What it does

STKR keeps track of what you work on, collects your ideas, and prepares your time for a timesheet. Everything is stored in a local data folder. On Windows you can use it entirely from the STKR icon next to the clock; on any system you can use the command line (`stkr`) and the local web page. This page has two paths: the desktop path (Windows, no terminal) and the command-line path. The [user guide](index.md) lists every topic.

## How to use

### Desktop path (Windows, no terminal)

1. **Install.** Run the setup file from the release page ([Install](install.md)). Leave "Launch STKR" ticked. The STKR icon appears next to the clock; if you cannot see it, click the `^` arrow and drag the icon onto the taskbar.
2. **Create a project.** Right-click the icon and choose **Manage projects...** (or click the icon, then **Settings**). Under "Add context", fill in an ID (for example `acme`), a label (for example `ACME portal`) and the kind `project`, and press **Add context**. If you need a Tempo ticket for the timesheet, enter it in the same form. Repeat for each project. The built-in contexts `off`, `break`, `planning`, `meeting` and `other` are already there.
3. **Switch from the panel.** Click the icon. The panel shows your projects as buttons, most recently used first. Click `ACME portal`: the icon turns blue, the tooltip shows the project and the time on it. Click another project to switch (the time moves with you), or **Stop** to stop. Press **Ctrl+Alt+Space** from any program to open the panel from the keyboard.
4. **Capture an idea.** In the panel, type in the idea box and press Enter. Shift+Enter starts a new line. Tick "Personal" to keep the idea away from any cloud AI. Ideas are listed on the Ideas page (**Open STKR**, then Ideas).
5. **Start and close your day.** In the panel press **Start day**: it records planning time and copies a prompt to the clipboard. Paste it into your AI chat, copy the answer the chat gives you, and press **Import from clipboard** in the panel. At the end of the day press **Close day** and import the answer the same way. [Daily loop](daily-loop.md) explains the answer format.
6. **See your week.** Choose **Open STKR**, then **Week**. Each day shows the tracked time per project. Blocks that ran for a long time without a switch are marked "idle-suspect" and count 0 until you confirm them on the Today page; gaps in your working hours are flagged and never counted.
7. **Export Tempo CSV.** On the Week page press **Download Tempo CSV**. The file has one row per day and ticket, rounded to 0.25 h; projects without a ticket are listed below the table instead of being exported, and personal time is never exported. Import the file into Tempo. See [Web UI](web-ui.md).
8. **Updates and backups.** **Settings** has **Back up now** and the **Updates** box. When a new version exists, the icon's menu shows **Install update** ([Update](update.md)).

If the panel does not open and a notification says WebView2 is missing, STKR opens the same pages in your browser instead; see [Troubleshooting](troubleshooting.md).

### Command-line path (any system)

Install STKR ([Install](install.md)) or build it (needs Go 1.27 or newer):

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

Track time (see [Time tracking](time-tracking.md)):

```
stkr ctx add acme --label "ACME portal" --kind project
stkr ctx list
stkr switch acme
stkr now
stkr stop
stkr day
```

Open the web page with `stkr serve` (it prints `http://127.0.0.1:7878`). On Windows you do not need `stkr serve` when the desktop app runs: it runs the same server.

Add `--json` to any command for machine-readable output. Add `--debug` to print debug logs to stderr. Use `--home <dir>` to use a different data folder.

### Where your data lives

The data folder is, in order of priority: the `--home` flag, the `STKR_HOME` environment variable, or `~/.stkr` (on Windows `C:\Users\<you>\.stkr`). On first run STKR creates it with these folders: `audio`, `prompts`, `backups`, `logs`. The database is `stkr.db`; see [Backup and restore](backup-restore.md). The settings file is `config.yaml` in the same folder. Logs are in `logs/stkr.log` (the desktop app also writes `logs/desktop.log`) and rotate at 5 MB (5 files kept). The desktop app and `stkr` use the same data folder, so what you do in one shows in the other.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | error |
| 2 | wrong usage, invalid value, or a feature that is unavailable |

## Configuration keys

See [Configuration](configuration.md). On Windows most of them can be changed in Settings. `config set` takes dotted keys such as `ollama.url`. Lists are comma-separated.

## Troubleshooting

See [Troubleshooting](troubleshooting.md) and, for the icon and panel, [Desktop app](desktop.md#troubleshooting).
