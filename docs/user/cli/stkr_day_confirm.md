## stkr day confirm

Confirm an idle-suspect block so it counts

### Synopsis

Mark the block started by <event-id> as worked. Idle-suspect blocks count only once confirmed. Use --undo to remove the mark.

```
stkr day confirm <event-id> [flags]
```

### Options

```
  -h, --help   help for confirm
      --undo   remove the confirmation
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr day](stkr_day.md)	 - Show the blocks, gaps and totals of a day

