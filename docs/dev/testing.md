# Testing

## Running

```
go test ./...
go test -race ./...            # needs CGO, run on Linux/macOS
go test -cover ./internal/...
```

Coverage targets: overall at least 70%; `internal/timeline` and `internal/service` at least 85%.

## Rules

- Tests are table-driven and sit next to the code. Use `t.TempDir()` for files and databases.
- Use `clock.Fake` for time. Never call `time.Now()` in code under test.
- No test may need a real external tool (ffmpeg, whisper, ollama, claude, copilot), the network, or the Windows desktop.
- Never touch the real `~/.stkr`. `testutil.TempHome(t)` creates an isolated data folder and sets `STKR_HOME`.
- Fake external programs with `testutil.BuildFakebin(t)`. The fake reads these environment variables: `FAKEBIN_STDOUT`, `FAKEBIN_STDERR`, `FAKEBIN_EXIT`, `FAKEBIN_SLEEP_MS`, `FAKEBIN_ECHO_STDIN`, `FAKEBIN_ARGS_FILE`.
- HTTP code uses `httptest`.
- Golden files live in `testdata/` and are updated with `-update`.
- Every bug fix gets a regression test.
- Code that logs user content needs a test showing the content is not in the log.

## End-to-end tests

`internal/e2e` builds the binary once in `TestMain` and runs it against a temporary `STKR_HOME`. Each feature adds a scenario to the smoke test.

## Lint and docs checks

```
golangci-lint run
go run ./tools/gendocs && git diff --exit-code docs/user/cli
```
