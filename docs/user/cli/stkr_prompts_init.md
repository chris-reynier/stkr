## stkr prompts init

Copy the default prompt templates to ~/.stkr/prompts for editing

### Synopsis

Copy start-day.md.tmpl, close-day.md.tmpl and review-day.md.tmpl into the prompts folder of the data directory. A file there replaces the built-in template. Existing files are kept unless --force is given. Delete a file to go back to the default.

```
stkr prompts init [flags]
```

### Examples

```
  stkr prompts init
```

### Options

```
      --force   overwrite existing templates
  -h, --help    help for init
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr prompts](stkr_prompts.md)	 - Manage the Start Day, Close Day and review prompt templates

