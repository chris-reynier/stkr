# Stream Deck keys contract (v1)

Which key calls which endpoint and which image it shows. All URLs are on `http://127.0.0.1:<port>`. Mutating calls are `POST` only; add the header `X-STKR-Source: deck` (or `?src=deck`) so time events are recorded as coming from the deck. Later features add rows to this table.

| Key | Press action | Image | Status |
|---|---|---|---|
| Context | `POST /api/switch/<context-id>` | none | available |
| Stop | `POST /api/stop` | none | available |
| Now | none | `GET /img/now.png` | available |
| Agents | `POST /api/agents/next/go` | `GET /img/agents.png` | available |
| Agents (acknowledge) | `POST /api/agents/ack-done` | none | available |
| Start Day | `POST /api/day/start` | none | available |
| Close Day | `POST /api/close/prompt?paste=1` | `GET /img/key/close.png` | available |
| Import | `POST /api/import/clipboard` | `GET /img/key/import.png` | available |
| Idea (voice) | `POST /api/idea/record/toggle` | `GET /img/key/idea.png` | available |
| Ideas inbox | none | `GET /img/key/ideas-inbox.png` | available |
| Bookmark | `POST /api/bookmark/from-clipboard` | none | available |
| Meeting action or decision | `POST /api/idea` with `{"text":"...","type":"action"}` (or `decision`) | none | available |

All images are 144x144 PNG, generated locally, `Cache-Control: no-store`.

## `/img/now.png`

See [http-api.md](http-api.md#get-imgnowpng).

## `/img/agents.png`

Reflects all agent instances at the moment of the request.

| Part | Content |
|---|---|
| Background | Worst state: `error` red, `action_required` orange, `done` green, `working` blue, `idle` grey. |
| Line 1 | Worst state word: `ERROR`, `ACTION`, `DONE`, `WORKING` or `IDLE`. |
| Line 2 | Counts: `E` error, `A` action required, `D` done, `W` working (idle instances are not counted). Example `E1 A0 D2 W3`. |
| Line 3 | Label of the instance the next press goes to (see the order in [agent-events.md](agent-events.md)). Missing when there are no instances. |

With no instances the key is grey and reads `AGENTS` / `none`. The picture changes only when the state changes (identical pictures are served from a cache), so polling every few seconds is cheap.

Pressing the key sends `POST /api/agents/next/go`: it records the time switch for the instance's context, focuses its window (or opens its workspace) and clears its `done` and `queued` flags.

## `/img/key/close.png`

The Close Day key. It is computed from the clock and the stored state on every request, so the plugin only has to poll (once per second gives the blink).

| State | When | Look |
|---|---|---|
| Neutral | Before `close_reminder`, on a day without tracked time, or on a weekend | grey, `CLOSE` / `DAY` |
| Due | After `close_reminder` on a working day while no close was imported today | alternates every second (even and odd seconds of the clock) between yellow with dark text and a dark background, `CLOSE` / `DAY` / `due` |
| Saved | A `stkr-close` block was imported today | green, `CLOSE` / `saved` |

A working day is a weekday with at least one event that is not `off`. A brief does not count as a close. A close that was missed is listed in the next Start Day digest.

Pressing the key sends `POST /api/close/prompt?paste=1&src=deck`: it copies the Close Day prompt and pastes it into the window set in `agent_window_title`. The AI answer is then imported with the Import key.

## `/img/key/import.png`

The Import key. Pressing it sends `POST /api/import/clipboard`; the image shows the result for 10 seconds and then returns to neutral. The state is stored (not held by the connection), so it also reflects imports done from the CLI or the web UI.

| State | Look | Meaning |
|---|---|---|
| Neutral | grey, `IMPORT` / `clipboard` | Nothing happened in the last 10 seconds. |
| OK | green, `IMPORT` / `OK` | Imported without warnings. |
| Warning | yellow, `IMPORT` / `WARN` | Imported, but with ignored lines, unfenced lines or unmatched `done` lines. |
| Failed | red, `IMPORT` / `FAILED` | No valid block: nothing was written. The response carries `redirect: "/ui/today#import"`. |

## Start Day

`POST /api/day/start?src=deck` logs `planning`, copies the Start Day prompt to the clipboard and writes the digest file. It has no image. Open your AI chat, paste (Ctrl+V), then press the Import key with the answer copied.

## `/img/key/idea.png`

The voice idea key. Pressing it sends `POST /api/idea/record/toggle`; the image follows the recorder of the running `stkr serve`.

| State | Look |
|---|---|
| Idle | grey, `IDEA` / `record` |
| Recording | red, `IDEA` / `REC` / `tap to stop` |

A press while ffmpeg is missing answers `503`, and the key stays grey.

## `/img/key/ideas-inbox.png`

The number of ideas with status `new` and tag `pro` (personal ideas are not counted). Blue `IDEAS` / `<n> new` when there are some, grey `IDEAS` / `0 new` otherwise. The key has no press action; open `/ui/ideas` to triage.
