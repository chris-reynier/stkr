# Capture

## What it does

Write ideas down the moment you have them, by typing, speaking or dropping a file from your phone. Capture is always saved first. Transcription and later AI sorting never block it and never lose it.

- **Text ideas**: one line from the terminal, a Stream Deck key, the web UI or the API.
- **Voice notes**: press a key to start recording, press again to stop. The recording is saved at once and transcribed in the background with whisper.
- **Phone inbox**: a folder (for example a synced cloud folder) that STKR checks every 30 seconds. Audio and text files in it become ideas.
- **Meeting items**: during a meeting, capture an action or a decision. It is filed under the project you worked on before the meeting.

Ideas are tagged `pro` (default) or `personal`. Personal ideas are kept out of exports and are never sent to a cloud AI provider (see [AI providers](ai-providers.md)).

## How to use

### Text

```
stkr idea "try htmx swaps"
stkr idea "dentist on Tuesday" --personal
stkr ideas                      # newest first
stkr ideas --status new --tag pro --json
```

Open **Ideas** in the web UI (<http://127.0.0.1:7878/ui/ideas>) to sort them: set a status (`new`, `triaged`, `done`, `archived`), a project, a tag and a title. Voice notes have a player.

### Meeting actions and decisions

```
stkr idea "send the budget to Sam" --type action
stkr idea "we use Postgres" --type decision
stkr idea "they liked the demo" --type note
```

Actions and decisions become items of the project you tracked last before the meeting (switch to `projectA`, then to `meeting`, then capture: the item belongs to `projectA`). With no earlier project it goes to `other`. Typing the same text again does not create a second item. Items appear on Today and in the Close Day prompt like imported ones. A `note` is saved as an idea.

Meeting items cannot be personal.

### Voice

Voice needs two free programs, and `stkr serve` running (the server holds the recording).

1. Install **ffmpeg** (<https://ffmpeg.org/download.html>; on Windows `winget install ffmpeg`).
2. Install **whisper.cpp** and download a model (for example `ggml-base.en.bin`). Put `whisper-cli` on your PATH, or set `whisper.bin`.
3. Tell STKR where they are and which microphone to use:

   ```
   stkr config set whisper.model "C:\models\ggml-base.en.bin"
   stkr config set mic_device "Microphone (USB Audio Device)"
   ```

   On Windows `mic_device` is the DirectShow name. List your devices with:

   ```
   ffmpeg -list_devices true -f dshow -i dummy
   ```

   Use the name in quotes from the "audio devices" list. On Linux (PulseAudio) and macOS (AVFoundation) an empty `mic_device` uses the default input; otherwise give the PulseAudio source name or the AVFoundation device index.
4. Restart `stkr serve`.
5. Press a Stream Deck key that sends `POST http://127.0.0.1:7878/api/idea/record/toggle` (the key image `/img/key/idea.png` is red while recording), or use **Record a voice note** on the Ideas page. Press again to stop. Add `?tag=personal` for a personal note.

A recording stops by itself after 30 minutes. Audio files are kept in `~/.stkr/audio/`. If whisper is missing, the idea is kept with its audio and shown as "Voice note, no transcript". If ffmpeg is missing, the key press answers 503 and the Ideas page says so.

### Phone inbox

Point STKR at a folder your phone writes to:

```
stkr config set phone_inbox "C:\Users\you\OneDrive\stkr-inbox"
```

Restart `stkr serve`. Every 30 seconds STKR takes the supported files (`.m4a`, `.mp3`, `.wav`, `.ogg`, `.opus`, `.txt`), turns each into an idea and moves it to `processed/` inside the inbox. Audio is converted with ffmpeg and transcribed like a voice note. A file that is still being written (changed in the last 5 seconds) is left for the next round.

Name a file with the prefix `p_` or `perso_`, or put it in a `personal/` subfolder, to make it a personal idea. The exact rules are in the [phone inbox contract](../contracts/phone-inbox.md).

### Stream Deck keys

| Key | Action | Image |
|---|---|---|
| Idea (voice) | `POST http://127.0.0.1:7878/api/idea/record/toggle` | `/img/key/idea.png` (red while recording) |
| Ideas inbox | none (open `/ui/ideas`) | `/img/key/ideas-inbox.png` (number of new pro ideas) |
| Bookmark | `POST http://127.0.0.1:7878/api/bookmark/from-clipboard` | none |
| Meeting action or decision | `POST http://127.0.0.1:7878/api/idea` with body `{"text":"...","type":"action"}` | none |

Text from a key needs a plugin that can send a JSON body; if yours cannot, use the CLI or the web UI.

## Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `phone_inbox` | `""` | Folder to watch. Empty turns it off. |
| `ffmpeg` | `ffmpeg` | Program used to record and convert audio. |
| `mic_device` | `""` | Microphone name. Required on Windows. |
| `whisper.bin` | `whisper-cli` | Speech-to-text program. |
| `whisper.model` | `""` | Model file passed with `-m`. |

All keys are in [Configuration](configuration.md). Settings changes need a restart of `stkr serve` (the Windows desktop app applies them by itself).

## Troubleshooting

- **Ideas page says "ffmpeg was not found"**: install it or set `ffmpeg` to its full path. Check with `curl http://127.0.0.1:7878/api/capture/health`.
- **"set mic_device" error on Windows**: DirectShow needs the device name. See the list command above.
- **"no audio was recorded"**: the microphone name is wrong or another program holds the device. Run the ffmpeg list command and copy the name exactly.
- **Voice notes stay without text**: whisper is missing, `whisper.model` is wrong, or the speech was empty. Run `whisper-cli -m <model> -f <file.wav>` by hand to see its message. The audio is still in `~/.stkr/audio/`.
- **A phone file stays in the inbox**: ffmpeg is missing (audio only; STKR retries every 30 seconds), or the file is still syncing. Text files need no extra tools.
- **Nothing happens on the key press**: `stkr serve` must be running. The CLI `stkr idea` does not need it.
- Nothing you capture is written to the log; only ids and lengths are.
