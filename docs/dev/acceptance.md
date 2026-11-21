# Acceptance matrix

Every acceptance criterion of STKR v1 and the test or manual check that proves it. Tests are written as `package/TestName`. `e2e` tests build the real binary and drive it through the command line, HTTP and MCP against a temporary data folder (`internal/e2e/acceptance_test.go` unless another file is named). Run them with:

```
go test ./...
go test -v -run Acceptance ./internal/e2e
```

No automated test needs a network, a Stream Deck, ffmpeg, whisper, a real AI provider or a visible Windows desktop (the desktop tests use the headless test mode of `stkrw.exe`, on Windows only). Where a criterion depends on those, the last column gives a manual procedure and the manual checks are collected at the end.

## Core

| Criterion | Automated proof | Manual |
|---|---|---|
| A full work day can be recorded only through Stream Deck keys | `e2e/TestAcceptanceFullDayDeckOnly`: planning, project, double click, meeting with an action and a decision, project, break and stop, all through `POST /api/...` with the deck header; every event has source `deck` | Real keys: M1 |
| `switch` creates the correct event and time block | `service/TestDoubleSwitchIsIdempotent`, `service/TestDayReport`, `timeline/TestBlocks`, `timeline/TestBlockProperties`, `timeline/TestDSTDurations`, `httpapi/TestSwitchStopNow` | |
| Gaps are flagged and never counted | `timeline/TestGaps`, `timeline/TestWorkingDay`, `service/TestConfirmMakesIdleBlockCount` | |
| Personal and system contexts are excluded from exports | `timeline/TestAggregateAndExport`, `service/TestTempoGolden`, `e2e/TestAcceptanceFullDayDeckOnly` (no `break` in the Tempo CSV) | |
| Export rounding to 0.25 h is correct (after summing, only at export) | `timeline/TestRoundForExport`, `service/TestTempoRoundsAfterSumming`, `service/TestTempoGolden` | |
| `stkr now` and the Today page show consistent state | `e2e/TestAcceptanceFullDayDeckOnly` compares `stkr now --json`, `GET /api/now` and the Now box of `/ui/today` (context and start time) | |
| SQLite migrations work | `store/TestMigrateCreatesEverythingAndIsIdempotent`, `store/TestPreMigrationBackupFromFakeV0`, `store/TestPendingMigrationBacksUpAndFailureRollsBack` | |
| Backups work | `service/TestDailyBackupKeeps14Generations`, `store/TestBackup`, `store/TestBackupIsAtomic`, `e2e/TestAcceptanceBackupRestoreAndLegacy` (backup, restore by copying into a new data folder, no temp files left) | |
| Legacy import is optional and cannot corrupt the database | `legacy/TestArchiveCopiesKnownFilesOnly`, `e2e/TestAcceptanceBackupRestoreAndLegacy` (database file is byte-identical after `import-legacy`) | |

## Stream Deck

| Criterion | Automated proof | Manual |
|---|---|---|
| A slot press logs a context switch | `httpapi/TestSwitchStopNow`, `e2e/TestSmokeP02Serve`, `e2e/TestAcceptanceFullDayDeckOnly` | M1 |
| The meeting scenario works end to end | `e2e/TestAcceptanceFullDayDeckOnly` (switch to meeting, capture an action and a decision, both appear on Today and belong to the project before the meeting), `service/TestMeetingAttribution` | M1 |
| The Agents hub reaches `action_required` in one press | `service/TestStreamDeckOnePressReachesActionRequired`, `e2e/TestSmokeP04Agents` | M1 |
| The plugin stays functional if STKR is unavailable | The plugin side is outside this repository. The contract and the forwarder sketch (500 ms timeout, errors ignored) are in [Stream Deck](../user/stream-deck.md) and [agent events](../contracts/agent-events.md) | M2 |
| Event forwarding never blocks the plugin | STKR side: `httpapi/TestAgentEventPerf` (p95 well under 100 ms), `e2e/TestAcceptancePerfBudgets`. Plugin side: see M2 | M2 |
| Unknown workspaces are visible | `service/TestUnknownWorkspacesAndMapping`, `web/TestAgentsPage` | |
| Window focus works or falls back safely | Fallbacks: `service/TestGoFallbacksAndResults`, `platform/TestPasteIntoMissingWindow`, `platform/TestLauncherArgv`, `platform/TestLauncherTimeout` | Real focus: M3 |

## Daily loop

