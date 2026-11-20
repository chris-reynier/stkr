# Stream Deck

## What it does

A Stream Deck (or any tool that can send an HTTP request) can switch your context with one key press, and show the current context on a key. STKR needs no plugin for this: it exposes plain HTTP on your own computer. These steps work with any action that can send a `POST` request and any action that can show an image from a URL.

## How to use

1. Start the server: `stkr serve` (see [Server](server.md)).
2. For each context key, add an action that sends an HTTP **POST** to:

   ```
   http://127.0.0.1:7878/api/switch/<context-id>
   ```

   Add the header `X-STKR-Source: deck` (or append `?src=deck`) so the time log records the key as the source. Use `/api/stop` for a stop key.
3. For a "what am I doing" key, point the key image at:

   ```
   http://127.0.0.1:7878/img/now.png
   ```

   The image is 144x144, shows the current context and elapsed time, and changes color by context kind. Refresh it every few seconds if your tool allows; the image is never cached.
4. Pressing a key twice quickly is safe: a repeat switch within 2 seconds is ignored.

Find context ids with `stkr ctx list` or `http://127.0.0.1:7878/api/contexts`.

Only POST is accepted for switching, so a browser-style "Website" action that opens a page with GET will not work. Use an action that sends a POST request (for example an HTTP-request action from a plugin).


## Agents keys

STKR can also gather the state of your coding agents (VS Code windows) and jump to the one that needs you. See [Agents](agents.md).

| Key | Action | Image |
|---|---|---|
| Agents | `POST http://127.0.0.1:7878/api/agents/next/go` with header `X-STKR-Source: deck` | `http://127.0.0.1:7878/img/agents.png` |
| Acknowledge done (optional) | `POST http://127.0.0.1:7878/api/agents/ack-done` | none |

The full key table is in the [Stream Deck keys contract](../contracts/streamdeck-keys.md).

### Forwarding events from a plugin

STKR does not detect VS Code windows itself. The plugin that does (or your hooks) forwards events. The following TypeScript sketch shows the expected behavior: a 500 ms timeout, errors ignored so the plugin never changes behavior, a full sync on start, on reconnect and every 5 minutes. Adapt the names to your plugin. `stateMap` follows the table in the [agent events contract](../contracts/agent-events.md).

```ts
const STKR = "http://127.0.0.1:7878";

// stateMap.ts: plugin/hook name -> contract state
const stateMap: Record<string, string> = {
  permission_request: "action_required", waiting_input: "action_required", notification: "action_required",
  stop: "done", completed: "done",
  failed: "error", error: "error",
  running: "working", thinking: "working",
  idle: "idle",
};

async function post(path: string, body: unknown): Promise<void> {
  try {
    await fetch(STKR + path, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-STKR-Source": "deck" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(500),
    });
  } catch {
    // STKR not running or slow: ignore, never affect the plugin.
  }
}

export function forwardState(instance: string, rawState: string, slot: number | null, workspace: string, title: string, summary = "") {
  const state = stateMap[rawState.toLowerCase().replace(/-/g, "_")];
  if (state) void post("/api/agents/event", { instance, event: "state", state, slot, workspace, window_title: title, summary });
}
export const forwardFocused = (instance: string, workspace: string) => void post("/api/agents/event", { instance, event: "focused", workspace });
export const forwardClosed = (instance: string) => void post("/api/agents/event", { instance, event: "closed" });

export function sync(instances: Array<{ instance: string; state: string; slot: number | null; workspace: string; window_title: string }>) {
  void post("/api/agents/sync", { instances });
}
// Call sync(...) on startup, on reconnect, and every 5 minutes:
// setInterval(() => sync(currentInstances()), 5 * 60 * 1000);
```

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `port` | `7878` | Port in the URLs above. |

## Troubleshooting

- **404 not_found** for a switch: the context id is wrong (`stkr ctx list`).
- **409 conflict**: the context is parked; run `stkr ctx activate <id>`.
- **404 on a GET to `/api/switch/...`**: use POST.
- **Image stays blank**: open `http://127.0.0.1:7878/img/now.png` in a browser to check the server is reachable.
- **The agents key says "none"** or never changes: no events have reached STKR. See [Agents](agents.md#troubleshooting).
