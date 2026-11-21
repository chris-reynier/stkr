# Release assets and update check (v2)

Defines what a GitHub release must contain for `stkr update` to install it, what the Windows setup file installs, and the machine-readable output of `stkr update`. v2 adds the Windows setup files and the desktop program; everything in v1 still holds.

## Release layout

Tag: `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-<prerelease>`. Pre-release tags must be marked as pre-releases on GitHub. Drafts and tags that are not versions are ignored. A release is created as a draft by the release workflow and published only after every asset below is attached.

Assets, per platform (`<version>` is the tag without `v`; `<os>` is `windows`, `linux` or `darwin`; `<arch>` is `amd64` or `arm64`):

| Asset | Content |
|---|---|
| `stkr_<version>_<os>_<arch>.zip` (windows) or `.tar.gz` (others) | `stkr` / `stkr.exe`, on Windows also `stkrw.exe`; `README.md`, `LICENSE`, `docs/user/...` |
| `stkr_<version>_windows_<arch>_setup.exe` (windows only) | per-user installer, see below |
| `checksums.txt` | one line per archive and per setup file: `<sha256 hex>  <asset name>` |

The updater picks the archive by exact name, reads the checksum for that name from `checksums.txt`, compares it with the SHA-256 of the downloaded file and only then extracts the STKR programs (`stkr`/`stkr.exe` and, on Windows, `stkrw.exe`). How they are installed, and the setup path for copies installed by the setup, are in [install-update.md](install-update.md). A `*_setup.exe` is never chosen as the archive. Downloads must use https (plain http is accepted for loopback only).

## Windows setup (`*_setup.exe`)

Built with Inno Setup 6 from `packaging/windows/stkr.iss`. Unsigned.

| Item | Value |
|---|---|
| AppId | `{6F3A9C2E-8B14-4D75-A1E0-3C5D7B92F4A8}` (fixed forever; Windows uses it to find an earlier install for upgrade and uninstall) |
| Privileges | per user (`PrivilegesRequired=lowest`), no admin rights |
| Install folder | `%LOCALAPPDATA%\Programs\STKR` (the user may change it) |
| Files | `stkr.exe`, `stkrw.exe`, `LICENSE`, `README.md`, `docs\user\**`, `install.json`, `unins000.exe`/`.dat` |
| Architecture | amd64 setup needs 64-bit x86 or ARM64 with emulation; arm64 setup needs ARM64 |
| PATH | the install folder is appended to `HKCU\Environment\Path` (REG_EXPAND_SZ) if no entry equals it (case-insensitive, trailing `\` ignored); uninstall removes only that entry |
| Start menu | `Programs\STKR` shortcut to `stkrw.exe`, AppUserModelID `STKR.Desktop` |
| Optional tasks | `autostart` (default on), `desktopicon` (default off); upgrades keep the user's choice |
| Sign-in entry | task `autostart`: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value `STKR` = `"<dir>\stkrw.exe" --autostart` |
| Uninstall entry | Settings, Apps; uninstall removes the files, the shortcuts, the Run value and the PATH entry |
| Data folder | `STKR_HOME` or `%USERPROFILE%\.stkr`: never written by setup, kept by uninstall unless the user answers Yes twice in the interactive uninstaller. A silent uninstall always keeps it |

Silent switches: `/VERYSILENT /SUPPRESSMSGBOXES /NORESTART`, `/TASKS="autostart,desktopicon"` (empty `/TASKS=""` selects none), `/DIR="path"`, `/LOG="file"`.

Before installing over a running copy, setup runs `stkrw.exe --quit` (the app for the default data folder quits) and waits at most 5 seconds.

### `install.json`

Written to the install folder by setup and removed by uninstall. It tells the updater that this copy came from a setup file.

```json
{"method":"setup","version":"0.2.0"}
```

`method` is `setup`; a copy without this file was unpacked from a zip or built from source. `version` is the version of the setup that wrote it.

## `stkr update --check --json`

```json
{ "current": "v0.1.0", "latest": "v0.2.0", "available": true, "url": "https://github.com/chris-reynier/stkr/releases/tag/v0.2.0" }
```

Exit code: `0` up to date, `10` update available, `1` error, `2` usage or unavailable (for example network down).

`stkr update --yes --json` adds `"applied": true`, `"restart_serve": <bool>` and `"executable": "<path>"`.

## Errors

| Condition | Result |
|---|---|
| Network failure, non-200 answer | exit 2 (`unavailable`), message without URLs or tokens |
| No release on the channel | exit 1 (`not_found`); on `stable` the message suggests `update.channel beta` |
| No archive for this OS/arch | exit 1 (`not_found`) |
| `checksums.txt` missing, no entry, or SHA-256 differs | exit 2 (`invalid`), current executable untouched |

## Cached state

`stkr serve` stores the last check in the database key `update.latest` as `{"latest","url","checked"}`. `stkr version` reads it without network access.
