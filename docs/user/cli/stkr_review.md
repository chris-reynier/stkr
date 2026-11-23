## stkr review

Copy the daily review prompt of the previous working day to the clipboard

### Synopsis

Build the review prompt of one working day and copy it to the clipboard. Without --date it is the previous working day: yesterday, or the last weekday with tracked time before it. The prompt holds the tracked blocks, gaps, totals, open items, new ideas, local activity and the evidence STKR does not have (meeting minutes, emails), and asks the AI to answer with one ```stkr-review v1 block. Paste the prompt into your assistant, then run `stkr import` on the answer.

Personal and system contexts keep their time but never their notes or items.

```
stkr review [flags]
```

### Examples

```
  stkr review
  stkr review --date 2026-11-20 --print --no-clipboard
  stkr review context --json
```

### Options

```
      --date string    day to review, YYYY-MM-DD (default: the previous working day)
  -h, --help           help for review
      --no-clipboard   do not touch the clipboard
      --print          print the prompt to standard output
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager
* [stkr review context](stkr_review_context.md)	 - Print the review metadata of a day as JSON

