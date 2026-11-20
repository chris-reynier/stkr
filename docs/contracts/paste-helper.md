# Paste helper contract (v1)

The paste helper puts the text on the clipboard into the window of your AI tool: it waits for the window, brings it to the front and sends Ctrl+V, and optionally Enter. Start Day and Close Day use it. Scripts and the Stream Deck can call it directly. Windows only; other systems answer `503` `unavailable`.

## Request

```text
POST /api/paste?window=<title>&enter=0|1
```

| Parameter | Meaning |
|---|---|
| `window` | Text the window title must contain (case-insensitive, up to 200 characters). Optional: the `agent_window_title` setting is used when it is missing. If both are empty the call fails with `400` `invalid`. |
| `enter` | `1` or `true` presses Enter after the paste. Default `0`. |

There is no body. The text pasted is whatever is on the clipboard.

`POST /api/close/prompt?paste=1&window=<title>&enter=0|1` does the same after copying the Close Day prompt. The CLI equivalent is `stkr close --paste [--window <title>] [--enter]`; it runs in the same process and does not need `stkr serve`.

## Behaviour

1. Wait up to 10 seconds for a visible window whose title contains `window`.
2. Bring it to the front. Windows restricts this for background programs; STKR uses the usual workaround (attach to the foreground thread, tap Alt).
3. Wait a quarter of a second, then send Ctrl+V through `SendInput`.
4. With `enter=1`, wait a quarter of a second and send Enter.

STKR does not start the AI tool. Open the chat first (a Stream Deck key, `code <folder>`, a browser tab), then paste. The `launch.command` and `launch.args` settings open a workspace folder for the Agents feature and are not used by the paste helper.

The default window title is empty on purpose: the helper must not guess which tool you use. Set it once:

```text
stkr config set agent_window_title "Claude"
```

## Responses

Success:

```json
{"ok": true}
```

Errors:

| Status | Body | Meaning |
|---|---|---|
| 404 | `{"error":"window_not_found","message":"..."}` | No window with that title appeared within 10 seconds. The text is still on the clipboard when the call came from `close/prompt`. |
| 400 | `{"error":"invalid","message":"..."}` | No window title given and `agent_window_title` is empty, or the title is too long. |
| 503 | `{"error":"unavailable","message":"..."}` | Not Windows, or the keystroke was blocked (for example the target runs as administrator and STKR does not). |

The clipboard text and the window title are never written to the log.

## Limits

- The window must be in the current desktop session and visible (not minimised to the tray).
- A target running with higher privileges than STKR ignores the keystrokes.
- Only the first matching window is used. Pick a title that is specific enough.
