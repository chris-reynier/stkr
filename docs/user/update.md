# Update STKR

## What it does

STKR can check GitHub Releases for a newer version and replace itself. The download is verified against the SHA-256 sums in the release file `checksums.txt`. If they do not match, nothing is changed.

`stkr serve` can also tell you when a new version exists (a banner in the web UI). It never installs anything on its own.

## How to use

```
stkr update --check     # print current and latest version; exit code 10 if an update exists
stkr update             # ask, then install the latest release
stkr update --yes       # install without asking (needed in scripts)
stkr update --version v0.1.0   # install a specific release, also an older one
stkr version            # shows "update available: vX" from the last check (no network)
```

Exit codes of `stkr update --check`: 0 up to date, 10 update available, 1 or 2 on errors (for example no network). `--json` prints the result as JSON.

Rules:

- A newer version is installed only when it is higher than the running one. An older version needs an explicit `--version`.
- A development build (built from source, version `dev`) refuses to install an update unless you add `--force`. `--check` always works and treats a development build as older than every release.
- If `stkr serve` is running, the update still installs. Restart `stkr serve` afterwards.
- On Windows the running `stkr.exe` is renamed to `stkr.exe.old`, the new file is written, and the old file is deleted the next time stkr starts. If writing fails, the old file is put back.
- The folder that holds the program must be writable by you.

### Channels

`update.channel` decides which releases count:

- `stable` (default): only normal releases. Pre-releases such as `v0.2.0-rc.1` are ignored. While no normal release exists, `stkr update --check` reports "no stable release found".
- `beta`: pre-releases are accepted too. The highest version wins, and a normal release beats a pre-release of the same number.

```
stkr config set update.channel beta
```

### Background check

While `stkr serve` runs it asks GitHub once at startup and then every `update.interval_hours`. The check has a 10 second limit, never delays startup and stays silent when it fails. The result is stored in the local database and shown as a banner: "STKR vX is available - run `stkr update`". Development builds never show the banner.

### Turn the check off

```
stkr config set update.check false
```

Restart `stkr serve` for it to take effect. `stkr update --check` still works when you run it yourself.

## Privacy

The check is one anonymous HTTPS GET to the GitHub API (`api.github.com`) listing the releases of `chris-reynier/stkr`. It sends no data about you, your machine or your usage. The archive and `checksums.txt` are downloaded only when you run `stkr update`. If you set the `GITHUB_TOKEN` environment variable it is used to avoid rate limits; it is never logged or stored.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `update.check` | `true` | Background check while `serve` runs. |
| `update.channel` | `stable` | `stable` or `beta` (pre-releases). |
| `update.interval_hours` | `24` | Hours between background checks. |

## Troubleshooting

- "checksum mismatch": the download was damaged or changed. Nothing was installed. Try again; if it repeats, download from the release page by hand.
- "has no build for <os>/<arch>": the release has no archive for your system.
- "server answered 403": GitHub rate limited you. Set `GITHUB_TOKEN` or wait.
- "move current executable aside": the program folder is not writable. Use a user that owns it, or unpack the archive by hand.
- "development build": you built from source. Use `--force` if you really want to replace it.
