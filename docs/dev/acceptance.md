# Acceptance matrix

Every acceptance criterion of STKR v1 and the test or manual check that proves it. Tests are written as `package/TestName`. `e2e` tests build the real binary and drive it through the command line, HTTP and MCP against a temporary data folder (`internal/e2e/acceptance_test.go` unless another file is named). Run them with:

```
go test ./...
go test -v -run Acceptance ./internal/e2e
```

No automated test needs a network, a Stream Deck, ffmpeg, whisper, a real AI provider or a Windows desktop. Where a criterion depends on those, the last column gives a manual procedure and the manual checks are collected at the end.

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
| Idle memory, binary size | Measured by hand, see [nfr.md](nfr.md) |

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
