# Desktop app (Windows)

## What it does

`stkrw.exe` puts an STKR icon in the Windows notification area, next to the clock. You do not need a terminal: start it from the Start menu or by double-clicking it.

- The icon shows what is running. Grey: nothing runs. Blue (project), purple (mode) or green (personal) with a white dot: a context runs. Amber: the current block is idle-suspect (longer than `idle_gap_minutes`, counts 0 until you confirm it). A red badge: your close of the day is due.
- The tooltip shows the context and the time on it, for example `STKR - Acme portal 1h12`.
- Right-click opens the menu with the daily actions. Left-click opens the Today page.
- It shows a notification when the close of the day becomes due, when an update is found and when an agent starts waiting for your approval. Each one is shown once, not at every refresh.

`stkrw.exe` runs the same STKR server as `stkr serve`, inside its own process, so Stream Deck keys, the web UI and `stkr` commands keep working while only the icon runs. If `stkr serve` is already running, the app uses that server instead ("attach mode") and does not start a second one. All menu actions go through the local server, exactly like a Stream Deck key or a `curl` call.

`stkr.exe` stays the command-line program. Both come from the same release and use the same data folder.

## How to use

If your copy of STKR has no `stkrw.exe` next to `stkr.exe`, build it from source (Go 1.27 or newer):

```
go build -ldflags "-H windowsgui" -o stkrw.exe ./cmd/stkrw
```

Start it:

```
stkrw.exe
```

It opens no window. Look for the icon next to the clock (see Troubleshooting if you do not see it). Starting it again while it runs does not create a second icon: the running one opens the Today page.

The menu:

| Entry | What it does |
|---|---|
| First line | The current context and time, or "Nothing running". Not clickable. |
| Switch to | Your active contexts, most recently used first. The current one is checked. "Manage projects..." opens Settings. |
| Stop | Stop tracking (switch to `off`). |
| Capture idea... | Opens the Ideas page with its capture box. |
| Save clipboard as bookmark | Saves the URL on the clipboard as a bookmark. |
| Start day | Logs planning and copies the Start Day prompt to the clipboard. |
| Close day | Copies the Close Day prompt to the clipboard. Shows "(due)" after `close_reminder`. |
| Import answer from clipboard | Imports the AI answer you copied. |
| Open | Today, Week, Agents, Ideas, Links or Settings in your browser. |
| Data folder, Logs folder | Open the folder in Explorer. |
| About STKR | Shows the version, the port and whether the app runs the server or uses `stkr serve`. |
| Quit | Removes the icon and stops the server it runs (within 5 seconds). A `stkr serve` it attached to keeps running. |

Each action shows its result in a notification: a short confirmation, or the error message from the server (for example "no stkr-brief or stkr-close block with valid lines was found").

Use a different data folder for a run:

```
stkrw.exe -home D:\stkr-test
```

or set `STKR_HOME`, as for `stkr`. Each data folder can have its own icon.

### Start it at sign-in

Press Win+R, type `shell:startup`, and put a shortcut to `stkrw.exe` in that folder. Do not also autostart `stkr serve` for the same data folder; the app runs the server itself.

### Keep the icon visible

Windows hides new icons in the overflow area (the `^` arrow). To keep the STKR icon next to the clock, drag it from the overflow area onto the taskbar, or open Settings > Personalization > Taskbar > Other system tray icons and turn STKR on.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `desktop.notifications` | `true` | Show the automatic notifications (close due, update found, agent waiting). Results of actions you start from the menu are always shown. |
| `desktop.poll_seconds` | `5` | How often the icon, tooltip and menu are refreshed (2 to 60 seconds). |
| `port` | `7878` | The port the app looks for a running server on, and the port it runs its own server on. |

Change them in Settings or with `stkr config set desktop.poll_seconds 10`, then quit and start the app again.

The app writes its own log to `logs\desktop.log` in the data folder; the server it runs writes to `logs\stkr.log` as usual. Context labels and idea text are not written to `desktop.log`.

## Troubleshooting

- **I do not see the icon.** Click the `^` arrow next to the clock: new icons start there. Drag it to the taskbar to keep it visible (see above). If it is not there either, check Task Manager for `stkrw.exe` and look at `logs\desktop.log`.
- **The icon is grey and the tooltip says "server not reachable".** The app could not start its server, or the `stkr serve` it used has stopped. It tries again at every refresh and takes over when `stkr serve` is gone. Check `logs\desktop.log` and `logs\stkr.log`.
- **"Port 7878 is in use, change `port` in Settings".** Another program (not STKR) uses the port. The app then runs its server on a free port and opens Settings there. Stream Deck keys cannot reach a random port: set `port` to a free one, for example `stkr config set port 7979`, update your Stream Deck keys, and restart the app.
- **About says "attached to `stkr serve`".** A `stkr serve` was already running on the port, so the app uses it. That is fine. If you want the app to run the server, stop `stkr serve` (the app takes over within one refresh) and remove its autostart task.
- **Notifications do not appear.** Check `desktop.notifications`, and Windows Settings > System > Notifications (Focus and Do not disturb also hide them).
- **I am on Linux or macOS.** The desktop app is Windows only. `stkrw` prints "the desktop app is Windows only, use `stkr serve`" and exits with code 2. Use `stkr serve` and the web UI.
