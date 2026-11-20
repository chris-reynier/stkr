## stkr switch

Start working on a context

### Synopsis

Record that you are now working on <context>. The previous block ends now. Switching to the context that is already active within 2 seconds does nothing, so double clicks are safe. The context must exist: see `stkr ctx list` and `stkr ctx add`.

```
stkr switch <context> [flags]
```

### Examples

```
  stkr switch acme
  stkr switch meeting -n "weekly sync"
```

### Options

```
  -h, --help          help for switch
  -n, --note string   short note stored with the switch
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

