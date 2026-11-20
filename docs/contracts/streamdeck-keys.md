# Stream Deck keys contract (v1)

Which key calls which endpoint and which image it shows. All URLs are on `http://127.0.0.1:<port>`. Mutating calls are `POST` only; add the header `X-STKR-Source: deck` (or `?src=deck`) so time events are recorded as coming from the deck. Later features add rows to this table.

| Key | Press action | Image | Status |
|---|---|---|---|
| Context | `POST /api/switch/<context-id>` | none | available |
| Stop | `POST /api/stop` | none | available |
| Now | none | `GET /img/now.png` | available |
| Agents | `POST /api/agents/next/go` | `GET /img/agents.png` | available |
| Agents (acknowledge) | `POST /api/agents/ack-done` | none | available |

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
