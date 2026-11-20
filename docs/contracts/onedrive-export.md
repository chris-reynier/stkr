# Project log export contract (v1)

STKR writes one markdown file per project into a folder that is usually synced (for example OneDrive). The folder is the `onedrive_dir` setting. When it is empty, nothing is exported.

## Files

| File | Content |
|---|---|
| `<onedrive_dir>/<project>/log.md` | Items and time of the project context `<project>` (its context id). |
| `<onedrive_dir>/_daily/inbox.md` | Items of the `other` context, of mode contexts (planning, meeting) and of projects that match no context. |
| `<onedrive_dir>/_daily/daily-digest.md` | The Start Day digest of the last run. Overwritten each time. |

Items of personal and system contexts are never exported. Folders and files are created as needed. Every write is atomic (temporary file in the same folder, then rename); a file whose new content equals the old one is not touched.

## Layout of `log.md` and `inbox.md`

```markdown
# <header, preserved>

anything the user writes here is preserved

## Current focus

- [ ] action: Write the user guide (due 2026-11-20)
- [ ] risk: CI is slow on Windows

## Log

### 2026-11-16
- Time: 7h45m
- [ ] action: Write the user guide (due 2026-11-20)
- decision: Use SQLite

### 2026-11-13
- [x] followup: Call Bob

## Notes

Your own notes. STKR never changes this section.
```

| Part | Owner | Behaviour |
|---|---|---|
| Header: everything before the first `## ` heading | user | Preserved byte for byte. A new file gets `# <context label>` (or `# Inbox`). |
| `## Current focus` | STKR | Rewritten on every export: for `inbox.md` first the top 3 of today (`Top 1 (date): text`), then open actions, follow-ups and risks, soonest due date first, at most 10. `_Nothing open._` when empty. |
| `## Log` | STKR | Rewritten on every export from the database: one `### YYYY-MM-DD` heading per day with items or tracked time, **newest first**. Under it `- Time: 2h15m` (counted time on that context, last 90 days, project files only) and one bullet per item. `_No entries yet._` when empty. |
| `## Notes` | user | Preserved byte for byte, whatever it contains. A new file gets an empty one. |
| Any other `## ` section | user | Preserved in place. |

Item bullets:

| Item | Bullet |
|---|---|
| open action, follow-up, risk | `- [ ] <type>: <summary> (due YYYY-MM-DD)` (the due part only when set) |
| done action, follow-up, risk | `- [x] <type>: <summary>` |
| decision, note | `- <type>: <summary>` |

Matching rules:

- A section starts at a line that begins with `## ` and ends before the next such line. Matching of `Current focus`, `Log` and `Notes` ignores case. Lines inside fenced code blocks are not headings.
- If a STKR section is missing from an existing file it is added before `## Notes`, or at the end when there is no Notes section. A file without a Notes section does not get one.
- If a section occurs twice, only the first is rewritten.
- If the file uses Windows line endings, the sections STKR writes use them too.
- Files over 4 MiB are not rewritten (the export reports an error).

## Errors

An export error never fails an import: the data is already stored. The import result carries the warning `export failed: <file>: <reason>` and the log has the details. The next import or export retries.

## Example

After `stkr import` of a close block with `item | action | alpha | Write the user guide | 2026-11-20`, the file `alpha/log.md` holds the layout above, and everything under `## Notes` is exactly what it was before.
