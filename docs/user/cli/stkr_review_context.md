## stkr review context

Print the review metadata of a day as JSON

### Synopsis

Print the same JSON as GET /api/review/context: the selected date and why it was chosen, tracked blocks, gaps, totals, contexts, open items, new ideas, agents, local activity, close state and missing evidence. Nothing is changed.

```
stkr review context [flags]
```

### Examples

```
  stkr review context
  stkr review context --date 2026-11-20
```

### Options

```
      --date string   day, YYYY-MM-DD (default: the previous working day)
  -h, --help          help for context
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr review](stkr_review.md)	 - Copy the daily review prompt of the previous working day to the clipboard

