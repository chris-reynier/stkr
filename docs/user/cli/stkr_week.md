## stkr week

Show counted time per context and day for an ISO week

### Synopsis

Show counted time per context for each day of an ISO week (default: the current week). --csv prints the Tempo import file: one row per day and ticket, hours rounded to rounding_hours after summing; personal and system contexts are left out, and contexts without a ticket are listed on stderr. With --raw the CSV has unrounded hours per day and context instead.

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
      --csv    print the Tempo CSV (date,ticket,hours,context,description)
  -h, --help   help for week
      --raw    with --csv: unrounded hours per context (date,context,label,tempo_ticket,hours)
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

