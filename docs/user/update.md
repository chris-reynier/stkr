# Update STKR

## What it does

STKR can check GitHub Releases for a newer version and install it. The download is verified against the SHA-256 sums in the release file `checksums.txt`. If they do not match, nothing is changed. Nothing is ever installed without a click or a command from you.

You can update in three ways:

- **From the desktop app (Windows).** The tray menu and the web UI show **Install update**. STKR downloads the update, closes itself, installs it and starts again.
- **From the terminal.** `stkr update` on any system.
- **By hand.** Download the setup or archive from the release page.

## How to use

### From the desktop app

When a new version exists, the tray app shows a notification, the tray menu gets **Install update vX...**, and the web UI shows a banner with an **Install update** button. The same button is in **Settings, Updates**, next to the current and latest version, a link to the release notes and **Check now**.

1. Choose **Install update** (tray menu: a message box asks you to confirm; the button in the web page is the confirmation).
2. STKR downloads and verifies the update while it keeps running.
3. STKR stops, installs and starts again by itself. A notification says "STKR updated to X". Your data is kept.

If anything fails before the install (no network, a wrong checksum), STKR keeps running the old version and shows the reason. If the install itself fails, the old files are put back and STKR starts again.

The tray menu also has **Check for updates**, which asks GitHub right away and shows the answer in a notification.

Updating from the app works when the app runs the server itself. If the app is attached to a `stkr serve` that you started (the tray says so under About), STKR does not control that server: run `stkr update` in a terminal and restart `stkr serve`. In the same way, under a plain `stkr serve` the web page has no Install button; it tells you to run `stkr update` and restart `stkr serve`.

### From v0.1.0

v0.1.0 has no desktop app, and its `stkr update` replaces only `stkr.exe`. To get the desktop app, download the setup file of the current release (`stkr_<version>_windows_amd64_setup.exe`, or `arm64`) from the releases page and run it once. It installs `stkr.exe` and `stkrw.exe` in `%LOCALAPPDATA%ProgramsSTKR`, adds the Start menu entry and keeps your data (the data folder is the same). If you want STKR to start when you sign in, leave that task ticked. If an older `stkr.exe` from the v0.1.0 zip is still on your PATH, remove its folder from the PATH so that `where stkr` shows the new one. From then on, updates are one click from the icon or Settings. If you unpacked the zip instead of using setup, unpack the new zip over the same folder; the in-app update only adds `stkrw.exe` to a folder that setup created.

### From the terminal

```
stkr update --check     # print current and latest version; exit code 10 if an update exists
stkr update             # ask, then install the latest release
stkr update --yes       # install without asking (needed in scripts)
stkr update --version v0.1.0   # install a specific release, also an older one
stkr version            # shows "update available: vX" from the last check (no network)
```

Exit codes of `stkr update --check`: 0 up to date, 10 update available, 1 or 2 on errors (for example no network). `--json` prints the result as JSON (with `method` when something was installed).

### How the update is installed

- **Installed with the Windows setup** (the setup file from the release page). STKR runs the new `setup.exe` of the same release silently, after checking its SHA-256. The setup closes the desktop app, replaces `stkr.exe` and `stkrw.exe`, keeps your data and settings, and (when you started the update from the app) starts the app again. From the terminal the setup runs in the background and the app is not started again: start it from the Start menu.
- **Every other copy** (a zip or tar.gz you unpacked, Linux, macOS). STKR replaces every STKR program in the folder of the running program (`stkr`, and `stkrw.exe` on Windows) from the archive. It is all or nothing: if replacing the second file fails, the first one is put back. The folder must be writable by you.

STKR knows which way a copy was installed from the file `install.json` next to the programs, which only the setup writes ([contract](../contracts/install-update.md)).

Rules:

- A newer version is installed only when it is higher than the running one. An older version needs an explicit `--version`.
- A development build (built from source, version `dev`) refuses to install an update unless you add `--force`. `--check` always works and treats a development build as older than every release. The app and the web page never update a development build.
- If `stkr serve` is running, the update still installs. Restart `stkr serve` afterwards.
- On Windows a replaced program is renamed to `<program>.old` and removed the next time STKR starts.

### Channels

`update.channel` decides which releases count:

- `stable` (default): only normal releases. Pre-releases such as `v0.2.0-rc.1` are ignored. While no normal release exists, `stkr update --check` reports "no stable release found".
- `beta`: pre-releases are accepted too. The highest version wins, and a normal release beats a pre-release of the same number.

Choose the channel in **Settings, Update checks**, or:

```
stkr config set update.channel beta
```

### Background check

While STKR runs its server (the desktop app or `stkr serve`) it asks GitHub once at startup and then every `update.interval_hours`. The check has a 10 second limit, never delays startup and stays silent when it fails. The result is stored in the local database and shown as a banner: "STKR vX is available". Development builds never show the banner.

### Turn the check off

Untick **Check for updates** in **Settings, Update checks**, or:

```
stkr config set update.check false
```

With the check off STKR makes no request until you press **Check now** or run `stkr update --check`. Saved settings apply at once when the desktop app runs the server; under `stkr serve` restart it.

## Privacy

The check is one anonymous HTTPS GET to the GitHub API (`api.github.com`) listing the releases of `chris-reynier/stkr`. It sends no data about you, your machine or your usage. The archive, the setup and `checksums.txt` are downloaded only when you install an update. If you set the `GITHUB_TOKEN` environment variable it is used to avoid rate limits; it is never logged or stored. Nothing about the update is sent anywhere else.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `update.check` | `true` | Background check while STKR runs its server. |
| `update.channel` | `stable` | `stable` or `beta` (pre-releases). |
| `update.interval_hours` | `24` | Hours between background checks. |

## Troubleshooting

- "checksum mismatch": the download was damaged or changed. Nothing was installed. Try again; if it repeats, download from the release page by hand.
- "has no build for <os>/<arch>": the release has no archive for your system.
- "server answered 403": GitHub rate limited you. Set `GITHUB_TOKEN` or wait.
- "move current executable aside" or "is the folder writable": the program folder is not writable. Use a user that owns it, or install by hand.
- "development build": you built from source. Use `--force` if you really want to replace it with `stkr update`.
- "STKR is attached to `stkr serve`": the tray app does not run the server. Run `stkr update` and restart `stkr serve`.
- The update failed in the app: the notification names the reason and STKR keeps the old version. The log `logs/desktop.log` has the kind of the error, never your data.
- The app did not come back after an update: start it from the Start menu. If it does not start, run the setup file of the release by hand.
- SmartScreen asks about the setup: the setup is not code-signed. The update checks its SHA-256 before starting it.
