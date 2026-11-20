## stkr ctx set

Change fields of a context

### Synopsis

Change the fields given as flags; other fields are kept. Pass an empty value (--ticket "") to clear a field.

```
stkr ctx set <id> [flags]
```

### Examples

```
  stkr ctx set acme --ticket ACME-99
```

### Options

```
  -h, --help              help for set
      --icon string       icon name
      --kind string       project, mode, personal or system (personal and system are never exported)
      --label string      display name
      --notebook string   notebook URL to open for this context
      --sort int          sort order in lists (lower first)
      --ticket string     timesheet ticket key used for exports
      --workdir string    working directory of this context
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr ctx](stkr_ctx.md)	 - List and manage contexts (what time is tracked against)

