# STKR v0.2.0

STKR now has a Windows desktop app. After one installer there is an STKR icon next to the clock: click it to switch projects, capture an idea, start and close your day, and open the full interface in its own window. You no longer need a terminal on Windows. The command line, the local web page, the Stream Deck support and the MCP server work as before.

## Highlights

- **Windows installer.** `stkr_<version>_windows_amd64_setup.exe` (and `arm64`) installs for your user only: no administrator rights, `stkr` on your PATH, a Start menu entry, an optional start at sign-in and an optional desktop shortcut. Uninstall keeps your data unless you answer Yes twice.
- **Notification-area icon.** The icon shows what runs (grey, blue, purple, green, amber for an idle-suspect block, a red badge when the close of the day is due), the tooltip shows the context and the time on it, and the right-click menu has the daily actions. It shows a notification when the close is due, an update is found or an agent waits for you.
- **Always-on-top panel.** Left-click the icon, or press Ctrl+Alt+Space from any program: switch project, stop, capture an idea (Enter saves it), Start day, Close day, Import from clipboard, agents waiting. It stays above maximized windows, can be unpinned, and remembers its place and size.
- **App window.** "Open STKR" shows Today, Week, Agents, Ideas, Links and Settings in a normal window with a taskbar button. The panel and the window use the Microsoft Edge WebView2 Runtime and show the same web UI you get in a browser; without the runtime STKR uses your browser.
- **Update from the app.** The icon's menu, a banner and Settings show **Install update** when a new version exists. STKR downloads it, checks its SHA-256, stops itself, installs it and starts again. Setup-installed copies run the new setup silently; zip copies have all their programs replaced, all or nothing. It never installs by itself without a click.
- **Settings apply at once.** When the desktop app runs the server, saving Settings restarts the server in a couple of seconds and applies the hotkey, refresh interval, notifications and pin without quitting. Settings also has Back up now, Open data folder and **Start STKR when I sign in**.
- **One server, two ways in.** The desktop app runs the same server as `stkr serve` inside its own process. If `stkr serve` is already running it attaches to it instead. Stream Deck keys, `stkr` commands and the web page work while only the icon runs.

## Hardening in this release

- A window that cannot start no longer ends the whole program. The WebView2 wrapper we used called `log.Fatalf` when the web view could not be created, which would have closed the tray icon and the server with it. STKR now keeps running, tells you once and opens its pages in your browser. Fixing it also removed a second defect in the wrapper (a failed start was never recognised and would have crashed).
- The windows cannot leave your own server: a link to another site opens in your default browser, a page cannot open extra windows, and a navigation to `file:`, `data:` or another port is cancelled.
- New acceptance tests run the real `stkrw.exe` in a headless test mode: it hosts the server, `/api/status` follows a CLI switch, the panel switch, idea and stop work, a second launch signals the first and exits, `--quit` stops it gracefully and the database passes `integrity_check`, and attach mode leaves a running `stkr serve` alone.
- The route-table security test now also fails if a route is registered in a way it cannot see, and static tests check that the installer stays per-user (HKCU only, no machine-wide paths, data deletion opt-in) and that no new code makes outbound HTTP requests.
- When a window fails to start the notification now says so, instead of claiming WebView2 is missing.

## Upgrading

- **From v0.1.0 on Windows: run the setup file once.** `stkr update` in v0.1.0 replaces only `stkr.exe`, so it cannot add the desktop app. Download the setup file of this release and run it; it keeps your data and uses the same data folder (`%USERPROFILE%\.stkr`). If you had `stkr.exe` from the zip on your PATH, remove that folder from your PATH. After this, updates are one click. See [Update](../user/update.md#from-v010).
- **Linux and macOS:** unchanged. `stkr update` works as before; there is no desktop app on these systems.
- Your data, `config.yaml` and the HTTP and MCP interfaces are compatible. New settings (`desktop.*`) have defaults.

## New and changed interfaces

- `GET /api/status`, `GET /ui/panel`, `GET /ui/panel/live`, `POST /ui/import/clipboard`, `POST /ui/update/check`, `POST /ui/update/apply`, `POST /ui/settings/backup`, `POST /ui/settings/open-data`. See [status API](../contracts/status-api.md), [desktop windows](../contracts/desktop-windows.md) and [install and update](../contracts/install-update.md).
- Settings keys `desktop.notifications`, `desktop.poll_seconds`, `desktop.panel_on_top`, `desktop.hotkey`, `desktop.open_panel_on_start`, `desktop.autostart` ([Configuration](../user/configuration.md)).
- Release assets: the Windows archive holds `stkr.exe` and `stkrw.exe`, and each Windows architecture has a setup file; all are in `checksums.txt` ([release assets](../contracts/release-assets.md)).
- No new outbound network calls. The only ones remain the configured AI provider, the bookmark title fetch and the update check.

## Quality evidence

- [Acceptance matrix](acceptance.md), including the new desktop section and manual checks M10 to M16.
- [Security review](security-review.md), addendum for the desktop app, installer and in-app update.
- [Performance numbers](nfr.md#desktop-app-v020): with the panel closed the app starts its tray in about 0.05 s and uses about 20 MB (working set); the panel-open numbers are measured by the CI job and manual check M16.
- Desktop code is tested with fakes (the Win32 and WebView2 layers are excluded from the 70% coverage rule because they cannot run headless): `internal/desktop` is at 91% without them.

## Known limitations

- **Unsigned programs and setup.** SmartScreen warns on first run (More info, Run anyway). Code signing, winget and scoop packages are planned.
- **Needs a manual check on a real desktop:** the notification-area icon and menu, the panel and its hotkey, window positions on several monitors, the interactive installer and uninstaller, the in-app update with two real releases, and the panel-open memory number. Procedures are manual checks M10 to M16 in the [acceptance matrix](acceptance.md#manual-checks). The tray icon, WebView2 windows, hotkey and installer wizard had only been compiled, never run on a desktop, when this release was prepared; the automated tests cover everything around them.
- Updates are never installed silently in the background; an option for that is planned.
- A restart that applies Settings stops a running voice recording.
- `stkr update` from a terminal on a setup-installed copy runs the setup in the background and does not start the tray app again.
- The desktop app and installer are Windows only. A tray icon for Linux and macOS is on the backlog.
- The windows are not tied to the exact icon position (the panel opens beside the mouse pointer).
- The v0.1.0 limits that still apply: GitHub Copilot defaults are unverified, no live refresh of the Agents and Ideas pages, no Jira integration.
