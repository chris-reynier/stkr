# Agent events contract (v1)

How a Stream Deck plugin, hook or script tells STKR about coding agent instances (VS Code windows). STKR aggregates them, picks the one that needs attention next, logs time when you focus one, and draws the agents key. The plugin keeps owning instance detection, slots and slot drawing; STKR never draws per-slot images.

Transport: HTTP `POST` with a JSON body to `http://127.0.0.1:<port>`. The general rules (Host and Origin guards, 1 MiB limit, error JSON) are in [http-api.md](http-api.md).

## States

| `state` | Meaning |
|---|---|
| `idle` | Nothing running, nothing to act on. |
| `working` | The agent is running. |
| `action_required` | The agent is **waiting for my approval or input** (permission prompt, question). |
| `done` | The agent finished and the result is waiting to be looked at. |
| `error` | The agent failed. |

### Approval rule

`action_required` means exactly "waiting for me to approve or answer". Map every hook or plugin signal that blocks the agent on a person to it. Do not use it for plain notifications that need no answer.

### Recommended `stateMap.ts`

Map the names your plugin or hooks produce to contract states. Compare names case-insensitively; replace `-` with `_` first. Unknown names should not be forwarded.

| Plugin / hook name | Contract state |
|---|---|
| `permission_request`, `permission_prompt`, `waiting_input`, `waiting_for_input`, `awaiting_approval`, `notification` | `action_required` |
| `stop`, `completed`, `complete`, `finished`, `task_complete` | `done` |
| `failed`, `failure`, `error`, `crashed` | `error` |
| `running`, `thinking`, `busy`, `working`, `pre_tool_use`, `user_prompt_submit` | `working` |
| `idle`, `ready`, `session_start`, `inactive` | `idle` |

## `POST /api/agents/event`

One event about one instance.

```json
{
  "instance": "win-3f2a",
  "event": "state",
  "state": "action_required",
  "slot": 2,
  "workspace": "C:\\Work\\acme-api",
  "window_title": "main.go - acme-api - Visual Studio Code",
  "summary": "Wants to run: go test ./...",
  "ts": "2026-11-20T10:15:00Z"
}
```

| Field | Type | Rules |
|---|---|---|
| `instance` | string | Required. Stable id of the window, 1-128 characters, no control characters. |
| `event` | string | Required. `state`, `focused` or `closed`. |
| `state` | string | Required for `state`. Optional for `focused` (applied before the focus rules). Ignored for `closed`. |
| `slot` | integer or null | 1-6, the plugin slot. Missing or `null` keeps the known slot. |
| `workspace` | string | Absolute workspace folder, up to 1000 characters. Missing keeps the known one. |
| `window_title` | string | Window title, cut at 500 characters. Missing keeps the known one. |
| `summary` | string | Short text, cut at 200 characters. A `state` event replaces the summary (missing clears it); other events keep it unless they carry one. |
| `ts` | string | RFC3339, informational. STKR stamps instances with its own clock, so a skewed plugin clock cannot confuse the idle rules. |

Unknown fields are ignored.

Event rules:

- `state`: create or update the instance. `updated` is set to now.
- `focused`: the user focused this window. The instance's `done` state becomes `idle`, `queued` is cleared, and when the workspace maps to a context a time event is recorded for that context with source `deck` (a repeat within 2 seconds is ignored, as for any switch). A context that is parked is skipped without an error.
- `closed`: delete the instance. Closing an unknown instance is not an error.
- **Workspace mapping**: the workspace is compared with each context's `workdir`. Slashes and backslashes are equal, trailing slashes and `.`/`..` segments are ignored, and the comparison ignores case on Windows and macOS and for drive-letter paths such as `C:\...`. A workspace that matches no context is reported as an *unknown workspace* (see the Agents page); mapping it later is applied to all matching instances.

Response `200`:

```json
{
  "instance": {
    "id": "win-3f2a", "slot": 2, "context_id": "acme", "workspace": "C:\\Work\\acme-api",
    "window_title": "main.go - acme-api - Visual Studio Code", "state": "action_required",
    "summary": "Wants to run: go test ./...", "updated": "2026-11-20T10:15:00.123+01:00",
    "queued": false, "context_label": "ACME", "label": "ACME", "mapped": true
  },
  "switched": false
}
```

