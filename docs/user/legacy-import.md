# Legacy import

## What it does

If you used the earlier Python version of this tool, `stkr import-legacy` keeps a reference copy of its data files. It only **copies** files:

- It does not read or convert them. Old tasks do not appear in STKR.
- It does not change, move or delete the originals.
- It does not open the STKR database, so it cannot damage it.

The files it copies, when present: `tasklog.json`, `entries.json`, `archive.json`, `settings.json`, `itemcount.json`. Other files are ignored.

## How to use

Point it at the old data folder, or at one of the files above:

```
stkr import-legacy ~/old-tool-data
stkr import-legacy ~/old-tool-data/tasklog.json
```

Output:

```
archived 3 file(s) to /home/you/.stkr/backups/legacy-20261120T180000Z
  tasklog.json
  entries.json
  settings.json
```

Add `--json` to get `{"dir": "...", "files": [...]}`.

## Configuration keys

None.

## Troubleshooting

| Problem | What to do |
|---|---|
| `no legacy files found` | The folder has none of the known files. Check the path. |
| `legacy path ...: no such file` | The path does not exist. |
| `archive directory already exists` | You ran it twice in the same second. Run it again. |
