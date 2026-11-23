## stkr activity

Show the local activity evidence of a day

### Synopsis

Summarise the activity events a local client (the Stream Deck plugin) sent for one day: time per lock state, category, application and workspace, and the locked, idle and break periods. Activity in personal and system contexts is counted as private time only.

```
stkr activity [flags]
```

### Examples

```
  stkr activity
  stkr activity --date yesterday --json
```

### Options

```
      --date string   day: YYYY-MM-DD, today or yesterday (default "today")
  -h, --help          help for activity
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