`instance` is `null` after `closed`. `slot` is `0` when unknown. `label` is the context label, else the workspace folder name, else the window title, else the id. `switched` is true when a time event was recorded.

## `POST /api/agents/sync`

The full set of instances the plugin currently knows.

```json
{"instances": [
  {"instance": "win-3f2a", "state": "working", "slot": 1, "workspace": "C:\\Work\\acme-api", "window_title": "...", "summary": ""},
  {"instance": "win-91bc", "state": "idle", "slot": 2}
]}
```

Each element has the event fields above (`event` is ignored, `state` is required). A sync is a snapshot:

- Instances missing from it are **deleted**. An empty list clears everything.
- Fields missing from an element are cleared (slot, workspace, window title, summary), except that the `queued` flag is always kept.
- An instance whose state did not change keeps its `updated` time, so a periodic sync does not keep `done` instances from going idle. A new or changed instance gets the current time.
- At most 200 instances; duplicates are rejected. A bad element rejects the whole sync (`400`) and changes nothing.

Response `200`: the list described under `GET /api/agents`.

## Idle rules

A background job runs every minute while `stkr serve` is running:

- `done` with no event for 30 minutes becomes `idle`.
- Any instance with no event for 24 hours becomes `idle`.

Instances are never deleted by these rules; only `closed` or a sync removes them.

## Reading and acting

| Route | Purpose |
|---|---|
| `GET /api/agents` | `{"instances":[...], "counts":{"error":0,"action_required":0,"done":0,"working":0,"idle":0}, "unknown_workspaces":[{"workspace":"...","instances":1}], "next": <instance or null>}`. Instances are ordered by slot (unknown slots last). |
| `GET /api/agents/next` | `{"instance": <instance>}` or `{"instance": null}`. |
| `POST /api/agents/next/go` | Choose the next instance, record the switch (source `deck`, or `api` without the Deck header), clear its `done`/`queued`, focus its window, fall back to opening the workspace. Response: `{"result": "...", "instance": <instance or null>, "switched": bool, "message": "..."}`. |
| `POST /api/agents/{instance}/queue` | Mark an instance queued. `404` if unknown. Response `{"instance": <instance>}`. |
| `POST /api/agents/ack-done` | Set every `done` instance to `idle`. Response `{"acknowledged": <count>}`. |
| `GET /img/agents.png` | The agents key image, see [streamdeck-keys.md](streamdeck-keys.md). |

### Next-instance order

1. State: `error`, then `action_required`, then `done`, then everything else (`working` and `idle`).
2. Within a state, **queued instances come first**.
3. Then the oldest `updated`, then the lowest slot, then the id.

Queueing therefore never lets an instance jump over a more urgent state; it only moves it to the front of its own state. Focusing an instance (or `next/go`) clears `queued` and refreshes `updated`, so repeated presses rotate through the instances.

### `next/go` results

| `result` | Meaning |
|---|---|
| `focused` | A window was brought to the front. |
| `launched` | No window was found; the workspace was opened with the `launch.*` command (default `code <workspace>`). |
| `none` | There are no instances. |
| `failed` | No window and the workspace could not be opened (or the instance has no workspace). `message` says why. The time switch, if any, was still recorded. |
| `pending` | Focusing took longer than 2 seconds. It continues in the background. |

Window search tries the instance's `window_title`, then its workspace folder name; a window matches when its title contains the text (case-insensitive). If both are empty, the `agent_window_title` setting is used. Window focus works on Windows only; elsewhere STKR goes straight to opening the workspace. The call itself answers `200` even when `result` is `failed`.

## Errors

| Status | `error` | Cause |
|---|---|---|
| 400 | `invalid` | Bad JSON, missing `instance`, unknown `event` or `state`, `slot` outside 1-6, sync too large or with duplicates. |
| 404 | `not_found` | `queue` for an unknown instance. |
| 413 | `too_large` | Body over 1 MiB. |
| 421 / 403 | `invalid_host` / `forbidden_origin` | Host or Origin guard. |

Summaries, window titles and workspaces are never written to the log; only their lengths are.
