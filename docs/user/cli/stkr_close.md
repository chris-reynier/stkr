## stkr close

Copy the Close Day prompt to the clipboard (and optionally paste it)

### Synopsis

Build the Close Day prompt from today's time, items, ideas, agent state and the morning top 3, and copy it to the clipboard. With --paste, also paste it into the AI window whose title contains --window (default: the agent_window_title setting); this works on Windows only. Then run `stkr import` on the answer.

```
stkr close [flags]
```

### Examples

```
  stkr close
  stkr close --paste
  stkr close --paste --window "Claude" --enter
```

### Options

```
      --enter           press Enter after pasting
  -h, --help            help for close
      --no-clipboard    do not touch the clipboard (ignored with --paste)
      --paste           paste the prompt into the AI window (Windows)
      --print           print the prompt to standard output
      --window string   part of the window title to paste into (default: agent_window_title)
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

