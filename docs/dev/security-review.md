# Security review (v0.1.0)

Scope: the rules in [SECURITY.md](../../SECURITY.md) and the security rules for contributors. Method: source searches for each rule, a test for each rule that can be tested, a vulnerability scan, and a list of what remains manual. Date of the review: 2026-11-21, commit before the v0.1.0 tag.

## Summary

| Area | Result |
|---|---|
| Loopback-only server | Pass |
| Host and Origin checks on every route | Pass |
| No telemetry; network calls limited and switchable | Pass |
| No secrets in logs, API, UI, MCP, exports | Pass (no stored secret fields exist; the only secret is the optional `GITHUB_TOKEN` environment variable) |
| No clipboard, prompt, transcript or idea text in logs | Pass |
| Subprocesses: argv only, timeouts, no shell | Pass |
| Atomic writes | Pass after one fix (database backups) |
| Personal content stays local | Pass |
| Dependency vulnerabilities (`govulncheck`) | Pass after upgrading two modules |
| Data folder permissions | Pass on Linux and macOS; Windows relies on the user profile |
| Unsigned binary and endpoint protection | Manual (see the end) |

## Findings and fixes

1. **Database backups were not written atomically.** `VACUUM INTO` wrote straight to the final file name, so a crash during a backup could leave a partial `stkr-*.db` that looks like a valid backup. Fixed: the copy goes to `<name>.tmp`, is synced and renamed; a stale temp file from a crash is replaced. Test: `store/TestBackupIsAtomic`.
2. **`govulncheck` found a reachable vulnerability in `golang.org/x/image`** (GO-2026-4962, excessive memory use when decoding a crafted font; reachable through the key image renderer). STKR only parses its own embedded font, so it was not exploitable, but the module is upgraded to v0.48.0 and `golang.org/x/text` to v0.44.0 (which cleared further advisories that were present in the module graph but not called). `govulncheck ./...` now reports no vulnerabilities.
3. **A test helper started a process without a context** (`testutil.BuildFakebin`). Not shipped code, but the rule "every subprocess has a timeout" now has zero exceptions: `grep -rn "exec.Command(" --include=*.go . | grep -v _test.go` returns nothing.

No other defects were found.

## Checks, by rule

### Network exposure

