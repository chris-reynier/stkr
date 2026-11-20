# Contributing

## Build, test, lint

You need Go 1.27 or newer and [golangci-lint](https://golangci-lint.run) v2.

```
CGO_ENABLED=0 go build ./...
go vet ./...
golangci-lint run
go test ./...
go run ./tools/gendocs    # regenerate docs/user/cli; commit the result
```

Check other platforms compile: `GOOS=linux CGO_ENABLED=0 go build ./...` and the same for `darwin` and `windows`.

Tests must not need a real external tool, network access or a desktop. Use `internal/testutil` (`TempHome`, `BuildFakebin`) and `clock.Fake`. Never point tests at your real `~/.stkr`.

## Code rules

- Layering is described in [docs/dev/architecture.md](docs/dev/architecture.md). Only `internal/service` may import `internal/store`; the linter enforces it.
- Call `time.Now()` only inside `internal/clock`.
- Do not log clipboard text, prompts, AI output, transcriptions, idea text or secrets. Log lengths and ids (`logx.Redact`).
- Write files atomically (temp file, sync, rename).
- Add a dependency only when it is needed, and only with a permissive license (MIT, BSD, Apache-2.0, ISC).
- Update user docs (`docs/user`) and contracts (`docs/contracts`) with the code.

## Commits and pull requests

Use [Conventional Commits](https://www.conventionalcommits.org): `feat(api): ...`, `fix(store): ...`, `docs: ...`, `test: ...`, `ci: ...`. Keep commits small. Open a pull request against `main`; CI must pass on Windows and Linux.
