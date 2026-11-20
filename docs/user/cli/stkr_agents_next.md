## stkr agents next

Show (or go to) the agent instance that needs attention next

### Synopsis

Show the next instance: error first, then action_required, then done, then the oldest. With --go, also log the switch to its context, focus its window (Windows) or open its workspace with the launch command, and clear its done/queued flags.

```
stkr agents next [flags]
```

### Options

```
      --go     switch to the instance's context and focus its window
  -h, --help   help for next
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr agents](stkr_agents.md)	 - List coding agent instances reported by the Stream Deck plugin

