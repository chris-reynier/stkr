## stkr start-day

Log planning and copy the Start Day prompt to the clipboard

### Synopsis

Switch to the planning context, build a digest of the last five working days (at most 2,500 characters), render the Start Day prompt and copy it to the clipboard. When onedrive_dir is set, the digest is also written to _daily/daily-digest.md there. Paste the prompt into your AI chat, then run `stkr import` on the answer.

The prompt template can be customised: see `stkr prompts init`.

```
stkr start-day [flags]
```

### Examples

```
  stkr start-day
  stkr start-day --no-clipboard --print
```

### Options

```
  -h, --help           help for start-day
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

