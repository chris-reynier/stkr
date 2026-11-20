## stkr ai

Check and use the optional AI provider

### Synopsis

The AI provider only tidies up captured ideas and can suggest a plan for the day. Choose it with `stkr config set ai.provider ollama|claude_code|github_copilot` and enable that provider (for example `stkr config set ollama.enabled true`). Restart `stkr serve` after changing it.

### Options

```
  -h, --help   help for ai
```

### Options inherited from parent commands

```
      --debug         write debug logs to stderr
      --home string   data directory (default $STKR_HOME or ~/.stkr)
      --json          machine-readable JSON output
```

### SEE ALSO

* [stkr](stkr.md)	 - Local-first task stack manager
* [stkr ai run-once](stkr_ai_run-once.md)	 - Sort pending ideas now
* [stkr ai status](stkr_ai_status.md)	 - Show the AI provider, whether it is reachable and its version
* [stkr ai test](stkr_ai_test.md)	 - Send a fixed harmless sample to the AI provider

