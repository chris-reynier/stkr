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

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `port` | `7878` | Port in the URLs above. |

## Troubleshooting

- **404 not_found** for a switch: the context id is wrong (`stkr ctx list`).
- **409 conflict**: the context is parked; run `stkr ctx activate <id>`.
- **404 on a GET to `/api/switch/...`**: use POST.
- **Image stays blank**: open `http://127.0.0.1:7878/img/now.png` in a browser to check the server is reachable.
