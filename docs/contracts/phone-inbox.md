# Phone inbox contract (v1)

A folder that a phone (or any tool) drops files into. `stkr serve` turns the files into ideas. The folder is set with the `phone_inbox` config key; empty turns the feature off.

## Layout

```
<phone_inbox>/
  note.txt              -> idea, tag pro
  p_dentist.m4a         -> idea, tag personal
  perso_gift.txt        -> idea, tag personal
  personal/anything.ogg -> idea, tag personal
  processed/            -> handled files are moved here (created by STKR)
```

## Files taken

| Extension (any case) | Handling |
|---|---|
| `.txt` | The text (UTF-8, optional BOM, first 64 KiB) becomes the idea text. An empty file creates no idea and is only moved. |
| `.m4a`, `.mp3`, `.wav`, `.ogg`, `.opus` | Converted with ffmpeg to 16 kHz mono WAV in `~/.stkr/audio/`, stored as an idea with the audio and transcribed with whisper in the background. |

Everything else is ignored and left alone: other extensions, hidden files (name starts with `.`), folders other than `personal/`, and anything in `processed/`. Only the top level of the inbox and of `personal/` is read.

## Tag rules

The tag is `personal` when any of these is true, otherwise `pro`:

1. the file name starts with `p_` or `perso_` (any case),
2. the file is inside the `personal/` subfolder.

Only the start of the file name counts: `pro_p_x.txt` and `personal_x.txt` are `pro`.

## Timing and stability

- STKR scans every 30 seconds and handles at most 50 files per scan, oldest first.
- A file whose modification time is less than 5 seconds old is skipped until a later scan, so half-written files are not read. A modification time far in the future (more than one hour ahead) is treated as stable.

## After handling

The source file is moved to `processed/` with an atomic rename, keeping its name. If the name is taken, `-2`, `-3`, ... is added before the extension (`note.txt`, `note-2.txt`).

If a file cannot be handled (for example ffmpeg is missing for audio) it stays where it is and is retried at every scan. The problem is logged once per file, without the file contents.

## Idea fields set

| Field | Value |
|---|---|
| `source` | `phone` |
| `tag` | per the rules above |
| `status` | `new` |
| `ai_state` | `pending` (`skipped` for audio when no transcript could be made) |
| `audio_path` | file name inside the audio folder (audio only) |

## Health

`GET /api/capture/health` reports `phone_inbox`: `ok` (folder readable), `disabled` (not configured) or `error` (cannot be read).

## Errors

A missing or unreadable inbox folder makes the scan fail with a logged error; nothing else is affected. No file content, text or transcript is written to the log.
