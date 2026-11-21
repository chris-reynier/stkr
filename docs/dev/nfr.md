# Performance and size numbers (v0.1.0; the desktop app is at the end)

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

# Desktop app (v0.2.0)

Targets: working set under 50 MB with the panel closed, under 80 MB (the program plus its WebView2 processes) with the panel open, and the tray icon within 1.5 s of starting the program.

## Method

`packaging/windows/measure-desktop.ps1` starts `stkrw.exe` on a temporary data folder and a free port and reads, with the PowerShell process APIs:

- **Cold start to the tray icon**: the process start time to the `tray ready` line in `desktop.log`, which the app writes the moment it adds its icon. **Cold start to the server**: process start to the first answer of `GET /healthz`. The time is one process start after the files are in the file cache; the very first start after a download or build can be slower while the system scans the new file.
- **Working set** (Task Manager "Memory") and **private bytes** of `stkrw.exe` after the idle time (30 s), and the working set of every child process (the `msedgewebview2.exe` processes of the panel).

Two modes. The default runs `stkrw --headless-test`: the real program with the single-instance lock, the hidden message window and the hosted server, but no icon, no window and no hotkey. WebView2 is started only when a window is first shown, so this is the "panel closed" state; the real tray adds one small icon handle. The `-Panel` mode runs the real app with `desktop.open_panel_on_start`, and needs a visible window, so it is for CI and disposable machines only (manual check M16). Local runs on a working desktop use the default mode only.

The CI job "desktop memory and start time" (windows-latest) runs both modes on every pull request and writes the tables to the job summary. The panel-closed run fails only above twice the target, the panel-open run never fails the build. Shared runners are noisy, so read the numbers as a range.

## Results (panel closed, headless test mode)

Measured on 2026-11-21 on Windows 11 (x64), Go 1.27.4, the release flags (`-s -w -H windowsgui`), four runs of 20 to 30 s idle:

| Measure | Value | Target |
|---|---|---|
| Cold start to the tray icon | 0.04 to 0.05 s | under 1.5 s |
| Cold start to the server answering | 0.20 to 0.23 s | |
| Working set, panel closed | 19.2 to 22.9 MB (about 20 MB; `stkr serve` alone: 17.6 MB) | under 50 MB |
| Private bytes | 50 to 54 MB | |
| Child processes | 0 | 0 |

One run right after a fresh build showed 62.8 MB in the first minute and 20 MB in every later run, the same effect as the 66.7 MB outlier in the v0.1.0 numbers above (the system scanning a new file). If you measure above 50 MB, measure again.

`stkrw.exe` is 18.0 MB (release flags, amd64), under the 35 MB budget of the desktop phases.

## Panel open

Not measured on the development machine: opening the panel shows a window, which this document's method does not do on a working desktop. The number comes from the CI job summary and from manual check M16 (Task Manager: the memory of `stkrw.exe` plus all `msedgewebview2.exe` rows). WebView2 shares most of its memory with other Edge-based programs on the same machine, so the total depends on what else is running; the 80 MB target is for a machine where nothing else uses WebView2.
