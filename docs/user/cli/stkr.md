## stkr

Local-first task stack manager

### Synopsis

STKR tracks what you are working on, keeps ideas and follow-ups, and exports time. Everything is stored locally in ~/.stkr (or $STKR_HOME).

```
stkr [flags]
```

### Options

```
      --debug         write debug logs to stderr
  -h, --help          help for stkr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr backup](stkr_backup.md)	 - Back up the database now and print the file path
* [stkr config](stkr_config.md)	 - Show or change the configuration
* [stkr ctx](stkr_ctx.md)	 - List and manage contexts (what time is tracked against)
* [stkr day](stkr_day.md)	 - Show the blocks, gaps and totals of a day
* [stkr import-legacy](stkr_import-legacy.md)	 - Archive files from the earlier Python version (copy only)
* [stkr now](stkr_now.md)	 - Show the current context and today's total
* [stkr stop](stkr_stop.md)	 - Stop tracking (switch to off)
* [stkr switch](stkr_switch.md)	 - Start working on a context
* [stkr version](stkr_version.md)	 - Print version information
* [stkr week](stkr_week.md)	 - Show counted time per context and day for an ISO week

