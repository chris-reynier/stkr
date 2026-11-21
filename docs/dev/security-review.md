# Security review (v0.1.0, with the v0.2.0 desktop addendum at the end)

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
| GitHub API (release list) | `stkr update`, "Check now" / "Check for updates", and while `serve` or the desktop app runs if `update.check` is true | `update.check: false` (explicit checks still work) |
| GitHub release downloads (archive or setup, `checksums.txt`) | only when you press Install update or run `stkr update`; the SHA-256 is verified before anything is installed or started | do not install updates |
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

## Addendum: desktop app, installer and in-app update (v0.2.0)

Scope: `stkrw.exe` (tray, panel and app windows), the Windows setup program, and the update flow added for the desktop app. Method as above: a test for each rule that can be tested without a real desktop, a static check where the code runs only in the installer, and a manual list for the rest. Date: 2026-11-21, branch for v0.2.0.

### Summary

| Rule | Result | Evidence |
|---|---|---|
| New routes sit behind the Host and Origin guards | Pass | `e2e/TestAcceptanceEveryRouteIsGuarded`, `e2e/TestAcceptanceRouteScanSeesEveryRegistration` |
| Update install is possible only under the desktop host | Pass | `e2e/TestSmokeP14SettingsAndUpdate` (plain `serve`), `e2e/TestDesktopHeadlessLifecycle` (desktop host), `web` hook tests |
| The setup program is checksum-verified before it runs | Pass | `update/TestSetupChecksumMismatchNotStarted`, `update/TestSetupPathStartsVerifiedSetup` |
| No new outbound calls | Pass | `e2e/TestAcceptanceOutboundHTTPOnlyWhereAllowed` |
| The PATH edit touches only HKCU and only its own entry | Pass (static) | `e2e/TestAcceptanceInstallerStaysPerUser`; CI job "windows installer" runs the real silent install and uninstall |
| The web view does not navigate to other origins | Pass (needs a manual check) | `desktop/TestNavigationAllowed`, `desktop/TestOriginOf`; manual check M11 |
| Uninstall data deletion is opt-in | Pass (static) | `e2e/TestAcceptanceInstallerStaysPerUser`; manual check M13 |
| A window that cannot start does not end the program | Pass after a fix | `desktop/TestWebViewFailureIsReportedNotFatal` |

### Routes the desktop app added

`GET /api/status`, `GET /ui/panel`, `GET /ui/panel/live`, `POST /ui/import/clipboard`, `POST /ui/update/check`, `POST /ui/update/apply`, `POST /ui/settings/config`, `POST /ui/settings/backup`, `POST /ui/settings/open-data`, plus the existing pages. They are registered on the same mux as every other route, so the Host guard (421) and the Origin guard on POST (403) wrap them. The route-table test now also scans `internal/server`, and a second test fails if any registration does not use a literal `"METHOD /path"`, because such a route would escape the table. The same test asserts that the routes above are in the table and that there is no route to show, quit or control the desktop app.

**There is no "desktop signal" route.** A second launch and `stkrw --quit` reach the running instance with a window message posted to its hidden window (found by window class and by a name derived from the data folder). The message carries two numbers (show or quit) and no text, so no content and no address crosses between the programs, and nothing on the network can trigger a quit. Only a process of the same user session can post it.

### Update from the app

- **Only under the desktop host.** `POST /ui/update/apply` fails with a conflict when the page is served by plain `stkr serve` (there is no hook to call), when no update was found by a check, and for a development build. Under `stkr serve` the page shows the command to run instead of a button.
- **Nothing runs before verification.** The release list comes from the GitHub API (`update.check`, or an explicit "Check now"). The archive or the setup is downloaded with a size cap, its SHA-256 is compared with `checksums.txt` of the same release, and only then is anything installed or started. A missing `checksums.txt`, a missing entry or a mismatch stops the update with nothing changed (`update/TestSetupChecksumMismatchNotStarted`, `update/TestChecksumMismatchKeepsCurrent`). The setup is written to a private (0700) temporary folder and started detached with fixed arguments (`/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /CURRENTUSER`, plus `/RELAUNCH=1` from the desktop app) and no shell.
- **All or nothing.** The replace method renames the old programs aside, renames the new ones in and undoes everything in reverse order on any failure (`update/TestApplyIsAllOrNothing`). It is never automatic: it needs a click, and the tray asks first with a message box whose default is No.
- **What it does not protect against.** A checksum protects against corruption, not against a compromised release, and the setup is not code-signed (SmartScreen warns). Both are on the backlog.

### Outbound network

