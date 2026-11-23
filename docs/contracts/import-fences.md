# Import fences contract (v1)

The text format STKR reads when you import the answer of an AI chat (`stkr import`, `POST /api/import`, `POST /api/import/clipboard`, the import box on the Today page). The Start Day, Close Day and daily review prompts ask the AI to answer in this format.

## Blocks

A block is a fenced code block (three or more backticks, or tildes) whose info string is one of:

| Info string | Used for |
|---|---|
| `stkr-brief v1` | Start Day answer: top 3 and new items |
| `stkr-close v1` | Close Day answer: new items and items to mark done |
| `stkr-review v1` | Daily review answer: dated review records (see [Review blocks](#review-blocks-stkr-review-v1)) |

Rules:

- The info string is case-insensitive. A missing version (`stkr-brief`) is read as `v1` with a warning. Any other version (`v2`) is ignored with a warning.
- Text before, between and after blocks is ignored. A text may hold several blocks, of any kind.
- Fences may be indented. A brief or close block that is not closed is read to the end of the text, with a warning (a review block that is not closed is rejected).
- Other fenced blocks (` ```text `, ` ```go `) are skipped.
- A UTF-8 byte order mark and Windows line endings (`\r\n`) are accepted.
- The text is limited to 256 KiB.

## Lines

Inside a block, each non-empty line is one record. Fields are separated by `|` and trimmed. Lines starting with `#` are comments. Control characters become spaces.

```text
top  | <rank 1-3> | <text>
item | <action|decision|note|followup|risk|achievement|gap> | <project> | <summary> | <due YYYY-MM-DD or empty>
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

## Review blocks (`stkr-review v1`)

The answer to the daily review prompt (`stkr review`, `GET /api/review/prompt`, see [review-context.md](review-context.md)). Each line is one dated record; an optional first line `date|type|context|text` is a header and is skipped.

````text
```stkr-review v1
date|type|context|text
2026-11-20|achievement|acme|Completed API authentication changes
2026-11-20|action|acme|Prepare deployment rollback plan
2026-11-20|risk|acme|Waiting for client security approval
2026-11-20|decision|meeting|Move the weekly review to Tuesday
2026-11-20|gap|administration|Confirm missing Friday meeting follow-up
```
````

| Field | Rule |
|---|---|
| `date` | `YYYY-MM-DD`, a real date, not after today. The item is stored with this date. |
| `type` | `achievement`, `action`, `decision`, `risk`, `gap`, `followup` or `note`. |
| `context` | A context id or label, matched like the `project` of an `item` line. Empty means `other`. |
| `text` | The rest of the line (it may contain `\|`), cut at 500 characters. |

Rules:

- **A block is atomic.** A malformed line (fewer than four fields, a bad or future date, empty text, a template placeholder such as `<text>`) rejects the whole block: nothing from it is stored, its lines count as `ignored`, `blocks_rejected` goes up and a warning names the block and the first bad line. A review block that is not closed is rejected the same way, because it may be a cut-off copy. Other blocks in the same text are not affected.
- **Unknown type.** A line with an unknown `type` is ignored with a warning; the rest of the block is imported, so nothing is lost silently.
- **Unknown context.** The record is stored under the text as written (it goes to the inbox log) and a warning lists the line numbers to check.
- **Personal and system contexts.** The record is stored but never exported to the project logs; it is counted in `private` and a warning lists the line numbers.
- **Duplicates.** Records with the same fingerprint (see below) inside one import are stored once and counted in `duplicates`. A record already stored counts in `items_existing`.
- `achievement` records are stored with status `done`; the others are `open`.
- A review never changes the top 3 or the `brief_saved` / `close_saved` flags.

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

1. Items are added with today's date (review records: with their own date), once per fingerprint.
2. `top` lines replace the stored top 3 of today. A brief sets `brief_saved`, a close sets `close_saved` for today (and the first close stamps `close_saved_at`).
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
  "review_blocks": 0,
  "blocks_rejected": 0,
  "duplicates": 0,
  "private": 0,
  "ignored": 1,
  "warnings": ["line 2: done lines are only used in stkr-close blocks"],
  "exported": ["_daily/inbox.md"]
}
```

`state` is `ok`, or `warn` when there are warnings or ignored lines. Warnings never repeat the text of the line, only its line number. `kind` is `brief`, `close`, `both`, `review`, or `brief+review` / `close+review` / `both+review` when a review block came with the others. A failed import (`422`) carries the same fields plus `error`, `message` and `redirect`.

## Preview (dry run)

`POST /api/import/preview` (body as for `POST /api/import`), `stkr import --dry-run` and the MCP tool `list_unapproved_review_records` parse the text with the same rules and write nothing: no item, no flag, no export, no key state. They return:

```json
{
  "valid": true, "kind": "review", "format": "fenced", "tops": 0, "done": 0, "blocks_rejected": 0, "ignored": 0,
  "records": [
    {"line": 3, "date": "2026-11-20", "type": "action", "context": "acme", "text": "Prepare deployment rollback plan",
     "fingerprint": "f5cb67d7030f5310", "status": "new", "known_context": true, "private": false}
  ],
  "warnings": []
}
```

`status` is `new` (an import would create it), `existing` (already stored) or `duplicate` (repeated earlier in the same text). Items of brief and close blocks are listed too, with today's date and no `line`. `stkr import --dry-run` exits with code 2 when `valid` is false.

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
| Review block with a malformed line, a future date, or not closed | Whole block rejected, counted in `blocks_rejected`; other blocks are imported. Only rejected blocks: as "no block". |
| Review line with an unknown type | Line ignored, counted in `ignored`, warning with the line number. |
| Text over 256 KiB | `400` `invalid`. |
| Clipboard not available (`/api/import/clipboard`) | `503` `unavailable`. |
