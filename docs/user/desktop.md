# Desktop app (Windows)

## What it does

`stkrw.exe` puts an STKR icon in the Windows notification area, next to the clock. You do not need a terminal: start it from the Start menu or by double-clicking it.

- The icon shows what is running. Grey: nothing runs. Blue (project), purple (mode) or green (personal) with a white dot: a context runs. Amber: the current block is idle-suspect (longer than `idle_gap_minutes`, counts 0 until you confirm it). A red badge: your close of the day is due.
- The tooltip shows the context and the time on it, for example `STKR - Acme portal 1h12`.
- Left-click opens the **panel**: a small window with the everyday buttons that stays above all other windows, also maximized ones. Right-click opens the menu with the daily actions. "Open STKR" opens the full interface in a normal window.
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

It opens no window at first. Look for the icon next to the clock (see Troubleshooting if you do not see it) and click it. Starting it again while it runs does not create a second icon: the running one shows the panel.

### The panel

The panel is about 360 x 560 pixels. Top to bottom:

- the current context with the time on it, and a Stop button;
- your active contexts as buttons, most recently used first. Click one to switch. With more than 8 contexts a filter box appears;
- a box to capture an idea. Enter saves it, Shift+Enter starts a new line. "Personal" keeps it out of any cloud AI provider. "Record voice" appears when `ffmpeg` and `whisper` are set up (see [Capture](capture.md));
- Start day, Close day (highlighted when it is due) and Import from clipboard;
- "Agents waiting: N", linking to the Agents page, and a line when the close of the day is due or an update is available;
- at the bottom: Open STKR (the full interface in its own window), Settings and the pin button.

The panel refreshes itself every `desktop.poll_seconds`.

- **Show and hide.** Left-click the icon, use the hotkey or choose "Show panel" in the menu. Press Esc, click its X, or left-click the icon again to hide it. Hiding does not quit STKR; Quit is in the tray menu.
- **Pin.** While pinned (the default) the panel stays above every other window. Click "Pinned" in the panel, or tick "Keep panel on top" in the tray menu, to let other windows cover it. The choice is saved in `desktop.panel_on_top`.
- **Position.** The first time, the panel opens near the mouse pointer (where you clicked the icon). After that it remembers where you put it and its size, also after a restart. If the screen layout changed and the saved place is no longer visible, it moves back onto a screen.
- **Links.** A link in the panel opens in the app window. Links to other sites open in your default browser, never inside STKR.

### The app window

"Open STKR", and the pages in the menu entry Open, show the full interface (Today, Week, Agents, Ideas, Links, Settings) in a normal, resizable window with a taskbar button. It remembers its size and position. Closing it only closes the window. Links to other sites open in your default browser.

The panel and the app window use the Microsoft Edge WebView2 Runtime, which is part of Windows 11 and of current Windows 10. If it is missing, STKR shows a notification once and opens the pages in your browser instead; see Troubleshooting.

### The hotkey

Press Ctrl+Alt+Space from any program to show the panel with the idea box focused, type, press Enter, and press the hotkey (or Esc) again to hide it. Change it with `desktop.hotkey`, for example `Ctrl+Shift+K`; an empty value turns it off. The format is modifiers joined with `+`, then one key: modifiers are Ctrl, Alt, Shift and Win (at least one), keys are A-Z, 0-9, F1-F24, Space, Enter, Tab, Esc, Backspace, Delete, Insert, Home, End, PageUp, PageDown and the arrow keys. If another program already uses the key, STKR says so in a notification and the hotkey stays off.

The menu:

| Entry | What it does |
|---|---|
| First line | The current context and time, or "Nothing running". Not clickable. |
| Show panel, Open STKR | Show the panel, or open the app window on Today. |
| Switch to | Your active contexts, most recently used first. The current one is checked. "Manage projects..." opens Settings. |
| Stop | Stop tracking (switch to `off`). |
| Capture idea... | Shows the panel with the idea box focused. |
| Save clipboard as bookmark | Saves the URL on the clipboard as a bookmark. |
| Start day | Logs planning and copies the Start Day prompt to the clipboard. |
| Close day | Copies the Close Day prompt to the clipboard. Shows "(due)" after `close_reminder`. |
| Import answer from clipboard | Imports the AI answer you copied. |
| Open | Today, Week, Agents, Ideas, Links or Settings in the app window. |
| Keep panel on top | Checked while the panel stays above other windows. Click to turn it off or on. |
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

