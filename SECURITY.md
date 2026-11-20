# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub security advisories: open the repository's **Security** tab and choose **Report a vulnerability**. Do not open a public issue for security problems. You can expect an initial reply within a few days.

## Design

STKR is local-only by design:

- The HTTP server binds to `127.0.0.1` and checks the Host and Origin headers on every route.
- There is no telemetry. The only outbound network calls are to the AI provider you configure, bookmark title lookups, and the update check. Each can be turned off or is configurable.
- Secrets such as tokens and API keys are never written to logs, exports or API output. Configuration output masks them.
- Personal content is not sent to non-local AI providers and is not exported.
- The data directory (`~/.stkr` or `STKR_HOME`) is created with user-only permissions where the operating system supports it. Files are written atomically.

## Supported versions

Only the latest release receives fixes while the project is on 0.x.
