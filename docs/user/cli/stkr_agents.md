## stkr agents

List coding agent instances reported by the Stream Deck plugin

### Synopsis

List the agent instances STKR knows about, with their state and context. Instances arrive from the Stream Deck plugin or hooks through `stkr serve`.

```
stkr agents [flags]
```

### Examples

```
  stkr agents
  stkr agents next
  stkr agents next --go
```

### Options

```
  -h, --help   help for agents
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager
* [stkr agents next](stkr_agents_next.md)	 - Show (or go to) the agent instance that needs attention next

