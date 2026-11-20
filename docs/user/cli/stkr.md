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

* [stkr agents](stkr_agents.md)	 - List coding agent instances reported by the Stream Deck plugin
* [stkr backup](stkr_backup.md)	 - Back up the database now and print the file path
* [stkr bookmark](stkr_bookmark.md)	 - Save a link
* [stkr close](stkr_close.md)	 - Copy the Close Day prompt to the clipboard (and optionally paste it)
* [stkr config](stkr_config.md)	 - Show or change the configuration
* [stkr ctx](stkr_ctx.md)	 - List and manage contexts (what time is tracked against)
* [stkr day](stkr_day.md)	 - Show the blocks, gaps and totals of a day
* [stkr idea](stkr_idea.md)	 - Capture an idea, or a meeting action, decision or note
* [stkr ideas](stkr_ideas.md)	 - List captured ideas
* [stkr import](stkr_import.md)	 - Import the AI answer (stkr-brief / stkr-close block) from the clipboard, a file or stdin
* [stkr import-legacy](stkr_import-legacy.md)	 - Archive files from the earlier Python version (copy only)
* [stkr links](stkr_links.md)	 - List or search saved links
* [stkr now](stkr_now.md)	 - Show the current context and today's total
* [stkr prompts](stkr_prompts.md)	 - Manage the Start Day and Close Day prompt templates
* [stkr serve](stkr_serve.md)	 - Run the local HTTP server and background jobs
* [stkr start-day](stkr_start-day.md)	 - Log planning and copy the Start Day prompt to the clipboard
* [stkr stop](stkr_stop.md)	 - Stop tracking (switch to off)
* [stkr switch](stkr_switch.md)	 - Start working on a context
* [stkr version](stkr_version.md)	 - Print version information
* [stkr week](stkr_week.md)	 - Show counted time per context and day for an ISO week

