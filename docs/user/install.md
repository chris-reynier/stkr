# Install STKR

## What it does

STKR is one program with no installer. You download an archive, unpack it and put the program on your PATH.

## How to use

1. Open the latest release on GitHub: `https://github.com/chris-reynier/stkr/releases`.
2. Download the archive for your system. Names look like `stkr_<version>_<os>_<arch>`:
   - Windows: `stkr_<version>_windows_amd64.zip` (or `arm64`)
   - Linux: `stkr_<version>_linux_amd64.tar.gz` (or `arm64`)
   - macOS: `stkr_<version>_darwin_arm64.tar.gz` (Apple silicon) or `darwin_amd64` (Intel)
3. Optional: check the download against `checksums.txt` from the same release.
   - Linux/macOS: `sha256sum -c checksums.txt --ignore-missing`
   - Windows (PowerShell): `Get-FileHash .\stkr_<version>_windows_amd64.zip -Algorithm SHA256`, then compare with the line in `checksums.txt`.
4. Unpack it. The archive holds `stkr` (`stkr.exe` on Windows), `README.md`, `LICENSE` and the user guide in `docs/user`.
5. Put the program on your PATH.
   - Windows: move `stkr.exe` to a folder such as `C:\Tools\stkr`, then add that folder to Path (Settings, System, About, Advanced system settings, Environment Variables).
   - Linux/macOS: `install -m 755 stkr ~/.local/bin/stkr` (make sure `~/.local/bin` is on your PATH).
6. Open a new terminal and check it: `stkr version`.
7. First run: `stkr config show` creates the data folder `~/.stkr` (or the folder in `STKR_HOME`). Then try `stkr ctx add acme --label "ACME"` and `stkr switch acme`. See [Getting started](getting-started.md).

Keep the program in a folder you can write to, so `stkr update` can replace it later (see [Update](update.md)).

## Windows SmartScreen and unsigned builds

The Windows program is not code-signed. On first launch Windows SmartScreen may say "Windows protected your PC". Choose "More info", then "Run anyway". This mostly happens for a file downloaded with a browser; running `stkr.exe` from a terminal after unpacking it usually does not ask. If you want certainty, build from source (`go build ./cmd/stkr`) or compare the SHA-256 with `checksums.txt`.

On macOS a downloaded unsigned program may be blocked by Gatekeeper. Remove the quarantine flag once: `xattr -d com.apple.quarantine stkr`.

## Configuration keys

None. The data folder is `STKR_HOME` or `~/.stkr`.

## Troubleshooting

- `stkr` is not found: open a new terminal after changing PATH, or run it with the full path.
- "Access is denied" when unpacking on Windows: unpack into a folder you own, not `C:\Program Files`.
- SmartScreen has no "Run anyway" button: your organisation blocks unsigned programs. Ask your administrator or build from source.