| Criterion | Automated proof | Manual |
|---|---|---|
| Start Day and Close Day complete in under three minutes | The STKR part is instant: `service/TestStartDay`, `service/TestStartDayHeavyHistoryFitsLimit`, `e2e/TestSmokeP05DailyLoop` | Stopwatch with a real chat: M4 |
| Clipboard import prevents duplicate items | `e2e/TestAcceptanceDuplicateImport` (CLI, then HTTP, then CLI again, then a reworded-whitespace copy: one item each), `service/TestImportBriefAndDuplicates`, `daily/TestFingerprint` | |
| OneDrive Notes sections stay untouched | `e2e/TestAcceptanceNotesPreserved` (Notes and a user-made section survive three imports), `daily/TestRenderLogNotesByteIdenticalAfterThreeExports`, `service/TestExportKeepsNotesAcrossExports` | |
| Close reminders blink correctly | `service/TestCloseStateAndBlink`, `service/TestReminderTickLogsOncePerDay`, `httpapi/TestCloseKeyImageBlinks` | Colors on a real key: M1 |
| A missing close shows up in the next Start Day digest | `daily/TestBuildDigestLimitAndPriority` ("Previous days not closed"), `service/TestStartDay` | |

## Capture

| Criterion | Automated proof | Manual |
|---|---|---|
| Voice idea capture survives missing whisper | `e2e/TestAcceptanceMissingWhisperAndFFmpeg` (phone audio with a missing whisper: the idea and its audio exist, `ai_state` is `skipped`, health reports `whisper: missing`), `service/TestMissingWhisperKeepsIdea`, `capture/TestWhisper` | Real recording: M5 |
| The audio idea is inserted before transcription | `service/TestVoiceToggle` ("idea before transcription"), `e2e/TestAcceptanceMissingWhisperAndFFmpeg` | M5 |
| Text ideas are available within one minute | `e2e/TestSmokeP06Capture` (instantly through the CLI), `service/TestAICaptureNeverWaitsForProvider`, `service/TestAINoopAndCaptureLatency` | |
| Missing ffmpeg or whisper is reported, not fatal | `e2e/TestAcceptanceMissingWhisperAndFFmpeg` (503 for the key press, server and text capture keep working), `service/TestMissingFFmpegIs503`, `service/TestInboxAudioWithoutFFmpegStaysPut`, `httpapi/TestCaptureHealthEndpoint` | |
| Bookmark capture validates URLs and survives a failed title lookup | `e2e/TestAcceptanceBookmarkValidationAndTitleFailure`, `service/TestBookmarkValidation`, `service/TestBookmarkTitleFetch`, `capture/TestFetchTitle` | |

## MCP and runtime AI

| Criterion | Automated proof | Manual |
|---|---|---|
| Claude Code can connect to `stkr mcp` | `e2e/TestSmokeP08MCP` and `e2e/TestAcceptanceMCPSwitchCaptureTriageReport` speak the real protocol over stdio (initialize, tools) with the same framing Claude Code uses | M6 |
| A GitHub Copilot session (MCP-capable) can read today, week and contexts | `mcpserver/TestToolsHappyPathAndErrors`, `e2e/TestAcceptanceMCPSwitchCaptureTriageReport` (`list_contexts`, `get_week`) | M6 |
| MCP agents can capture and triage ideas | `e2e/TestAcceptanceMCPSwitchCaptureTriageReport` (`add_idea`, `triage_idea`, `list_ideas`, seen from the CLI) | |
| MCP agents can report agent events | `e2e/TestAcceptanceMCPSwitchCaptureTriageReport` (`report_agent_event`, seen in `stkr agents`) | |
| MCP agents can switch context | `e2e/TestAcceptanceMCPSwitchCaptureTriageReport` (event source `mcp`) | |
| The runtime provider can be Claude Code, GitHub Copilot or Ollama | `e2e/TestAcceptanceProviderSwitchWithoutRebuild` (Ollama and Claude Code; the Copilot adapter is the same exec adapter, `ai/TestExecCleanup`, `ai/TestSelection`) | Real providers: M7 |
| No STKR secret is exposed to MCP or the provider | `mcpserver/TestNoSecretsInAnyOutput`, `mcpserver/TestPersonalContentIsHidden`, `ai/TestPromptsHoldOnlyTheInput`, `e2e/TestAcceptanceNoContentOrSecretsInLogsOrOutputs` | |
| An MCP integration document exists without the old Python repository | [MCP server](../user/mcp.md), [MCP tools contract](../contracts/mcp-tools.md) (generated from the tool registry; `mcpserver/TestToolListGoldenAndContract` fails if it drifts), `mcpserver/TestManifestAndPrompts` | |

## Optional AI

| Criterion | Automated proof | Manual |
|---|---|---|
| Switching providers does not require rebuilding | `e2e/TestAcceptanceProviderSwitchWithoutRebuild` (none, Ollama, Claude Code, none again, with one binary and `config set`) | |
| All adapters keep the same service contract | `ai/TestOperationsAndValidation`, `ai/TestOllamaCleanup`, `ai/TestExecCleanup` | M7 |
| Operation with Ollama off is fully functional | `e2e/TestAcceptanceProviderSwitchWithoutRebuild` (step 1 and step 5), `service/TestAINoopAndCaptureLatency`, `service/TestAIUnreachableLeavesPending` | |
| Raw text is kept if cleanup fails | `e2e/TestAcceptanceProviderSwitchWithoutRebuild` (invalid output: `failed`, raw text intact), `service/TestAIFailureKeepsRawText` | |
| Strict JSON is validated | `ai/TestOperationsAndValidation` (unknown fields and bad shapes are rejected) | |
| Retries are bounded | `e2e/TestAcceptanceProviderSwitchWithoutRebuild` (one retry), `ai/TestParentContextCancelStopsRetry` | |
| Personal content is not sent externally | `e2e/TestAcceptanceProviderSwitchWithoutRebuild` (personal idea `skipped` with a non-local provider), `service/TestAIPrivacyGate` | |

## Desktop app (v0.2.0, Windows)

