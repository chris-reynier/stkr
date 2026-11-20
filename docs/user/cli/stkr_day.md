## stkr day

Show the blocks, gaps and totals of a day

### Synopsis

Show one day: each block (from an event to the next), idle-suspect blocks, timesheet gaps and totals per context. date is YYYY-MM-DD, today (default) or yesterday.

```
stkr day [date] [flags]
```

### Examples

```
  stkr day
  stkr day 2026-11-16
  stkr day confirm 42
```

### Options

```
  -h, --help   help for day
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager
* [stkr day confirm](stkr_day_confirm.md)	 - Confirm an idle-suspect block so it counts

