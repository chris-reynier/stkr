# Import fences contract (v1)

The text format STKR reads when you import the answer of an AI chat (`stkr import`, `POST /api/import`, `POST /api/import/clipboard`, the import box on the Today page). The Start Day and Close Day prompts ask the AI to answer in this format.

## Blocks

A block is a fenced code block (three or more backticks, or tildes) whose info string is one of:

| Info string | Used for |
|---|---|
| `stkr-brief v1` | Start Day answer: top 3 and new items |
| `stkr-close v1` | Close Day answer: new items and items to mark done |

Rules:

- The info string is case-insensitive. A missing version (`stkr-brief`) is read as `v1` with a warning. Any other version (`v2`) is ignored with a warning.
- Text before, between and after blocks is ignored. A text may hold several blocks, of both kinds.
- Fences may be indented. A block that is not closed is read to the end of the text, with a warning.
- Other fenced blocks (` ```text `, ` ```go `) are skipped.
- A UTF-8 byte order mark and Windows line endings (`\r\n`) are accepted.
- The text is limited to 256 KiB.

## Lines

Inside a block, each non-empty line is one record. Fields are separated by `|` and trimmed. Lines starting with `#` are comments. Control characters become spaces.

```text
top  | <rank 1-3> | <text>
item | <action|decision|note|followup|risk> | <project> | <summary> | <due YYYY-MM-DD or empty>
done | <fingerprint or summary>
```

| Record | Allowed in | Meaning |
|---|---|---|
| `top` | both | Priority number 1, 2 or 3 of today. Text is cut at 200 characters. A later line with the same rank wins. A `top` in a close block replaces the stored value too; the default Close Day prompt does not ask for one. |
| `item` | both | A new item for today. The summary is cut at 500 characters. |
| `done` | `stkr-close` | Marks an open item done. The value is the 16-character fingerprint (with or without a leading `#`) or the item's summary (compared ignoring case, spacing and trailing full stops). |

Details for `item`:

- `project` is matched against context ids, then against context labels (ignoring case). A match is stored as the context id. Anything else is kept as written (up to 64 characters). An empty project is `other`.
- The last field is the due date when it is empty or a real date `YYYY-MM-DD`. If it is anything else, the whole remainder is the summary, so a summary may contain `|`.
- A `done` line in a `stkr-brief` block is ignored with a warning.
- Placeholders such as `<summary>` are ignored. This way the prompt text itself, left on the clipboard, imports nothing.

## Fingerprint

An item is identified by its fingerprint:

```text
fingerprint = first 16 hex digits of sha256( lower( type | project | normalized summary ) )
normalized summary = summary in lower case, white space collapsed, trailing ".", "!" and spaces removed
```

`project` here is the stored project (the context id when it matched). Importing the same item again creates nothing, whatever its date. Example: `item | action | other | Write docs |` has fingerprint `01929f695f28d542`. The fingerprint is shown after `#` in the open-item lists of the prompts.

## Unfenced lines (fallback)

When the text has no `stkr-` block at all, lines of the form `top | ...`, `item | ...` or `done | ...` are accepted, optionally preceded by a list or quote marker (`-`, `*`, `>`). The import succeeds with the warning `no stkr block found: accepted N unfenced line(s)`. The kind is guessed: a `done` line means close, otherwise a `top` line means brief. If a block exists anywhere in the text, unfenced lines are ignored.

## What an import does

1. Items are added with today's date, once per fingerprint.
2. `top` lines replace the stored top 3 of today. A brief sets `brief_saved`, a close sets `close_saved` for today.
3. `done` lines mark open items done.
4. The affected project logs are exported (see [onedrive-export.md](onedrive-export.md)).
5. The Stream Deck import key shows the result for 10 seconds.

A text without a single usable record writes nothing, turns the key red and, over HTTP, answers `422` with `redirect: "/ui/today#import"`.

## Result

`POST /api/import` and the CLI (`--json`) return:

```json
{
  "ok": true,
  "state": "warn",
  "kind": "close",
  "format": "fenced",
  "items_created": 1,
  "items_existing": 0,
  "items_done": 1,
  "tops_saved": 0,
  "ignored": 1,
  "warnings": ["line 2: done lines are only used in stkr-close blocks"],
  "exported": ["_daily/inbox.md"]
}
```

`state` is `ok`, or `warn` when there are warnings or ignored lines. Warnings never repeat the text of the line, only its line number.

## Example

````text
Here is your plan for today.

```stkr-brief v1
top | 1 | Ship the import feature
top | 2 | Review the open pull requests
item | action | alpha | Write the user guide | 2026-11-20
item | risk | alpha | CI is slow on Windows |
```
````

## Error cases

| Input | Result |
|---|---|
| No block and no pipe lines | Nothing written. CLI exit code 2. HTTP `422` `no_valid_block`. |
| Block with only invalid lines | The same. |
| `top` rank outside 1-3, unknown item type, empty summary | Line ignored, counted in `ignored`, warning with the line number. |
| `done` that matches no open item | Warning `N done line(s) matched no open item`. |
| Text over 256 KiB | `400` `invalid`. |
| Clipboard not available (`/api/import/clipboard`) | `503` `unavailable`. |
