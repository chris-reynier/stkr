# Portable STKR (Windows, no installer)

## What it does

A portable STKR is one folder with `stkr.exe` (the command line), `stkrw.exe` (the desktop app) and this guide. It installs nothing. There is no PATH entry, no Start menu entry, no sign-in entry and no "Installed apps" entry, and it doesn't need admin rights.

**You still get the tray icon.** It comes from `stkrw.exe`, not from the installer. The steps under "Optional setup" add the installer's extras back by hand. Skip any you don't need.

**Build it yourself** to avoid the SmartScreen prompt ("Windows protected your PC … Run anyway"). Windows only asks before running files it has marked as downloaded from the internet, and the release zip and setup are not code-signed. A program built on your own PC has no such mark, just like your other builds.

Your data (time log, ideas, settings) is not kept in the program folder. It goes in `%USERPROFILE%\.stkr` (or the folder in `STKR_HOME`), the same place the installed version uses.

## How to use

### Build

You need git and Go. Go downloads the exact toolchain named in `go.mod` if yours is older. In PowerShell:

```powershell
git clone https://github.com/chris-reynier/stkr
cd stkr
git checkout v0.2.0          # the release you want; `git tag` lists them
./packaging/windows/build-portable.ps1
```

The script builds both programs for this PC with the release flags, icon and version, and writes them to `%LOCALAPPDATA%\STKR-portable` together with the user guide. It ends by printing `stkr <version>`.

| Option | Meaning |
|---|---|
| `-Out <folder>` | build into another folder (it must be writable by you, so in-app updates work) |
| `-Arch amd64\|arm64` | target architecture (default: this PC's) |
| `-Version 0.2.0` | version to stamp (default: the git tag of the checked-out commit) |

The version comes from the git tag. If the checked-out commit has no tag, the script uses the latest tag before it and warns you. A build stamped with a release version updates like a release. An unstamped `go build` counts as a development build, and `stkr update` refuses to replace it.

To rebuild into the same folder, quit STKR first (tray icon → Quit). The script stops if STKR is running from that folder.

In this guide `<STKR>` means the build folder.

### Start it

Double-click `<STKR>\stkrw.exe`. No window opens. The STKR icon appears next to the clock. Left-click it for the panel, right-click it for the menu, or press Ctrl+Alt+Space from any program. See [Desktop app](desktop.md).

Windows 11 puts new icons in the overflow area (the `^` arrow). To keep the icon visible, go to Settings → Personalization → Taskbar → Other system tray icons and turn STKR on.

### Optional setup

**Start menu entry.** In PowerShell:

```powershell
$dir = "$env:LOCALAPPDATA\STKR-portable"     # <STKR>
$lnk = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\STKR.lnk"
$s = (New-Object -ComObject WScript.Shell).CreateShortcut($lnk)
$s.TargetPath = "$dir\stkrw.exe"; $s.WorkingDirectory = $dir; $s.Save()
```

For a desktop shortcut, use the same lines with `$lnk = "$env:USERPROFILE\Desktop\STKR.lnk"`.

**Start at sign-in.** In STKR, open Settings and tick **Start STKR when I sign in**. This writes the sign-in entry for the `stkrw.exe` that is running. If you move the folder later, untick and tick the option again.

**`stkr` in a terminal.** Add `<STKR>` to your user PATH:

```powershell
$dir = "$env:LOCALAPPDATA\STKR-portable"     # <STKR>
$p = [Environment]::GetEnvironmentVariable('Path','User')
if (($p -split ';') -notcontains $dir) { [Environment]::SetEnvironmentVariable('Path', ($p.TrimEnd(';') + ";$dir"), 'User') }
```

Open a new terminal and run `stkr version`. Then run `where stkr`: the first line must be `<STKR>\stkr.exe`. If it isn't, another copy comes first in PATH; remove that copy's folder from PATH.

**Another data folder.** Set `STKR_HOME` for your user, for example `[Environment]::SetEnvironmentVariable('STKR_HOME','D:\stkr-data','User')`. Then quit STKR and start it again from a new Explorer window or terminal. You can also start one program on another folder directly: `stkrw.exe -home D:\stkr-data`.

### Update

Use **Install update** in the tray menu or in Settings, or run `stkr update`. STKR downloads the release zip, checks its SHA-256 and replaces `stkr.exe` and `stkrw.exe` in `<STKR>`. Files STKR writes itself are not marked as downloaded, so updating does not bring back the SmartScreen prompt. You can also rebuild: `git pull`, `git checkout <new tag>`, quit STKR, then run the script again.

### Remove

1. In Settings, untick **Start STKR when I sign in**.
2. Quit STKR (tray icon → Quit).
3. Remove `<STKR>` from your user PATH (Settings → System → About → Advanced system settings → Environment Variables).
4. Delete the shortcuts and the `<STKR>` folder.

Your data folder stays. Delete `%USERPROFILE%\.stkr` only if you don't want your data any more.

### Switching from the installed version

The installed version and a portable copy share the same data folder and the same sign-in entry name, so use one or the other. To switch, quit STKR, then uninstall the installed version (Settings → Apps → Installed apps → STKR → Uninstall) and answer **No** when it asks to delete your data. Then start `<STKR>\stkrw.exe`. Your contexts and time log are still there.

## Configuration keys

None. The data folder is `STKR_HOME` or `~/.stkr`, and `desktop.autostart` works as described in [Configuration](configuration.md).

## Troubleshooting

- **"Windows protected your PC" with Run anyway, on a copy from the release zip.** The files carry the "downloaded from the internet" mark. Build the programs yourself (above), or remove the mark: tick **Unblock** in the zip's Properties before extracting. Your organisation may hide that checkbox on purpose; if so, build instead.
- **"Blocked by your system administrator" or "your organization", with no Run anyway.** A company policy blocks unsigned programs, including ones you build. Ask IT to allow `stkr.exe` and `stkrw.exe`, or the folder they are in.
- **The script says "go is not on PATH" or "git is not on PATH".** Install it, open a new terminal and run the script again.
- **The script says STKR is running.** Quit it from the tray icon menu and run the script again.
- **The script says "no git tag found".** Run `git fetch --tags`, or pass `-Version`.
- **`stkr update` says "this is a development build".** The programs were built without a version. Build them with the script instead.
- **The panel opens in the browser instead of its own window.** The Microsoft Edge WebView2 Runtime is missing; see [Troubleshooting](troubleshooting.md).