The installer has an option for this ("Start STKR when I sign in to Windows", on by default); it adds a sign-in entry for `stkrw.exe`. To turn it off later, open Task Manager, Startup apps, and disable STKR. If you installed from the zip, press Win+R, type `shell:startup`, and put a shortcut to `stkrw.exe` in that folder. Do not also autostart `stkr serve` for the same data folder; the app runs the server itself.

### Keep the icon visible

Windows hides new icons in the overflow area (the `^` arrow). To keep the STKR icon next to the clock, drag it from the overflow area onto the taskbar, or open Settings > Personalization > Taskbar > Other system tray icons and turn STKR on.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `desktop.notifications` | `true` | Show the automatic notifications (close due, update found, agent waiting). Results of actions you start from the menu are always shown. |
| `desktop.poll_seconds` | `5` | How often the icon, tooltip, menu and panel are refreshed (2 to 60 seconds). |
| `desktop.panel_on_top` | `true` | Keep the panel above other windows (the pin). |
| `desktop.hotkey` | `Ctrl+Alt+Space` | Global key that shows or hides the panel. Empty turns it off. |
| `desktop.open_panel_on_start` | `false` | Show the panel when the app starts. |
| `port` | `7878` | The port the app looks for a running server on, and the port it runs its own server on. |

Change them in Settings or with `stkr config set desktop.poll_seconds 10`, then quit and start the app again. The pin is the exception: the pin button and the tray menu apply it at once and save it.

The app also keeps the position of its windows in `desktop\window.json` in the data folder. It is not configuration; delete the file to reset the positions.

The panel and the app window keep their browser data (cache and cookies of the pages) in `desktop\webview` in the data folder.

The app writes its own log to `logs\desktop.log` in the data folder; the server it runs writes to `logs\stkr.log` as usual. Context labels and idea text are not written to `desktop.log`.

## Troubleshooting

- **I do not see the icon.** Click the `^` arrow next to the clock: new icons start there. Drag it to the taskbar to keep it visible (see above). If it is not there either, check Task Manager for `stkrw.exe` and look at `logs\desktop.log`.
- **The icon is grey and the tooltip says "server not reachable".** The app could not start its server, or the `stkr serve` it used has stopped. It tries again at every refresh and takes over when `stkr serve` is gone. Check `logs\desktop.log` and `logs\stkr.log`.
- **"Port 7878 is in use, change `port` in Settings".** Another program (not STKR) uses the port. The app then runs its server on a free port and opens Settings there. Stream Deck keys cannot reach a random port: set `port` to a free one, for example `stkr config set port 7979`, update your Stream Deck keys, and restart the app.
- **About says "attached to `stkr serve`".** A `stkr serve` was already running on the port, so the app uses it. That is fine. If you want the app to run the server, stop `stkr serve` (the app takes over within one refresh) and remove its autostart task.
- **"Microsoft Edge WebView2 is not installed".** The panel and the app window need the WebView2 Runtime. Install the "Evergreen Standalone Installer" from Microsoft's WebView2 page, then restart STKR. Until then the panel actions open Today (or Ideas, for "Capture idea...") in your browser and the other pages open in your browser too. The panel is an ordinary page, so you can also open `http://127.0.0.1:7878/ui/panel` in a browser tab yourself.
- **The hotkey does nothing.** STKR shows a notification at start if another program already uses the key. Pick another one with `desktop.hotkey`. Some keys, such as Win+L, are reserved by Windows and cannot be used.
- **The panel is not on top of a program.** Check that the pin is on ("Keep panel on top" is checked in the tray menu). A program running as administrator, or a full-screen game, can still cover it.
- **The panel opened off screen or is too small.** Hide and show it again: it is moved back onto a screen. Or delete `desktop\window.json` to reset its place and size.
- **Notifications do not appear.** Check `desktop.notifications`, and Windows Settings > System > Notifications (Focus and Do not disturb also hide them).
- **I am on Linux or macOS.** The desktop app is Windows only. `stkrw` prints "the desktop app is Windows only, use `stkr serve`" and exits with code 2. Use `stkr serve` and the web UI.
