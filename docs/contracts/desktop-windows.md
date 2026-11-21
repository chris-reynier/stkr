# Desktop windows contract (v1)

How the Windows desktop app (`stkrw.exe`) shows the web UI in its two windows, and the two interfaces other code can rely on: the messages a page may send to the app, and the `window.json` file. The pages themselves are the ones in [http-api.md](http-api.md).

## Windows

| Window | Address loaded | Behaviour |
|---|---|---|
| Panel | `http://127.0.0.1:<port>/ui/panel` (first load adds `?focus=idea` when opened for capture) | About 360 x 560, tool window (no taskbar button), above all windows while pinned. Closing (X, Esc) hides it. |
| App | the page asked for, default `/ui/today` | Normal resizable window with a taskbar button. Closing destroys it. |

Both are WebView2 windows. The browser data lives in `<home>/desktop/webview`. Pages are only ever loaded from the server of the app.

## Navigation policy

Each window remembers the origin of the page it was asked to load (`http://127.0.0.1:<port>`: scheme and host with port, compared without regard to case). Before every navigation the window compares the target with that origin:

| Target | Result |
|---|---|
| Same origin (any path) | Allowed. |
| `about:blank` | Allowed. |
| Another host, another port, another scheme (`https://127.0.0.1:<port>`), an address with a user name, `file:`, `data:`, `javascript:` or an empty address | Cancelled. |

A cancelled `http` or `https` target (without a user name) is opened in the default browser, the same as a `url:` message. A request for a new window (`window.open`, `target=_blank`) never opens a window: a page of the same origin opens in the app window, an `http` or `https` address opens in the default browser, anything else is ignored. The decision is `desktop.NavigationAllowed(origin, target)`. The log records that a navigation was blocked, never the address.

## Messages from a page to the app

A page sends a text with `window.chrome.webview.postMessage(text)`. The first-party script `/static/desktop.js` is the only sender. The app accepts exactly these messages and ignores everything else (no error is shown):

| Message | Effect |
|---|---|
| `hide` | Hide the panel. Ignored by the app window. |
| `pin` | Toggle "keep the panel on top" and save `desktop.panel_on_top`. |
| `app:<path>` | Open `<path>` in the app window. `<path>` starts with `/ui/`, has no spaces, backslashes or control characters and does not start with `/ui//`. It may carry a query string. |
| `url:<address>` | Open `<address>` in the default browser. Only `http` and `https` addresses with a host and no user name or password are accepted. |

A message is at most 2048 bytes and has no NUL, CR or LF. Messages never run commands, read files or reach the HTTP API.

The app calls two functions of the page, if they exist: `window.stkrPanelShown(focusIdea)` after the hidden panel is shown again (refresh now, focus the idea box) and `window.stkrPanelPinned(pinned)` after the pin changed.

## `<home>/desktop/window.json` (v1)

Remembers where the windows were. It is written atomically (temp file, fsync, rename) with permission 0600 when a window is moved, resized, hidden or closed. It is not configuration; deleting it resets the places.

```json
{
  "version": 1,
  "panel": { "x": 1560, "y": 480, "w": 360, "h": 560 },
  "app":   { "x": 100, "y": 80, "w": 1100, "h": 780, "maximized": true }
}
```

| Field | Meaning |
|---|---|
| `version` | `1`. A file with another version is ignored. |
| `panel`, `app` | Last place. Both are optional. `x`, `y`, `w`, `h` are screen pixels (the restored rectangle for a maximized app window). `maximized` is only used by `app`. A place with `w` or `h` of 0 or less is ignored. |

On start a saved place is moved and shrunk to lie inside the work area (the screen without the taskbar) of the monitor it overlaps most, or of the primary monitor if it overlaps none. The panel has a minimum size of 300 x 380 pixels, the app window 640 x 420 (scaled by the screen DPI). A missing, unreadable or invalid file gives the default place: the panel next to the mouse pointer, the app window centred.

## Errors

- WebView2 Runtime missing: one notification, and every window action opens the page in the default browser. The app keeps running.
- WebView2 installed but a window cannot be created (its environment or controller fails): the same fallback with another notification ("The STKR window could not be opened"). This never ends the program; the WebView2 wrapper in `internal/desktop/edge` returns the error instead of exiting.
- Another program already holds `desktop.hotkey`: one notification naming the key; the hotkey stays off.
- An invalid `desktop.hotkey` is refused by `stkr config set` and by Settings; a hand-edited invalid value makes the app show a notification and run without a hotkey.
