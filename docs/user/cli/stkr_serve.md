## stkr serve

Run the local HTTP server and background jobs

### Synopsis

Start the HTTP API on 127.0.0.1 and the background jobs (daily backup, idle agent sweeper, close reminder). The server only accepts connections from this computer. Stop it with Ctrl+C.

The port comes from the `port` config key (default 7878); --port overrides it for this run. --port 0 picks a free port.

```
stkr serve [flags]
```

### Examples

```
  stkr serve
  stkr serve --port 7999
```

### Options

```
  -h, --help       help for serve
      --port int   port to listen on (default from config) (default 7878)
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

