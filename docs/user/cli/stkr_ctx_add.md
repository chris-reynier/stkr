## stkr ctx add

Create a context

### Synopsis

Create a context. The id is 1-40 characters a-z, 0-9, '-' or '_'. The label defaults to the id and the kind to project.

```
stkr ctx add <id> [flags]
```

### Examples

```
  stkr ctx add acme --label "ACME rollout" --kind project --ticket ACME-12
```

### Options

```
  -h, --help              help for add
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

