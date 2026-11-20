## stkr import

Import the AI answer (stkr-brief / stkr-close block) from the clipboard, a file or stdin

### Synopsis

Read the answer of your AI chat and store it: items are added (never twice), the top 3 and the saved flags of today are updated, and the project logs under onedrive_dir are rewritten.

Without an argument the text comes from the clipboard; `-` reads standard input. The text must contain a ```stkr-brief v1 or ```stkr-close v1 fenced block; plain `top | ...` / `item | ...` / `done | ...` lines are accepted with a warning. When nothing usable is found, nothing is written and the command exits with code 2.

```
stkr import [file|-] [flags]
```

### Examples

```
  stkr import
  stkr import answer.txt
  cat answer.txt | stkr import -
```

### Options

```
  -h, --help   help for import
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

