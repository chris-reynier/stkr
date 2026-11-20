# Configuration

## What it does

Settings live in `config.yaml` in the data folder (`stkr config path` prints the location). Missing keys use the defaults below. An empty path disables the feature that uses it.

## How to use

```
stkr config show                       # effective settings, secrets masked
stkr config path                       # where the file is
stkr config set idle_gap_minutes 120   # change a value (validated before saving)
stkr config set ollama.url http://127.0.0.1:11434
stkr config set allowed_origins "http://localhost:3000,http://localhost:4000"
```

You can also edit the file by hand. Unknown keys are ignored with a warning. Invalid values are rejected.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `port` | `7878` | Local web/API port (1-65535). |
| `timezone` | `""` | IANA name such as `Europe/Oslo`. Empty means the system time zone. Decides which day a time belongs to. |
| `day_start` | `09:00` | Start of working hours (`HH:MM`). |
| `day_end` | `17:00` | End of working hours (`HH:MM`). Must be later than `day_start`. The last block of a day ends here. |
| `close_reminder` | `16:45` | After this time on a working day, an unsaved close makes the Close key blink and shows a notice on Today (`HH:MM`). See [Daily loop](daily-loop.md). |
| `idle_gap_minutes` | `90` | Gap after which a block is treated as idle until confirmed. |
| `rounding_hours` | `0.25` | Rounding step applied when exporting time. Must be above 0. |
| `onedrive_dir` | `""` | Folder for the project log files and the Start Day digest. Empty disables export. See [Daily loop](daily-loop.md). |
| `phone_inbox` | `""` | Folder watched every 30 seconds for phone captures. Empty disables it. See [Capture](capture.md). |
| `open_notebook_on_switch` | `false` | Open a context's notebook when you switch to it. |
| `mic_device` | `""` | Microphone name for voice capture. Required on Windows (DirectShow name); empty means the default device on Linux and macOS. See [Capture](capture.md). |
| `bookmarks.fetch_title` | `true` | Download a bookmarked page (2 s limit, 512 KB) to read its title. `false` keeps the URL as the title and makes no network call. See [Bookmarks](bookmarks.md). |
| `ffmpeg` | `ffmpeg` | Path or name of the ffmpeg program. |
| `whisper.bin` | `whisper-cli` | Speech-to-text program. |
| `whisper.model` | `""` | Path to the speech model file. |
| `ai.provider` | `none` | One of `none`, `claude_code`, `github_copilot`, `ollama`. |
| `ai.timeout_seconds` | `120` | Timeout for each AI call. |
| `ai.batch_minutes` | `15` | How often `serve` sorts pending ideas with the AI provider. Replaces `ollama.batch_minutes` (the old name still works). |
| `ai.plan_start_day` | `false` | Add an AI-suggested plan to the Start Day prompt (waits at most 5 seconds). |
| `claude_code.enabled` | `false` | Allow the Claude Code CLI as a provider. |
| `claude_code.bin` | `claude` | Program to run. |
| `claude_code.args` | `-p, --output-format, json` | Arguments passed to it. |
| `claude_code.window_title` | `""` | Window title used to find the agent window. |
| `github_copilot.enabled` | `false` | Allow the GitHub Copilot CLI as a provider. |
| `github_copilot.bin` | `copilot` | Program to run. |
| `github_copilot.args` | `-p, <short instruction>, --silent` | Arguments passed to it. |
| `github_copilot.window_title` | `""` | Window title used to find the agent window. |
| `ollama.enabled` | `false` | Allow a local Ollama server. |
| `ollama.url` | `http://127.0.0.1:11434` | Ollama address. |
| `ollama.model` | `llama3.2` | Model name. |
| `mcp.enabled` | `true` | Allow the MCP server command. |
| `agent_window_title` | `""` | Part of the title of your AI window: the default for pasting prompts (`close --paste`, `/api/paste`) and the last-resort window to focus for agents. |
| `launch.command` | `code` | Program used to open a workspace. |
| `launch.args` | `{path}` | Its arguments; `{path}` is the workspace folder. |
| `update.check` | `true` | Check for new releases (one request to GitHub). |
| `update.channel` | `stable` | `stable` or `beta` (beta also accepts pre-releases). See [Update](update.md). |
| `update.interval_hours` | `24` | Hours between update checks. |
| `allowed_origins` | empty | Extra browser origins allowed to call the local API. |

Many of these keys take effect only in features that arrive in later releases.

## Troubleshooting

- `stkr: invalid configuration: ...` lists every bad value. Fix it with `config set` or edit `config.yaml`.
- A `warning: unknown config key` message means a key is misspelled or from a different version.
- Fix a broken file by deleting `config.yaml`; defaults are used again.
