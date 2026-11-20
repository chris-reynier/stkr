## stkr idea

Capture an idea, or a meeting action, decision or note

### Synopsis

Write an idea down. It is saved first; transcription and AI sorting happen later. Use --personal for ideas that must stay out of AI prompts and exports.

With --type action or decision the text becomes an item of the current project (the last project you tracked before the meeting, else `other`). With --type note it is saved as a meeting idea.

```
stkr idea <text> [flags]
```

### Examples

```
  stkr idea "try htmx swaps"
  stkr idea "dentist" --personal
  stkr idea "send the budget to Sam" --type action
```

### Options

```
  -h, --help          help for idea
      --personal      tag the idea as personal
      --type string   meeting capture: action, decision or note
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

