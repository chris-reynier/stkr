# Agents

## What it does

If you run coding agents in several VS Code windows, STKR keeps one list of them. A Stream Deck plugin (or hooks) reports each window's state. STKR then:

- shows all instances on the **Agents** page,
- picks the one that needs you next: errors first, then "waiting for my approval", then finished ones, then the oldest,
- jumps to it with one key press: logs the time against that project, brings its window to the front (Windows) or opens the folder with `code`,
- draws a summary key image: worst state color, counts and the next instance.

Time is logged only when you focus a window whose folder is mapped to a context.

## How to use

1. Run `stkr serve`.
2. Give each project context a working folder, so focusing its window logs time:

   ```
   stkr ctx set acme --workdir "C:\Work\acme-api"
   ```

   Or open <http://127.0.0.1:7878/ui/agents>: instances whose folder matches no context are listed under "Unknown workspaces", with a "Map" button.
3. Let your plugin or hooks send events. The payloads are in the [agent events contract](../contracts/agent-events.md); a ready-made forwarder snippet is in [Stream Deck](stream-deck.md).
4. Add two Stream Deck keys: one that sends `POST /api/agents/next/go` and shows `/img/agents.png`, and optionally `POST /api/agents/ack-done`.

From the command line:

```
stkr agents                 # list instances
stkr agents next            # show the one that needs attention next
stkr agents next --go       # also switch context and focus its window
```

On the Agents page, **Go** goes to one instance, **Queue** puts it first among instances in the same state, and **Acknowledge done** sets every finished instance to idle.

How states settle: a finished (`done`) instance with no news for 30 minutes becomes idle, and any instance with no news for 24 hours becomes idle. Focusing an instance clears its done and queued flags.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `launch.command` | `code` | Program that opens a workspace when no window is found. |
| `launch.args` | `{path}` | Its arguments; `{path}` is the workspace folder. |
| `agent_window_title` | `""` | Window title text to look for when an instance has neither a window title nor a folder. |

Each context's `workdir` (set with `stkr ctx set <id> --workdir <path>` or in Settings) maps folders to contexts. Paths match with `/` or `\`, with or without a trailing slash, and ignoring case on Windows.

## Troubleshooting

- **The key shows "none"**: no events have arrived. Check that the plugin forwards to the right port and that `stkr serve` is running (`curl http://127.0.0.1:7878/api/agents`).
- **No time is logged on focus**: the folder is not mapped. Look under "Unknown workspaces" and map it, or check the context is not parked.
- **"failed" after a Go**: no window title matched and the launch command could not run. Check that `code` is on your PATH or change `launch.command`.
- **The window does not come to the front**: Windows sometimes refuses to let a background program take focus. STKR tries a workaround; if the taskbar icon only flashes, click it. On macOS and Linux focusing is not available and STKR opens the folder instead.
- **An instance seems stuck**: it goes idle by itself after 30 minutes (done) or 24 hours; closing the window or the plugin's next sync removes it.
