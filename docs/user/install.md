# Install STKR

## What it does

On Windows, one setup file installs STKR for your user account: the `stkr` command, the STKR desktop app (the icon in the notification area), a Start menu entry, and `stkr` on your PATH for new terminals. No administrator rights are needed. On Linux and macOS you unpack an archive and put `stkr` on your PATH.

## How to use

### Windows (recommended)

1. Open the latest release on GitHub: `https://github.com/chris-reynier/stkr/releases`.
2. Download `stkr_<version>_windows_amd64_setup.exe`. On an ARM PC (for example a Surface Pro X) download `stkr_<version>_windows_arm64_setup.exe` instead.
3. Double-click the file and follow the steps. Two options are on the "Select Additional Tasks" page:
   - "Start STKR when I sign in to Windows" (on by default).
   - "Create a desktop shortcut" (off by default).
4. Leave "Launch STKR" ticked on the last page. The STKR icon appears next to the clock. See [Desktop app](desktop.md).
5. Open a new terminal and check it: `stkr version`.

Windows SmartScreen may say "Windows protected your PC", because the file is not code-signed yet. Choose "More info", then "Run anyway". If there is no "Run anyway" button, your organisation blocks unsigned programs; ask your administrator or use the zip below.

What gets installed, and where:

| What | Where |
|---|---|
| `stkr.exe`, `stkrw.exe`, the user guide, licence | `%LOCALAPPDATA%\Programs\STKR` |
| PATH entry for that folder | your user PATH only (not the system PATH) |
| Start menu entry "STKR" | Start menu, Programs |
| Sign-in entry (only if ticked) | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value `STKR` |
| Your data (time log, ideas, settings) | `%USERPROFILE%\.stkr`, created on first run, never touched by setup |

Install a newer setup file over an older install to upgrade. Setup closes the running STKR app first, keeps the choices you made, and keeps your data. You can also update from inside STKR, see [Update](update.md).

Coming from v0.1.0, which had no desktop app? Run the setup file once: `stkr update` in v0.1.0 replaces only `stkr.exe`. See [Update](update.md#from-v010).

### Uninstall on Windows

Open Settings, Apps, Installed apps, find STKR and choose Uninstall. It removes the programs, the Start menu entry, the sign-in entry and the PATH entry. Your data folder is kept. Uninstall asks whether you also want to delete your data; the default is No, and Yes asks a second time because deleting cannot be undone. A silent uninstall (`unins000.exe /VERYSILENT`) always keeps your data.

### Windows without the installer (zip)

1. Download `stkr_<version>_windows_amd64.zip` (or `arm64`).
2. Unpack it. It holds `stkr.exe` (the command), `stkrw.exe` (the desktop app), `README.md`, `LICENSE` and the user guide in `docs/user`.
3. Put both programs in a folder you can write to, for example `C:\Tools\stkr`, and add that folder to Path (Settings, System, About, Advanced system settings, Environment Variables).
4. Open a new terminal and run `stkr version`.

If SmartScreen asks before every run of the unpacked programs, or you want a copy with no installer that you build yourself, see [Portable](portable.md).

### Linux and macOS

1. Download the archive for your system: `stkr_<version>_linux_amd64.tar.gz` (or `arm64`), `stkr_<version>_darwin_arm64.tar.gz` (Apple silicon) or `darwin_amd64` (Intel).
2. Unpack it and put the program on your PATH: `install -m 755 stkr ~/.local/bin/stkr` (make sure `~/.local/bin` is on your PATH).
3. Open a new terminal and run `stkr version`.
4. On macOS a downloaded unsigned program may be blocked by Gatekeeper. Remove the quarantine flag once: `xattr -d com.apple.quarantine stkr`.

### Check a download (optional)

Each release has a `checksums.txt` with a SHA-256 for every file, setup files included.

- Windows (PowerShell): `Get-FileHash .\stkr_<version>_windows_amd64_setup.exe -Algorithm SHA256`, then compare it with the line in `checksums.txt`.
- Linux/macOS: `sha256sum -c checksums.txt --ignore-missing`

### First run

`stkr config show` creates the data folder `~/.stkr` (or the folder in `STKR_HOME`). Then try `stkr ctx add acme --label "ACME"` and `stkr switch acme`. See [Getting started](getting-started.md).

## Configuration keys

None. The data folder is `STKR_HOME` or `~/.stkr`.

## Troubleshooting

- `stkr` is not found after installing: open a new terminal (an already open one keeps the old PATH), or run it with the full path `%LOCALAPPDATA%\Programs\STKR\stkr.exe`.
- SmartScreen has no "Run anyway" button: your organisation blocks unsigned programs. Ask your administrator, use the zip, or build from source (`go build ./cmd/stkr ./cmd/stkrw`).
- Setup says STKR is still running: close it from the tray icon menu (Quit) and run setup again, or let setup close it.
- You also have a copy from a zip on your PATH: the one that comes first in PATH wins. Run `where stkr` to see which.
- "Access is denied" when unpacking the zip on Windows: unpack into a folder you own, not `C:\Program Files`.
