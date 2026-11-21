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
3. The `Release` workflow (`.github/workflows/release.yml`) runs on the tag. It runs the tests, then GoReleaser builds windows, linux and darwin for amd64 and arm64 with `CGO_ENABLED=0`, `-trimpath` and the version injected through `-ldflags`. It publishes the archives, `checksums.txt` and a changelog grouped by Conventional Commit type (feat, fix, docs).
4. Check the release page: 6 archives (zip for Windows, tar.gz otherwise) and `checksums.txt`. Run `stkr update --check` from an older build to see it offered.

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
