# Bookmarks

## What it does

Save a link in one step and find it again later. STKR stores the URL, a title and optional tags. The title is read from the page.

## How to use

```
stkr bookmark https://go.dev/doc/ --tags go,docs
stkr links                 # all links, pinned first
stkr links docs            # search title, URL and tags
```

Copy a link, then press a Stream Deck key that sends `POST http://127.0.0.1:7878/api/bookmark/from-clipboard` (add `?tags=a` for a tag). The first line of the clipboard is used.

The **Links** page (<http://127.0.0.1:7878/ui/links>) lets you add, search, pin and delete links.

Rules:

- Only absolute `http` and `https` URLs are accepted. URLs with a user name or password are refused.
- The title comes from the page's `<title>`. STKR downloads at most 512 KB and waits at most 2 seconds. If that fails, the URL is the title; the bookmark is still saved.
- Saving a URL you already have changes nothing. The command exits with code 1 and names the existing link. The API answers `409` with the stored bookmark.
- Tags are lower-cased and de-duplicated.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `bookmarks.fetch_title` | `true` | Download the page to read its title. Set to `false` to make no network call at all; the URL is then the title. |

This is the only network call bookmarks make.

## Troubleshooting

- **The title is the URL**: the page is slow, not HTML, needs a login, or `bookmarks.fetch_title` is `false`. The link is saved anyway.
- **"not an absolute http or https URL"**: include the `https://` prefix.
- **Clipboard key returns 503**: the system has no clipboard tool (on Linux install `xclip`, `xsel` or `wl-clipboard`).
