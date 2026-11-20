## stkr config set

Set a configuration value (dotted keys, for example ollama.url)

### Synopsis

Set a configuration value. Nested keys use dots, for example ai.provider. List values are comma-separated. Invalid values are rejected and nothing is written.

```
stkr config set <key> <value> [flags]
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr config](stkr_config.md)	 - Show or change the configuration

