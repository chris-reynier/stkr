## stkr import-legacy

Archive files from the earlier Python version (copy only)

### Synopsis

Copy the known legacy files ([tasklog.json entries.json archive.json settings.json itemcount.json]) found in <path> into backups/legacy-<timestamp>/. The files are only copied for reference: they are not parsed, the originals are not changed and the database is not opened.

```
stkr import-legacy <path> [flags]
```

### Examples

```
  stkr import-legacy ~/old-tool-data
```

### Options

```
  -h, --help   help for import-legacy
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

