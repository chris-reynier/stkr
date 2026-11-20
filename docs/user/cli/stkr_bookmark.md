## stkr bookmark

Save a link

### Synopsis

Save an http or https link. The page title is read from the page (2 second limit); if that fails, or `bookmarks.fetch_title` is off, the URL is the title. A saved URL is reported and exits with code 1.

```
stkr bookmark <url> [flags]
```

### Examples

```
  stkr bookmark https://go.dev/doc/ --tags go,docs
```

### Options

```
  -h, --help          help for bookmark
      --tags string   comma-separated tags
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager

