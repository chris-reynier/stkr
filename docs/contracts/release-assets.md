# Release assets and update check (v1)

Defines what a GitHub release must contain for `stkr update` to install it, and the machine-readable output of `stkr update`.

## Release layout

Tag: `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-<prerelease>`. Pre-release tags must be marked as pre-releases on GitHub. Drafts and tags that are not versions are ignored.

Assets, per platform (`<version>` is the tag without `v`; `<os>` is `windows`, `linux` or `darwin`; `<arch>` is `amd64` or `arm64`):

| Asset | Content |
|---|---|
| `stkr_<version>_<os>_<arch>.zip` (windows) or `.tar.gz` (others) | `stkr` / `stkr.exe`, `README.md`, `LICENSE`, `docs/user/...` |
| `checksums.txt` | one line per archive: `<sha256 hex>  <asset name>` |

The updater picks the asset by exact name, reads the checksum for that name from `checksums.txt`, compares it with the SHA-256 of the downloaded archive and only then extracts the first regular file named `stkr` or `stkr.exe`. Downloads must use https (plain http is accepted for loopback only).

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
