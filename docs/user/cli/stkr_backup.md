## stkr backup

Back up the database now and print the file path

### Synopsis

Write a consistent copy of the database to the backups folder and print its path. The newest 14 backups are kept. To restore, stop STKR and copy a backup over stkr.db.

```
stkr backup [flags]
```

### Options

```
  -h, --help   help for backup
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

