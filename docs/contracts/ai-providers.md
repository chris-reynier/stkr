# AI providers contract (v1)

How STKR talks to AI providers and what it expects back. Version `v1`. The provider is chosen by `ai.provider` and that provider's `enabled` flag. Anything else means no AI.

## Operations

Every operation is one prompt in, one JSON object out. The prompt contains the instructions, the allowed projects (id and label only) and the input between `<<<TEXT` and `TEXT>>>` markers, which the model is told to treat as data. Prompts are embedded templates; they contain nothing else (no config values, paths or secrets).

| Operation | Input | JSON answer |
|---|---|---|
| classify | text, projects | `{"kind":"idea\|action\|decision\|note\|followup\|risk","project":"<id or empty>","tags":["..."],"confidence":0.0-1.0}` |
| cleanup | text, projects | `{"title":"<=80 chars","text":"<cleaned text>","suggested_project":"<id or empty>"}` |
| split | text | `{"notes":["...","..."]}` (1 to 50 notes) |
| plan | day digest | `{"plan":"<lines separated by \n>"}` |
| followups | open items as `<id> \| <project> \| <summary>` | `{"followups":[{"item":"<id from the list>","reason":"..."}]}` |

Validation (strict, a violation counts as invalid output):

- Exactly the fields above. Unknown fields and trailing data are rejected.
- `kind` is one of the six values; `confidence` is within 0 to 1; at most 5 tags.
- `cleanup.text` is not empty. A title longer than 200 characters is cut; an empty title becomes the first line of the text.
- A `project` or `suggested_project` that is not in the allowed list is replaced by an empty value (advice only, not an error).
- `followups` entries whose `item` is not in the input are dropped.
- Markdown code fences around the JSON and text before or after the object are removed before decoding.

## Failure handling

Each call has a timeout (`ai.timeout_seconds`) per attempt and at most **one** retry, only after invalid output or a timeout. A failed operation returns a usable fallback value (the raw text, a plain `note`, one note, no plan, no follow-ups) together with an error that matches `ErrUnavailable`. The error kind is one of:

| Kind | Meaning | Batch behaviour |
|---|---|---|
| invalid output | not the expected JSON, twice | idea becomes `failed` |
| timeout | no answer in time, twice | idea becomes `failed` |
| unreachable | provider not running or not installed, non-2xx answer, non-zero exit, `is_error` envelope | batch stops, ideas stay `pending` |
| disabled | no provider is active | nothing is called |

Errors and logs never contain prompt or answer text. Responses larger than 1 MB (1,048,576 bytes) are rejected as invalid output.

## Ollama (HTTP)

`POST {ollama.url}/api/chat` with

```json
{"model":"llama3.2","messages":[{"role":"user","content":"<prompt>"}],"format":"json","think":false,"stream":false}
```

The answer text is `message.content` of the response. Redirects are not followed. `GET {ollama.url}/api/version` gives the version for `stkr ai status`. The provider counts as local only when the URL host is `localhost` or a loopback address.

## Claude Code and GitHub Copilot (program)

The program is started without a shell: argv is `<bin>` plus the configured `args`. The prompt is written to standard input and standard output is the answer. These providers are never local.

- Claude Code default: `claude -p --output-format json`. The output is `{"type":"result","is_error":false,"result":"<text>",...}`. STKR uses `result` as the answer; `is_error: true` is treated as unreachable.
- GitHub Copilot default: `copilot -p "<short instruction>" --silent`; plain text output. These default flags could not be checked against an installed Copilot CLI and may need adjusting through `github_copilot.args`.
- Output that is not an object with a `result` field is used as it is.
- `<bin> --version` (2 second limit) gives the version for `stkr ai status`.

Example exchange with Claude Code:

```
stdin:  You tidy up a captured note ... <<<TEXT
        um so remind me to review the roadmap draft thursday
        TEXT>>>
stdout: {"type":"result","is_error":false,"result":"```json\n{\"title\":\"Review roadmap draft\",\"text\":\"Review the roadmap draft on Thursday.\",\"suggested_project\":\"\"}\n```"}
```

## Privacy gate

Before any call for an idea, if the provider is not local and the idea is tagged `personal` or was captured in a personal or system context, the idea is marked `skipped` and nothing is sent.

## Stored state

| Place | Content |
|---|---|
| `ideas.clean_text`, `title`, `suggested_project` | result of cleanup (an existing title or project is kept) |
| `ideas.ai_state` | `pending`, `done`, `failed`, `skipped` |
| `kv ai.last_ok` | RFC3339 time of the last successful call |
| `kv ai.last_err` | `<RFC3339>\|<operation>\|<error text>` of the last failure (no content) |

## CLI output (`--json`)

`stkr ai status --json`:

```json
{"provider":"ollama","active":true,"local":true,"reachable":true,"version":"0.9.0","last_ok":"2026-11-20T10:00:00Z","pending":2}
```

`stkr ai test --json`: `{"provider":"ollama","ok":true,"ms":850,"title":"Review the quarterly roadmap draft"}`.

`stkr ai run-once --json`: `{"done":2,"failed":0,"skipped":1,"waiting":0}`.

Errors exit with code 2 (provider not active or unavailable) and a message on standard error.
