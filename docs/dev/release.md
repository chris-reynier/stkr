# Releasing STKR

## Version policy

Versions follow semantic versioning and stay on `0.x` until the interfaces settle. The first public release is `v0.1.0`. Pre-release tags (`v0.2.0-rc.1`) are published as GitHub pre-releases automatically (`release.prerelease: auto`) and are only offered to users on the `beta` update channel.

## Cut a release

1. Make sure `main` is green in CI, including the "release config" job.
2. Tag and push:
   ```
   git checkout main && git pull
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```
3. The `Release` workflow (`.github/workflows/release.yml`) runs on the tag on `windows-latest` (Inno Setup is preinstalled there). It runs the tests, then GoReleaser builds `stkr` for windows, linux and darwin (amd64, arm64) and `stkrw` (the Windows desktop app, `-H windowsgui`) for windows, with `CGO_ENABLED=0`, `-trimpath` and the version injected through `-ldflags`. Windows resources (icon, version info) are generated in `before.hooks` by `tools/genicon` and `go tool go-winres`. GoReleaser creates the release as a **draft** with the archives, `checksums.txt` and a changelog grouped by Conventional Commit type. Then `packaging/windows/build-setups.ps1 -Checksums` compiles `stkr_<version>_windows_<arch>_setup.exe` for each Windows architecture with `iscc` and appends their SHA-256 lines to `checksums.txt`; `gh release upload --clobber` attaches them and `gh release edit --draft=false` publishes. If a step fails the release stays a draft and users never see it.
4. Check the release page: 6 archives (zip for Windows, tar.gz otherwise), 2 `_setup.exe` files and `checksums.txt` listing all 8. The Windows zips hold `stkr.exe` and `stkrw.exe`. Run `stkr update --check` from an older build to see it offered.

Archive names are fixed: `stkr_<version>_<os>_<arch>.zip` or `.tar.gz` (version without the `v`). The updater (`internal/update`) selects the asset by that name and verifies it against `checksums.txt`, so do not rename assets or that file.

## Try the config locally

```
goreleaser check
goreleaser build --snapshot --clean --single-target
```

CI runs both on every pull request and fails when the binary is 30 MB or larger.

## If a release is wrong

Delete the GitHub release and the tag (`git push origin :refs/tags/vX.Y.Z`), fix the problem and tag again with a new patch version. Users who already updated keep working; `stkr update --version vX.Y.Z` can move them.

## Changelog policy

There is no hand-written `CHANGELOG.md`. The changelog of each GitHub release is generated from the Conventional Commit messages of the squashed pull requests (`feat`, `fix`, `docs`; `test`, `ci` and `chore` are left out), so write the pull request title as the line you want users to read. For a release with something to explain, add `docs/dev/release-notes-vX.Y.Z.md` (highlights, known limitations, manual checks) before tagging and link it from the release description.

## Windows installer

- Script: `packaging/windows/stkr.iss` (Inno Setup 6). `packaging/windows/build-setups.ps1` reads GoReleaser's `dist/artifacts.json` and `dist/metadata.json`, writes `packaging/windows/build.generated.iss` (gitignored, holds the version and file paths) and runs `iscc`. The `AppId` GUID must never change; it is recorded in `docs/contracts/release-assets.md`.
- Try it on a Windows machine with Inno Setup 6 installed: `goreleaser release --snapshot --clean --skip=publish`, then `./packaging/windows/build-setups.ps1 -Checksums`. Do not run `smoke-test.ps1` on a working desktop: it installs and uninstalls for the current user and briefly changes the real user PATH. CI runs it on every pull request (job "windows installer").
- The icon (`packaging/windows/stkr.ico`) and the `.syso` resource files are generated and gitignored; the icon comes from the same drawing code as the tray icon (`go run ./tools/genicon`).
- Everything is unsigned. SmartScreen warns on first run. Code signing and winget/scoop manifests are backlog items.