- The server binds to `127.0.0.1` only (`httpapi.Listen`). Tests: `httpapi/TestListenerIsLoopback`; `e2e/TestAcceptanceLoopbackOnly` starts the real binary and shows that it refuses connections on every other IPv4 address of the machine.
- Host check (DNS rebinding) and Origin check (cross-site requests) wrap the whole mux, so a route added later is covered too. `e2e/TestAcceptanceEveryRouteIsGuarded` reads every `"METHOD /path"` registered in `internal/httpapi` and `internal/web`, requires at least 50 routes, and for each one sends a foreign `Host` (expects 421) and, for state-changing methods, a foreign `Origin` (expects 403). The guards are applied in `Handler()` around the one mux that both packages register on, so no route can skip them.
- Mutating routes accept POST only. `httpapi/TestGETDoesNotMutate` and the acceptance test show that a GET to a switch URL changes nothing.
- The web UI sends `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and `X-Frame-Options: DENY`. It has no inline script, no CDN and no external fonts. User text is escaped by `html/template` (`web/TestEscapesUserText`, `web/TestLinkTextIsEscaped`).
- Request bodies are limited to 1 MB; headers must arrive within 5 s.

### Outbound network

No telemetry. The complete list of outbound calls (source search for `http.Client`, `http.Get`, `net.Dial`):

| Call | When | Off switch |
|---|---|---|
| GitHub API (release list) | `stkr update`, and while `serve` runs if `update.check` is true | `update.check: false` |
| Bookmark title lookup (2 s, 512 KB, http/https, 5 redirects) | when you save a bookmark | `bookmarks.fetch_title: false` |
| AI provider | only when a provider is enabled: Ollama URL, or the `claude` / `copilot` program | `ai.provider: none` |
| Localhost health probe of `serve` | `stkr update`, to warn that a restart is needed | none needed (loopback) |

`e2e/TestAcceptanceStartupWithoutNetwork` points the proxy at a black hole and shows that startup takes well under a second and the server answers while the update check hangs.

### Secrets

- The configuration has a `secret:"true"` mechanism that masks values in `config show`, the API and the Settings page (`config/TestRedact*`, `web/TestSettingsDoNotLeakSecrets`). No field uses it yet, because STKR stores no credentials: Claude Code and Copilot keep their own sign-in, and Ollama needs none.
- The only secret STKR ever sees is the optional `GITHUB_TOKEN` environment variable, sent only to the GitHub API (`update/TestTokenSentOnAPIOnly`) and never to download hosts.
- `e2e/TestAcceptanceNoContentOrSecretsInLogsOrOutputs` sets `GITHUB_TOKEN` to a sentinel and checks `config show`, `version`, `ai status`, `update --check`, the Settings, Today, Agents and contexts pages and the log files. The MCP tests (`mcpserver/TestNoSecretsInAnyOutput`) do the same for every tool, the manifest and the prompts.

### Logging

- Logs carry ids, lengths and outcomes only. The request log has method, path, status and duration (no query string, no body).
- `e2e/TestAcceptanceNoContentOrSecretsInLogsOrOutputs` runs a busy session (switch note, ideas from the CLI, HTTP and MCP, a personal idea, a bookmark URL, an import, an AI answer, Start Day) and searches the complete log files for nine sentinel strings. It also fails if the log has no request lines, so it cannot pass by logging nothing.
- Package tests: `service/TestImportClipboardContentNeverLogged`, `service/TestLogsDoNotContainNotes`, `service/TestAgentLogsNoSummary`, `httpapi/TestRequestLogHasNoQueryOrBody`, `httpapi/TestDailyLogsNeverContainContent`, `ai/TestOnResultAndNoContentInLogs`.

### Subprocesses

- `exec.Command(` (without a context) in non-test code: **0**. Every launch uses `exec.CommandContext` with an explicit argv and no shell: ffmpeg and whisper (`internal/capture`), the AI exec adapter (`internal/ai`), and the workspace launcher (`internal/platform`). A search for `sh -c`, `cmd /c` and PowerShell strings finds nothing.
- Timeouts: AI calls use `ai.timeout_seconds` per attempt (and a 5 s probe); ffmpeg conversion 5 minutes; whisper a bounded timeout; recording is capped at 30 minutes (with `-t`) and a hard deadline; the launcher has `LaunchTimeout`; window focus and paste have their own waits (10 s). Large input goes over standard input (the AI prompt), not the command line. Output from the AI program is capped at 1 MB.
- Launcher arguments: a workspace path that starts with `-` is refused so it cannot be read as a flag (`platform/TestLauncherArgv`).
- `time.Now()` appears only for request latency and a window polling deadline, never for domain time.

### Files and the database

- Config, prompt templates, the OneDrive logs, the legacy archive, the update (new executable) and now database backups are written with a temp file in the same folder, `fsync` and rename (`config.WriteFileAtomic`, `legacy.Archive`, `update.ReplaceExecutable`, `store.Backup`). Database changes use transactions (`store/TestInTxRollsBack`, `store/TestPendingMigrationBacksUpAndFailureRollsBack`). SQLite runs in WAL mode.
- Exceptions that are intentional: the log file (append-only, rotated), the voice recording (ffmpeg writes the audio file; the idea row is inserted before transcription and a missing or empty file is reported), and moving phone-inbox files (rename).
- Data folder and files are created with `0700` / `0600`. On Windows, Go cannot set these bits and the folder is protected by the access rules of the user profile that already apply to `%USERPROFILE%`. If you point `STKR_HOME` at a shared folder, protect it yourself. The OneDrive export folder is created `0755`/`0644` on purpose: it is your sync folder.
- The audio route serves only a regular file inside the audio folder, resolved through symlinks (`service/TestAudioFileRejectsEscapes`, `service/TestAudioFileRejectsSymlinkEscape`).
- Self-update: the archive is installed only after its SHA-256 matches `checksums.txt` from the same release; downloads have size caps; the old executable is restored if the replacement fails (`update/TestChecksumMismatchKeepsCurrent`, `update/TestReplaceRollback`). Checksums protect against corruption, not against a compromised release; signing is future work.

### Privacy

- Personal-tagged ideas, ideas captured in personal or system contexts, and personal/system time are never sent to a provider that is not local (`service/TestAIPrivacyGate`; `e2e/TestAcceptanceProviderSwitchWithoutRebuild` shows a personal idea `skipped` with Claude Code and untouched). Ollama counts as local only at a loopback address.
- Personal and system contexts are never exported to timesheets or project logs, are left out of the Start Day digest, and are not shown to MCP agents (`mcpserver/TestPersonalContentIsHidden`; the acceptance test checks a personal idea is neither echoed nor listed).

### Dependencies

`govulncheck ./...` on the final tree: **No vulnerabilities found.** The dependency list is in `go.mod` and is limited to the libraries the project allows (all MIT, BSD or Apache-2.0): cobra, yaml, modernc sqlite (and its transitive modules), the MCP Go SDK, `golang.org/x/{image,sys,text,sync,oauth2,time}`, `atotto/clipboard`. `go mod tidy` leaves no diff. Dependabot is enabled for modules and for the workflow actions.

## Accepted risks and manual checks

- **Unsigned executables.** Windows SmartScreen and endpoint protection may flag an unsigned `stkr.exe` that listens on a local port, writes files and starts programs. Checksums are published; signing is on the backlog. Manual check M8 in [acceptance.md](acceptance.md).
- **Any local process of the same user can call the API.** The server has no login because it is bound to loopback and checks Host and Origin; another program running as you can still send requests. This is the same trust level as the database file in your profile.
- **AI provider programs run with your account.** The configured `claude` or `copilot` command is whatever you set in `config.yaml`; STKR does not sandbox it. Do not point these keys at programs you do not trust.
- **The MCP server trusts its client.** `stkr mcp` runs as you and can change your time data. It hides personal content and never exposes paths or settings, but an agent you connect can switch contexts, add ideas and report events by design.
