# Server

## What it does

`stkr serve` runs STKR's local HTTP server. Stream Deck keys and scripts use it to switch contexts, and it draws the key images. Your browser can use it too: see [Web UI](web-ui.md). It also runs background jobs: a daily backup (checked hourly), a sweeper that idles agent instances that stopped reporting, the close reminder (see [Daily loop](daily-loop.md)), the phone inbox watcher (see [Capture](capture.md)), the AI idea sorter when a provider is enabled (see [AI providers](ai-providers.md)) and the update check (see [Update](update.md)).

On Windows, the [desktop app](desktop.md) (`stkrw.exe`) runs the same server for you and shows it as an icon next to the clock, so you do not need to start `stkr serve` yourself.

The server listens on `127.0.0.1` only, so other computers cannot reach it. It also rejects requests with a foreign `Host` or `Origin` header. The server has no telemetry. The only network calls STKR makes are the ones you configure: the update check (one request to GitHub, can be turned off), the title lookup for bookmarks (can be turned off) and your AI provider if you enable one.

## How to use

```
stkr serve                 # uses the port from config (7878)
stkr serve --port 7999     # a different port for this run
stkr serve --port 0        # pick any free port; the URL is printed
```

It prints the address and runs until you press Ctrl+C. Try it:

```
curl -X POST http://127.0.0.1:7878/api/switch/other
curl http://127.0.0.1:7878/api/now
curl http://127.0.0.1:7878/healthz
```

`stkr switch`, `stkr now` and the other commands also work while the server runs. They use the same database.

### Start it automatically on Windows

Create a scheduled task that runs at logon (PowerShell, adjust the path):

```
$action   = New-ScheduledTaskAction -Execute "C:\Tools\stkr\stkr.exe" -Argument "serve"
$trigger  = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit 0 -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "STKR serve" -Action $action -Trigger $trigger -Settings $settings
```

Remove it with `Unregister-ScheduledTask -TaskName "STKR serve" -Confirm:$false`. To stop a running server, end the `stkr.exe` process or press Ctrl+C in its window.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `port` | `7878` | Port to listen on. `--port` overrides it for one run. |
| `allowed_origins` | empty | Extra browser origins allowed to send state-changing requests. Only needed if a web page you control calls the API. |

Change a key with `stkr config set port 7999`. The API is described in [the HTTP API contract](../contracts/http-api.md).

## Troubleshooting

- **"cannot listen on 127.0.0.1:7878"** (exit code 2): another program, often another `stkr serve`, uses the port. Stop it or use `--port`.
- **421 or 403 from a script**: the `Host` header must be `127.0.0.1:<port>` or `localhost:<port>`. Browsers add an `Origin` header to cross-site POSTs; add that origin to `allowed_origins` if it is yours.
- **Nothing happens when a key is pressed**: check `curl http://127.0.0.1:7878/healthz`, then the log in the `logs` folder of your data directory.
