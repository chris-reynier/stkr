# Performance and size numbers (v0.1.0)

Targets: deck POST under 100 ms, key image GET under 50 ms, idle memory under 50 MB, binary under 30 MB, startup that does not depend on the network, no CGO.

Measured on 2026-11-21 on Windows 11 (x64) with Go 1.27.4, on the commit prepared for v0.1.0. Numbers are from one machine and one session; they show the margin, not a guarantee. The automated budgets in the test suite use the targets above, not these numbers.

## Response times

Real HTTP round trips against the running binary on loopback (`go test -v -run AcceptancePerfBudgets ./internal/e2e`; 200 requests after a 30-request warm-up; deck header set):

| Request | p95 | Target |
|---|---|---|
| `POST /api/switch/{ctx}` (alternating two contexts) | 1.1 ms | < 100 ms |
| `GET /img/now.png` (key image, changes every call) | 2.3 ms | < 50 ms |
| `GET /api/now` | 2.7 ms | < 100 ms (not a stated target) |

In-process (no network stack), `go test -v -run TestPerfBudgets ./internal/httpapi`: POST switch p95 1.0 ms; `now.png` p95 7.4 ms with a new image rendered for every request (cached images are faster). The agent event endpoint (`httpapi/TestAgentEventPerf`) is also well under 100 ms. Both timing tests take the best of three runs after a warm-up, so a short pause on a busy machine does not fail the build, and they are skipped with `-short` and under the race detector.

## Idle memory

`stkr serve --port 0` with a fresh data folder and the default settings (update check on), measured with the Windows build from the release snapshot after 60 seconds idle:

| Measure | Value |
|---|---|
| Working set (what Task Manager shows as Memory) | 23.3 to 23.7 MB in five runs |
| Private bytes (committed memory) | 51 MB |
| Startup to the "serving on" line | 88 to 173 ms |

One earlier run, started seconds after the file was created by the build, showed a working set of 66.7 MB at 60 seconds. It did not repeat in five later runs and is most likely the operating system scanning the new file, but the cause was not confirmed. If you see a number above 50 MB on your machine, measure again after the first start.

Measure it yourself:

- Windows (PowerShell): start `stkr serve`, wait 60 seconds, then `Get-Process stkr | Select-Object WorkingSet64,PrivateMemorySize64` or `tasklist /FI "IMAGENAME eq stkr.exe"`.
- Linux: start `stkr serve`, wait 60 seconds, then `grep VmRSS /proc/$(pgrep -x stkr)/status`. This was not measured for this document (the Linux build was only cross-compiled); expect a similar value.

## Binary size

From `goreleaser release --snapshot` (the same configuration the release uses: `CGO_ENABLED=0`, `-s -w`). Size of the unpacked program in decimal megabytes (1 MB = 1,000,000 bytes).

| Platform | Program | Archive |
|---|---|---|
| windows/amd64 | 20.3 MB | 8.2 MB |
| windows/arm64 | 18.7 MB | 7.4 MB |
| linux/amd64 | 20.0 MB | 8.1 MB |
| linux/arm64 | 18.7 MB | 7.4 MB |
| darwin/amd64 | 20.2 MB | 8.1 MB |
| darwin/arm64 | 19.0 MB | 7.6 MB |

All are under the 30 MB target.

## Startup without a network

`go test -v -run AcceptanceStartupWithoutNetwork ./internal/e2e` starts `stkr serve` with the update check enabled and `HTTPS_PROXY` pointing at an address that never answers (10.255.255.1). Results on this machine: the server printed its address after 91 ms, and a deck `POST /api/switch/...` made while the update check was hanging was answered immediately. The test fails if startup takes more than 2 seconds; the target is 1 second, and the extra margin is for slow CI machines. The update check runs in a background job with a 10 second limit, stays silent on failure and never delays startup. Nothing else in startup uses the network.

## Other requirements

- No CGO: every build and the release use `CGO_ENABLED=0`; the SQLite driver is pure Go.
- A single Windows `.exe`: the web UI, templates, font, HTMX and time zone data are embedded.
- Missing optional tools degrade safely: see the Capture rows in [acceptance.md](acceptance.md).
