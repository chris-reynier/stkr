# AI providers

## What it does

An optional AI provider tidies up the ideas you capture: it fixes a rough voice transcript, writes a short title and suggests a project. It can also add a suggested plan to the Start Day prompt. STKR works fully without it.

The AI is advisory:

- Capture never waits for it. An idea is saved first; the AI works on it later in the background.
- Your original text (`raw_text`) is never changed. The result is stored next to it as the cleaned text.
- If the provider is missing, slow or answers nonsense, you keep the raw text and nothing else happens.

## How to use

Pick one provider, enable it, and restart `stkr serve` (settings are read when `serve` starts):

### Ollama (runs on your computer)

```
ollama pull llama3.2
stkr config set ai.provider ollama
stkr config set ollama.enabled true
stkr config set ollama.model llama3.2
stkr ai status
```

STKR calls `http://127.0.0.1:11434` by default (`ollama.url`). Nothing leaves your computer. Use a small model that follows JSON instructions; larger ones are slower but tidier.

### Claude Code

Needs the `claude` program, signed in.

```
stkr config set ai.provider claude_code
stkr config set claude_code.enabled true
stkr ai status
```

STKR runs `claude -p --output-format json` (change it with `claude_code.bin` and `claude_code.args`), sends the prompt on standard input, and reads the answer from standard output.

### GitHub Copilot

Needs the GitHub Copilot CLI (`copilot`), signed in.

```
stkr config set ai.provider github_copilot
stkr config set github_copilot.enabled true
stkr ai status
```

STKR runs `copilot` with `github_copilot.args` (default: `-p "<short instruction>" --silent`) and sends the prompt on standard input. The Copilot CLI changes often; if your version uses other flags, set `github_copilot.args` (for example `stkr config set github_copilot.args "-p,Read the task on standard input and reply with the JSON object only.,--silent"`; lists are comma-separated) and check it with `stkr ai test`. STKR does not store any Copilot credentials; sign in with the Copilot CLI itself.

### Check it and run it

```
stkr ai status      # provider, active, local, reachable, version, last success and failure, pending ideas
stkr ai test        # sends one fixed harmless sentence and shows the answer time
stkr ai run-once    # sort pending ideas now (up to 20)
```

`stkr serve` sorts pending ideas every `ai.batch_minutes` minutes (default 15). The **Settings** page has a **Test provider** button that does the same as `stkr ai test`.

### Plan in Start Day

```
stkr config set ai.plan_start_day true
```

`stkr start-day` then asks the provider for a short plan from the day's digest and adds it under "Suggested plan (AI)". It waits at most 5 seconds; if the provider is slow or fails, the prompt is built without it and a warning is shown.

## Privacy model

- Only the idea text and the list of your project ids and labels go into a prompt. Config values, folders, clipboard content and secrets are never sent.
- Ideas tagged `personal`, and ideas captured while a personal or system context (such as `break`) was active, are sent **only** to a provider that runs on your computer (Ollama at `127.0.0.1` or `localhost`). With Claude Code, GitHub Copilot or a remote Ollama URL these ideas are marked `skipped` and never leave.
- The Start Day plan uses the digest that STKR already builds for prompts; it contains no personal ideas or personal time.
- STKR logs the provider, the operation, the duration and the outcome. It never logs prompts, ideas or answers.
- The only network calls are to the provider you configured.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `ai.provider` | `none` | `none`, `ollama`, `claude_code` or `github_copilot`. |
| `ai.timeout_seconds` | `120` | Time allowed for each call (one retry on invalid output or a timeout). |
| `ai.batch_minutes` | `15` | How often `serve` sorts pending ideas. Replaces `ollama.batch_minutes`, which still works. |
| `ai.plan_start_day` | `false` | Add an AI plan to the Start Day prompt (5 second limit). |
| `ollama.enabled`, `ollama.url`, `ollama.model` | `false`, `http://127.0.0.1:11434`, `llama3.2` | Ollama provider. |
| `claude_code.enabled`, `.bin`, `.args` | `false`, `claude`, `-p, --output-format, json` | Claude Code provider. |
| `github_copilot.enabled`, `.bin`, `.args` | `false`, `copilot`, see above | GitHub Copilot provider. |

A provider is used only when `ai.provider` names it **and** its `enabled` flag is true. Otherwise STKR uses no AI and `stkr ai status` shows why.

## Troubleshooting

- **`active: no` and a warning**: the provider is not enabled, or the program or URL is empty or invalid. Fix the key named in the warning and restart `stkr serve`.
- **`reachable: no`**: Ollama is not running (`ollama serve`), the URL is wrong, or the program is not on your PATH. For Claude Code and Copilot run `claude --version` or `copilot --version` in a terminal.
- **Ideas stay `pending`**: the provider is unreachable (STKR tries again at the next batch), the idea has no text yet (voice notes wait for transcription), or no batch ran yet. Run `stkr ai run-once`.
- **Ideas become `failed`**: the provider answered twice with something that was not the expected JSON, or timed out twice. Try a larger model, raise `ai.timeout_seconds`, or check `stkr ai test`. The raw text is untouched.
- **Ideas become `skipped`**: they are personal and the provider is not local (see Privacy model).
- **Changed the provider but nothing changed**: restart `stkr serve`. CLI commands such as `stkr ai status` read the config every time.
- **Manual check of a CLI provider**: run `stkr ai test`. A good answer prints the time it took and a title for the sample sentence.
