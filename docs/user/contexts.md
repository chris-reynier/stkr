# Contexts

## What it does

A context is something you track time against. Every switch names a context. Each context has:

| Field | Meaning |
|---|---|
| id | Short name used in commands: 1-40 characters `a-z`, `0-9`, `-`, `_`, starting with a letter or digit. It cannot be changed. |
| label | Display name. Defaults to the id. |
| kind | `project`, `mode`, `personal` or `system`. Time in `personal` and `system` contexts is never exported to timesheets. |
| ticket | Timesheet ticket key used for exports (optional). |
| notebook, workdir, icon | Optional links used by other features (opening a notebook or workspace, Stream Deck icons). |
| sort | Order in lists (lower first). |
| status | `active` or `parked`. You cannot switch to a parked context. Its history is kept. |

A new data folder starts with these contexts:

| id | kind | use |
|---|---|---|
| `off` | system | Not working. `stkr stop` switches here. It cannot be parked. |
| `break` | personal | Breaks. Never exported. |
| `planning` | mode | Planning your work. |
| `meeting` | mode | Meetings. |
| `other` | project | Work that has no project of its own. |

## How to use

```
stkr ctx list                         # active contexts
stkr ctx list --all                   # include parked ones
stkr ctx list --kind project
stkr ctx add acme --label "ACME rollout" --kind project --ticket ACME-12
stkr ctx set acme --ticket ACME-99    # change only the given fields
stkr ctx set acme --ticket ""         # clear a field
stkr ctx park acme                    # hide it and block switching to it
stkr ctx activate acme                # bring it back
```

Add `--json` for machine-readable output.

## Configuration keys

None. Contexts are stored in the database, not in `config.yaml`.

## Troubleshooting

| Problem | What to do |
|---|---|
| `invalid context id` | Use lower-case letters, digits, `-` and `_` only, at most 40 characters. |
| `context "x" already exists` | Pick another id, or change the existing one with `stkr ctx set`. |
| `context "x" is parked` when switching | Run `stkr ctx activate x`. |
| `"off" cannot be parked` / `the kind of "off" cannot change` | `off` is needed by `stkr stop` and is fixed. |
