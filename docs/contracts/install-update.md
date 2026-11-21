# Install and update (v1)

Defines how a STKR copy knows how it was installed, how an update is installed for each kind of copy, the program arguments the update flow uses, and the sign-in entry. It extends [release-assets.md](release-assets.md) (what a release contains) and [desktop-windows.md](desktop-windows.md). Version 1.

## `install.json`

Written by the Windows setup next to the programs (`<install folder>\install.json`). Nothing else writes it. The updater only reads it.

```json
{"method":"setup","version":"0.2.0"}
```

| Field | Type | Meaning |
|---|---|---|
| `method` | string | `setup`: this folder was installed by the setup. Any other value, or a missing field, counts as "not installed by the setup". |
| `version` | string | The version the setup installed (informational). |

Rules: the file is at most 4 KB, UTF-8 JSON, unknown fields are ignored. A missing, unreadable or invalid file means a copy that was not installed by the setup (zip or tar.gz, built from source). The uninstaller removes the file. The updater looks for the file next to the program that runs the update (`stkr.exe` or `stkrw.exe`).

## Choosing the update method

For a release `R` and the program that runs the update (`<dir>\stkr.exe` or `<dir>\stkrw.exe`):

1. **setup**: the platform is Windows, `<dir>\install.json` says `"method":"setup"`, and `R` has the asset `stkr_<version>_windows_<arch>_setup.exe` for this architecture.
2. **replace**: in every other case.

A setup-installed copy whose release has no setup file falls back to replace.

## Method `setup`

1. Download `checksums.txt` and the setup; the SHA-256 of the setup must equal its line in `checksums.txt`. A missing file, a missing line or a mismatch aborts the update and nothing is started.
2. Write the setup to a new temporary folder (`stkr-update-*` in the system temp folder, file mode user only).
3. Start it detached (it outlives the caller) with these arguments, in this order:

| Argument | Meaning |
|---|---|
| `/VERYSILENT` | no windows |
| `/SUPPRESSMSGBOXES` | no message boxes |
| `/NORESTART` | never restart Windows |
| `/CURRENTUSER` | per-user install (the setup is per-user anyway) |
| `/RELAUNCH=1` | only when the update was started from the desktop app: start `stkrw.exe --after-update` when done |

The setup asks a running `stkrw.exe` to quit (`stkrw.exe --quit`, waits at most 5 seconds), replaces `stkr.exe` and `stkrw.exe`, rewrites `install.json`, keeps the data folder, the PATH entry and the sign-in entry choice. With `/RELAUNCH=1` **and** a silent run it then starts `<install folder>\stkrw.exe --after-update`. A wizard run, and a silent run without `/RELAUNCH=1` (for example `stkr update` from a terminal), start nothing.

The caller does not wait for the setup and exits right after starting it (the desktop app stops its server first). A setup that cannot be started is reported as an error and the temporary folder is removed.

## Method `replace`

1. Download the archive for the platform and verify its SHA-256 against `checksums.txt` (rules in release-assets.md).
2. Extract every STKR program from the archive: `stkr`/`stkr.exe` and `stkrw.exe`. Other files are ignored.
3. Decide the target files:
   - The program kind of the running program (`stkr` or `stkrw`) replaces the running program's own file, whatever it is called.
   - Every other STKR program replaces the file of the same name in the same folder if it exists there.
   - A program that does not exist in the folder is installed only when the folder has an `install.json` (it is the install folder). A plain unpacked folder only gets the programs it already has.
   - The archive must contain the program kind of the running program, else the update is refused.
4. Write every new file next to its target (temporary name `.stkr-update-*`, fsync, same permissions plus execute).
5. Commit, all or nothing: for each file in turn, rename the current file to `<file>.old`, then rename the new file into place. If any rename fails, every change already made is undone (files moved back, newly installed programs removed) and the error names the step. The error text says "previous version restored" when the rollback worked.
6. After a commit: non-Windows systems delete the `.old` files at once. Windows keeps them (a running program cannot be deleted) and every `stkr` and `stkrw` start removes the `.old` files of both programs in its folder.

Nothing is changed before step 5, so a failed download, a bad checksum, a missing file or an unwritable folder leaves the installed version untouched.

## Program arguments

`stkrw.exe` (all optional):

| Argument | Meaning |
|---|---|
| `-home <dir>` | data folder (default `STKR_HOME` or `~/.stkr`) |
| `--quit` | ask the running app of this data folder to quit, then exit (exit code 0 even if none runs) |
| `--autostart` | marker of the sign-in entry; no effect |
| `--after-update` | this instance was started by an update: shows "STKR updated to X" once when it is running |

After a `replace` update the app starts the new `stkrw.exe --after-update` itself, after it released the single-instance lock, then exits. After a `setup` update the setup starts it (see above).

## Sign-in entry (Run key)

| Item | Value |
|---|---|
| Key | `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run` |
| Value name | `STKR` |
| Type | `REG_SZ` |
| Data | `"<folder>\stkrw.exe" --autostart` (the path is quoted) |
| Written by | the setup (task "Start STKR when I sign in"), and Settings, Desktop app, "Start STKR when I sign in" |
| Removed by | the uninstaller, the setup when the task is not selected, and Settings |

Settings turns it on by writing the value with the path of the desktop program next to the running program (an existing value that points elsewhere is replaced), and off by deleting the value; deleting a missing value is not an error. The state shown in Settings is whether the value exists. Systems other than Windows have no such entry; the operation fails with an "unavailable" error. The config key `desktop.autostart` only mirrors the last choice made in Settings and is never read to decide the state.

## Web UI routes

All are `POST` form actions with the usual Host and Origin guards, answered with `303` to the `next` field (a local `/ui/` path) and a one-time message.

| Route | Effect | Errors (shown as the message) |
|---|---|---|
| `POST /ui/update/check` | Asks GitHub for the latest release (30 s limit) whatever `update.check` says, stores the result | no network, no release found |
| `POST /ui/update/apply` | Asks the desktop app that runs the server to install the pending update; returns at once, the app downloads, stops the server, installs and starts again | not hosted by the desktop app (`stkr serve`): "run `stkr update` and restart `stkr serve`"; no update is waiting; development build; an update is already running |
| `POST /ui/settings/backup` | Writes a backup now (same as `stkr backup`) | backup errors |
| `POST /ui/settings/open-data` | Opens the data folder in the file manager | not hosted by the desktop app |
| `POST /ui/settings/config` | Saves settings; hosted by the desktop app, the server then restarts in the background on the same port (a changed `port` takes the new port) and the page reloads itself | invalid values change nothing and restart nothing |

"Hosted by the desktop app" is `hosted_by: "desktop"` in `GET /api/status` ([status-api.md](status-api.md)). `desktop.*` keys posted to `POST /ui/settings/config` are ignored when the server is not hosted by the desktop app. `stkr update` and `stkr update --check` keep their output and exit codes ([release-assets.md](release-assets.md)); `stkr update --json` adds `method` (`replace` or `setup`) after an install.

## Errors

| Situation | Result |
|---|---|
| checksum mismatch, missing `checksums.txt` or line | update refused, nothing changed |
| archive without the running program | update refused, nothing changed |
| program folder not writable | update refused before any change |
| a rename fails during the commit | all changes undone, error returned, the desktop app starts its server again |
| the setup cannot be started | error, temporary folder removed |
| development build (`dev`) | refused by the app and the web UI; `stkr update` needs `--force` |
| desktop app attached to a `stkr serve` | refused with a hint to run `stkr update` and restart `stkr serve` |
