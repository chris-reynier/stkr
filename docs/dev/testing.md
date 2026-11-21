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
- No test may need a real external tool (ffmpeg, whisper, ollama, claude, copilot), the network, or a visible Windows desktop.
- Never touch the real `~/.stkr`. `testutil.TempHome(t)` creates an isolated data folder and sets `STKR_HOME`.
- Fake external programs with `testutil.BuildFakebin(t)`. The fake reads these environment variables: `FAKEBIN_STDOUT`, `FAKEBIN_STDERR`, `FAKEBIN_EXIT`, `FAKEBIN_SLEEP_MS`, `FAKEBIN_ECHO_STDIN`, `FAKEBIN_ARGS_FILE`, `FAKEBIN_STDOUT_BYTES`.
- HTTP code uses `httptest`.
- Golden files live in `testdata/` and are updated with `-update`.
- Every bug fix gets a regression test.
- Code that logs user content needs a test showing the content is not in the log.

## End-to-end tests

`internal/e2e` builds the binary once in `TestMain` and runs it against a temporary `STKR_HOME`. Each feature has a smoke test (`TestSmoke...`). The acceptance suite (`acceptance_test.go`, run with `go test -v -run Acceptance ./internal/e2e`) proves the release criteria listed in [acceptance.md](acceptance.md): a full deck-only day, duplicate imports, notes preservation, missing tools, MCP, provider switching, security checks on every route, log scanning with sentinel strings, and performance and startup budgets. Timing tests are skipped with `-short`.

## Desktop tests

`internal/desktop` is plain Go except for the `*_windows.go` files (Win32 tray, WebView2 windows), which cannot run headless. Its tests use fakes for the tray, the windows, the registry and the updater, and are held to 70% coverage **excluding `*_windows.go`** (about 91% today). `internal/desktop/edge` is a patched copy of a third-party wrapper (Windows only, no tests of its own beyond `TestWebViewFailureIsReportedNotFatal`) and is left out of the coverage numbers and of the linters.

`stkrw --headless-test` runs the real program without a notification-area icon, windows, hotkey, message boxes or browser. The e2e tests (`internal/e2e/desktop_test.go`, `TestDesktop...`) build `stkrw.exe` and drive it through a temporary `STKR_HOME` and a free port: hosting, `/api/status`, the panel round trip, second launch, `--quit`, attach mode and a database integrity check. They skip on other systems. Do not run the program without the flag in a test: it would put an icon and windows on the desktop.

Measuring: `packaging/windows/measure-desktop.ps1` (see [nfr.md](nfr.md)).

## Lint and docs checks

```
golangci-lint run
go run ./tools/gendocs && git diff --exit-code docs/user/cli
```
