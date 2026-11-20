## stkr ai test

Send a fixed harmless sample to the AI provider

### Synopsis

Sends one fixed sentence (never your data) to the configured provider and reports whether a valid answer came back. Exits with code 2 when the provider is not active or did not answer.

```
stkr ai test [flags]
```

### Examples

```
  stkr ai test
```

### Options

```
  -h, --help   help for test
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr ai](stkr_ai.md)	 - Check and use the optional AI provider

