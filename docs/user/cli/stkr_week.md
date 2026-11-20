## stkr week

Show counted time per context and day for an ISO week

### Synopsis

Show counted time per context for each day of an ISO week (default: the current week). --csv prints unrounded hours per day and context for contexts that may be exported; personal and system contexts are left out.

```
stkr week [yyyy-Www] [flags]
```

### Examples

```
  stkr week
  stkr week 2026-W47
  stkr week 2026-W47 --csv > week.csv
```

### Options

```
      --csv    print exportable hours as CSV (date,context,label,tempo_ticket,hours)
  -h, --help   help for week
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