The desktop-facing tests run the real `stkrw.exe` in its test mode (`--headless-test`): the real single-instance lock, hidden message window and hosted server, but no icon, no window, no hotkey and no browser. They skip on other systems. See [Testing](testing.md#desktop-tests).

| Criterion | Automated proof | Manual |
|---|---|---|
| The app hosts the server and `/api/status` follows the CLI | `e2e/TestDesktopHeadlessLifecycle` (hosted_by `desktop`, a `stkr switch` shows in `/api/status`) | M10 |
| Panel switch, idea and stop work through the hosted server | `e2e/TestDesktopHeadlessLifecycle`, `e2e/TestSmokeP12Panel` (browser) | M11 |
| A second launch signals the first and exits 0 | `e2e/TestDesktopHeadlessLifecycle` (exit code 0, the first one logs the show action) | M10 (5) |
| `--quit` stops the app gracefully and the database stays intact | `e2e/TestDesktopHeadlessLifecycle` (exit 0, server gone, `PRAGMA integrity_check` = ok, data still there) | M10 (8) |
| Attach mode when `stkr serve` already runs | `e2e/TestDesktopHeadlessAttach` (no second server, the serve keeps answering after the quit), `desktop/TestApp*` | M10 (7) |
| Update install is refused without a pending update, and under plain `serve` | `e2e/TestDesktopHeadlessLifecycle`, `e2e/TestSmokeP14SettingsAndUpdate` | M14 |
| Every desktop route is behind the Host/Origin guard | `e2e/TestAcceptanceEveryRouteIsGuarded`, `e2e/TestAcceptanceRouteScanSeesEveryRegistration` | |
| The window cannot end the program when WebView2 fails | `desktop/TestEmbedFailureDoesNotEndTheProcess` | M12 (5) |
| The window cannot navigate to other origins | `desktop/TestNavigationAllowed`, `desktop/TestOriginOf` | M11 (10) |
| Installer is per-user and the data deletion is opt-in | `e2e/TestAcceptanceInstallerStaysPerUser`; CI job "windows installer" (silent install, upgrade, uninstall) | M13 |
| No new outbound network calls | `e2e/TestAcceptanceOutboundHTTPOnlyWhereAllowed` | |
| Idle memory and cold start with the panel closed | CI job "desktop memory and start time" (`packaging/windows/measure-desktop.ps1`), numbers in [nfr.md](nfr.md) | M16 (panel open) |

## Security and non-functional requirements

These have their own documents: [security review](security-review.md) and [performance and size numbers](nfr.md). Short version of the proof:

| Requirement | Automated proof |
|---|---|
| Loopback only | `httpapi/TestListenerIsLoopback`, `e2e/TestAcceptanceLoopbackOnly` (the running server refuses connections on the machine's other addresses) |
| Host and Origin checked on every route | `e2e/TestAcceptanceEveryRouteIsGuarded` (reads every registered route from the source and tries a foreign Host and a foreign Origin on each), `httpapi/TestHostGuard`, `httpapi/TestOriginGuard` |
| No content or secret in logs or outputs | `e2e/TestAcceptanceNoContentOrSecretsInLogsOrOutputs` (sentinel strings in notes, ideas, imports, bookmarks, AI answers and an environment token; the whole log and the outputs are searched) |
| Deck POST under 100 ms, image GET under 50 ms | `e2e/TestAcceptancePerfBudgets` (real HTTP against the binary), `httpapi/TestPerfBudgets` |
| Startup does not need the network | `e2e/TestAcceptanceStartupWithoutNetwork` |
| Duplicate imports add nothing, double clicks are idempotent | `e2e/TestAcceptanceDuplicateImport`, `e2e/TestAcceptanceFullDayDeckOnly` (double click), `service/TestDoubleSwitchIsIdempotent` |
| Atomic file and database writes | `store/TestBackupIsAtomic`, `config/TestWriteFileAtomic`, `update/TestReplaceRollback`, `daily/TestRenderLogUpdatePreservesEverythingElse`, `store/TestInTxRollsBack` |
| Idle memory, start time, binary size | Measured, see [nfr.md](nfr.md) (`packaging/windows/measure-desktop.ps1` in CI for the desktop app) |

## Manual checks

These need hardware, a desktop or a real account. Do them once per release and note the result in the release notes.

- **M1: Stream Deck keys.** Build a profile from [Stream Deck](../user/stream-deck.md): context keys, Agents key, Start Day, Close Day, Import, Idea. Press each; check `stkr now`, the key images (including the blinking Close key after `close_reminder`) and a double click (one event only).
- **M2: Plugin without STKR.** With `stkr serve` stopped, press keys and let the plugin forward agent events. The plugin must keep working and must not delay or show errors. Start `stkr serve` again; the next sync restores the Agents page.
- **M3: Window focus and paste.** On Windows, run `stkr agents next --go` for an instance with an open VS Code window and check that it comes to the front and the time is logged. Run `stkr close --paste` with `agent_window_title` set to a real chat window and check the prompt arrives. Repeat with the window closed to see the fallback (the folder opens with `launch.command`).
- **M4: Daily loop timing.** With a real AI chat: Start Day, paste, answer, Import, then Close Day, paste, answer, Import. Time it; each loop is under three minutes. Confirm the project log files in `onedrive_dir` keep your Notes.
- **M5: Voice.** With ffmpeg and whisper installed: press the Idea key, speak, press again. The idea appears at once and gets its text a few seconds later. Then rename whisper in `whisper.bin` and repeat: the idea is kept, shown as "Voice note, no transcript". Drop an `.m4a` file in the phone inbox and check it is taken.
- **M6: MCP clients.** Add the server to Claude Code (`claude mcp add stkr -- stkr mcp`) and to VS Code Copilot (`.vscode/mcp.json`), then ask "what am I tracking" and "capture an idea". See [MCP server](../user/mcp.md).
- **M7: Real AI providers.** For each of Ollama, Claude Code and GitHub Copilot: enable it, run `stkr ai status`, `stkr ai test` and `stkr ai run-once` with one normal and one personal idea. Personal ideas must be `skipped` for Claude Code and Copilot. The Copilot defaults have not been checked against an installed Copilot CLI; adjust `github_copilot.args` if needed.
- **M8: Endpoint protection.** Run the unsigned `stkr.exe` on a managed Windows machine. Check that the antivirus or application control allows it to start, listen on `127.0.0.1` and write to `~/.stkr`. See [Install](../user/install.md) for the SmartScreen note.
- **M9: Release assets.** After tagging, check that the release has six archives and `checksums.txt`, then run `stkr update --check` from an older build.
- **M10: Desktop tray (Windows).** Build `go build -ldflags "-H windowsgui" -o stkrw.exe ./cmd/stkrw` and start it with a temporary `STKR_HOME` and a free `port`. Check: (1) no console window opens and an STKR icon appears in the notification area (grey while nothing runs); (2) right-click shows the menu: header, Switch to with the contexts, Stop, Capture idea, Save clipboard as bookmark, Start day, Close day, Import answer from clipboard, Open, Data folder, Logs folder, About, Quit; (3) Switch to a project: the icon turns blue with a white dot, the tooltip reads `STKR - <label> 0h00`, a notification confirms; Stop turns it grey; (4) left-click shows the panel (details in M11); (5) start `stkrw.exe` a second time: it exits at once and the first one shows the panel; (6) restart Explorer (`taskkill /f /im explorer.exe` then `start explorer.exe`): the icon comes back within a few seconds; (7) with `stkr serve` already running on the port, start `stkrw.exe`: About says "attached to `stkr serve`"; stop serve and within one poll the tray runs the server itself; (8) Quit: the icon disappears and `/healthz` stops answering within 5 seconds. Automated: the HTTP side, menu, icon drawing, notifications and attach/host logic are covered by `go test ./internal/desktop/...`; a run of the built program on a desktop confirmed hosting, the second-instance signal and graceful stop on `WM_CLOSE`.
- **M11: Panel and app window (Windows).** Start `stkrw.exe` with a temporary `STKR_HOME` and a free `port`, with at least two contexts. Check: (1) left-click the icon: a 360 x 560 panel opens near the pointer, with no taskbar button, showing "Nothing running", your contexts, the idea box, Start day / Close day / Import from clipboard, "Agents waiting: 0" and the footer with Open STKR, Settings and Pinned; (2) maximize a browser or Explorer window and click the icon again to hide, then once more to show: the panel is above the maximized window; (3) switch to a context in the panel: the running line and the tray tooltip change within `desktop.poll_seconds`; Stop returns to "Nothing running"; (4) type text in the idea box, Shift+Enter adds a line, Enter saves it and the page shows "Idea saved." (check `stkr ideas`); (5) click Pinned in the panel (the button reads "Pin" and "Keep panel on top" is unchecked in the tray menu), click a maximized window: it now covers the panel; tick "Keep panel on top" in the tray menu: the panel is on top again and the button reads "Pinned"; quit and start again: the pin state is kept (`desktop.panel_on_top` in config.yaml); (6) drag the panel and resize it, press Esc to hide it, quit STKR and start it again: the panel shows at the same place and size (`desktop\window.json`); (7) set the panel on a second monitor, quit, disconnect that monitor (or edit `window.json` to x = 9000) and start again: the panel opens on the remaining screen; (8) Open STKR: a normal window with a taskbar button shows Today; resize and move it, close it, open it again: same size and place; a link to another site (add a bookmark and click it in Links) opens in the default browser, not in the window; (9) Quit: both windows and the `msedgewebview2.exe` processes of this data folder are gone within a few seconds; (10) navigation block: in the app window open Links, add the bookmarks `http://127.0.0.1:1/` and `https://example.com/` and click each: both open in the default browser and the window stays on Links; a middle-click on a link, or a link with a new-window target, opens no extra STKR window. Automated: the panel page and its actions (`go test ./internal/web -run Panel`), the hotkey parser, the page-message checks, the window state and clamp math (`go test ./internal/desktop`) and a fetch plus switch through `stkr serve` (`go test ./internal/e2e -run P12`); the WebView2 and Win32 window code is only compiled in tests.
- **M12: Hotkey and missing WebView2 (Windows).** (1) With the default `desktop.hotkey` (Ctrl+Alt+Space), press it from another program: the panel opens with the idea box focused; type and press Enter: the idea is saved; press the hotkey again: the panel hides. (2) Run `stkr config set desktop.hotkey Ctrl+Shift+K` and restart: the new key works and the old one does not. (3) Start a program that owns Ctrl+Alt+Space (or set the same key in two STKR data folders) and start `stkrw.exe`: a notification names the key and says another program uses it; the rest of the app works. (4) `stkr config set desktop.hotkey ""`: no hotkey is registered. (5) On a machine or VM without the WebView2 Runtime (or with the runtime renamed): left-click the icon: one notification explains that WebView2 is missing and Today opens in the default browser; "Open STKR" and the Open menu open their pages in the browser; nothing crashes and the tray keeps working. After installing the runtime and restarting STKR the panel opens normally.
- **M13: Interactive installer (Windows, disposable user or VM).** (1) Run `stkr_<version>_windows_amd64_setup.exe` by double-click. SmartScreen shows "More info, Run anyway". The wizard needs no admin prompt, offers the two tasks and the Launch checkbox; STKR starts and the tray icon appears. (2) A new terminal finds `stkr version`. The Start menu has "STKR". With the sign-in task on, Task Manager, Startup apps lists STKR; sign out and in: the icon returns. (3) Install the same file again while the app runs: setup closes it, the PATH entry is not duplicated, and your task choices are kept. (4) Settings, Apps, Uninstall: the programs, shortcut and PATH entry go; the second question about data defaults to No and `%USERPROFILE%\.stkr` stays. Repeat and answer Yes twice: the data folder is deleted. (5) A silent uninstall (`unins000.exe /VERYSILENT`) keeps the data.
- **M14: Update from the tray (Windows, both install methods).** Needs two real releases: an older one (for example `v0.2.0-rc.1`, on the `beta` channel) installed, and the newer one (`v0.2.0`) published. Do it once with a copy installed by the setup and once with a copy unpacked from the zip. (1) Start `stkrw.exe`, set `update.channel` to `beta` if the older one is a pre-release, and choose "Check for updates" in the tray menu: a notification says the new version is available and the menu now has "Install update vX...". The web banner and Settings, Updates show the same version and the Install update button. (2) Choose "Install update": a message box asks; answer No: nothing happens. Choose it again and answer Yes (or press the button in Settings): "Downloading the update..." appears, then the icon disappears, comes back within about a minute and a notification says "STKR updated to X". (3) Check `stkr version` and About: the new version; `stkr.exe` and `stkrw.exe` are both new (file versions or dates). Setup copy: `install.json` shows the new version and the PATH and sign-in choices are unchanged. Zip copy: no `.old` files remain after the next start. (4) Your data (contexts, events, ideas) is unchanged. (5) Failure paths: disconnect the network before pressing Install: a notification gives the reason and STKR keeps running; make the program folder read-only (zip copy) and install: the update is refused and the old version keeps running (a wrong checksum cannot be provoked from the UI; the automated tests cover it). (6) Attach mode: start `stkr serve`, then `stkrw.exe`; choosing "Install update" shows the hint to run `stkr update` and restart `stkr serve`, and the web page shows no Install button. (7) `stkr update` from a terminal on the setup copy prints that it runs the setup, and the setup finishes in the background (the app is not started again). Automated: the updater (`go test ./internal/update`), the setup arguments and checksum, the desktop flow with fakes (`go test ./internal/desktop -run Update`), the web routes (`go test ./internal/web -run Update`).
- **M15: Sign-in entry and live settings (Windows).** (1) Start `stkrw.exe` with the setup copy where the sign-in task was ticked: Settings, Desktop app shows "Start STKR when I sign in" ticked. Untick, save: the app restarts its server within a few seconds and the page reloads; `reg query HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v STKR` reports no value and Task Manager, Startup apps no longer lists STKR. Tick, save: the value is `"<folder>\stkrw.exe" --autostart` and sign out and in starts the app. (2) Change the entry in Task Manager, Startup apps (disable it) and reopen Settings: the box reflects what Windows has. (3) Change "Panel hotkey" and "Refresh every" in Settings and save: the new hotkey works at once, the old one does not, and the tray refreshes at the new interval, without quitting the app. (4) Change `ai.provider` and save: after the restart "Test provider" uses the new provider. (5) Start a voice recording, save Settings: the recording is stopped by the restart (known limit). (6) "Back up now" adds a file to `backups\`; "Open data folder" opens Explorer on the data folder. (7) Under `stkr serve` (no desktop app) Settings has no Desktop app section, no Open data folder button, and the message after saving says to restart `stkr serve`. Automated: the sign-in entry logic with a fake registry (`go test ./internal/platform ./internal/service`), the restart flow and port handling (`go test ./internal/desktop -run Settings`), the web side (`go test ./internal/web -run Settings`).
- **M16: Panel-open memory and cold start (Windows).** On a disposable machine or CI (this shows a window and an icon): `go build -ldflags "-s -w" -o stkr.exe ./cmd/stkr` and `go build -ldflags "-s -w -H windowsgui" -o stkrw.exe ./cmd/stkrw`, then `powershell -File packaging\windows\measure-desktop.ps1 -Stkr .\stkr.exe -Stkrw .\stkrw.exe -Panel` (the script uses a temporary data folder and a free port, opens the panel, waits 30 s, adds up the working set of stkrw and its `msedgewebview2.exe` children and quits). Target: under 80 MB in total with the panel open and the tray icon within 1.5 s of the start. By hand: Task Manager, Details, add the memory of `stkrw.exe` and all `msedgewebview2.exe` rows. The CI job "desktop memory and start time" runs the same script (panel closed in test mode, panel open informational) and writes the table to the job summary. Run it twice: the first run after a fresh build can be much higher because the system scans the new file.