No new call. `TestAcceptanceOutboundHTTPOnlyWhereAllowed` lists the only files that make HTTP requests: the Ollama client, the bookmark title fetch, the updater, and two loopback clients (the `stkr update` server probe and the desktop app's client of `127.0.0.1`). Any new file that uses an HTTP client fails the test until it is added to the list on purpose. The WebView2 runtime is Microsoft's component; STKR loads only pages from `127.0.0.1` in it (see "Accepted risks").

### The web view

- Every page comes from the local server (CSP `default-src 'self'`, no inline script, no CDN). The first-party script `desktop.js` sends only a fixed set of messages to the app (`hide`, `pin`, `app:/ui/...`, `url:http(s)://...`) and `desktop.ParseBridge` ignores everything else (`desktop/TestParseBridge`).
- **Navigation is now blocked natively.** The window uses a patched copy of the WebView2 wrapper (see below) that handles `NavigationStarting`: a navigation to any address that is not the window's own origin (`http://127.0.0.1:<port>`) or `about:blank` is cancelled, including `file:`, `data:`, `javascript:` and another port or host. An `http(s)` target is handed to the default browser instead. Requests for a new window (`window.open`, `target=_blank`) are always suppressed and routed the same way. The decision is a pure function covered by table tests; the COM call that applies it needs a desktop and is part of manual check M11.
- **A broken web view cannot take the program down.** The upstream wrapper called `log.Fatalf` when WebView2 could not create its environment, which would have ended `stkrw` and its server. The copy in `internal/desktop/edge` returns an error instead; the app then tells the user once and falls back to the browser. While fixing this, a second upstream defect turned up: the failure code was compared as a signed 64-bit number, so a failed start never matched and would have crashed on a nil pointer. `desktop/TestWebViewFailureIsReportedNotFatal` covers both. It drives the wrapper's completion callbacks with failure results (no window, no runtime needed). A first version of the test embedded a real web view with an unusable data folder; on a CI runner that crashed inside WebView2, so the test no longer depends on the runtime's own failure behaviour, and the wrapper also treats a missing environment or controller as a failure and bounds its wait.
- Permission prompts of the web view (camera, location and similar) are not used by any page. The WebView2 profile lives in `<data folder>/desktop/webview` and is not shared with Edge.

### Installer

- `PrivilegesRequired=lowest` and no machine-wide location: `TestAcceptanceInstallerStaysPerUser` fails on `HKLM`, `HKA`, `{pf}`, `{commonappdata}`, `{sys}` and any `Reg*` call or `[Registry]` entry whose root is not `HKCU`.
- The PATH edit appends the install folder to the user's `Path` (kept as an expandable string) and removes only that entry on uninstall; other entries are untouched. The CI job "windows installer" installs silently, checks the PATH entry, upgrades, uninstalls and checks that the entry is gone and that a data folder in a temporary `STKR_HOME` is unchanged. The sign-in entry is the single value `HKCU\...\Run\STKR`.
- Data deletion on uninstall is opt-in: two confirmations, each defaulting to No, a silent uninstall always keeps the data, and the folder is deleted only if `SafeToDelete` accepts it. Manual check M13.

### Logs

`desktop.log` records action kinds, ports, versions and error kinds, never a context label, note, idea, bookmark or path. `desktop/TestAppHostsAndRunsActions` asserts that labels and ids stay out of the log, and `e2e/TestDesktopHeadlessLifecycle` searches the real log after a session that contained an idea and a context label. The test mode logs "open" only for its own `127.0.0.1` pages and the word "external" for anything else (`desktop/TestHeadlessOpenLogsOnlyOwnPages`).

### Test mode

`stkrw --headless-test` exists for the acceptance tests. It keeps the real single-instance mutex, the hidden message window and the hosted server, but has no notification-area icon, no windows, no global hotkey, no message boxes and never opens a browser or a folder. It is not a back door: it adds no route and no capability, it only turns things off.

### Accepted risks and manual checks (desktop)

- **Unsigned programs and setup.** SmartScreen shows a warning on first run; the troubleshooting page explains it. Code signing is on the backlog.
- **WebView2 is Microsoft's browser engine.** It is updated by Windows Update, and its own diagnostics settings are Microsoft's. STKR loads only its local server in it.
- **The window message can be posted by any process of the same user session.** Its only effects are "show the panel" and "quit the app", the same power that process has by ending the app.
- **A patched copy of a third-party wrapper.** `internal/desktop/edge` is MIT code that STKR now maintains; its changes are listed in `doc.go` and `doc.go` says why. Re-diff it against upstream when go-webview2 is updated.
- Manual checks M10 to M16 in [acceptance.md](acceptance.md) cover what needs a real desktop: tray icon, panel, hotkey, navigation block, installer wizard, update, sign-in entry and the panel-open measurements.
