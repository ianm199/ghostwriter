# Ghostwriter — Meeting Transcription Daemon

*"The `ffmpeg` of meeting transcription."*

An open-source, local-first daemon that detects meetings, captures audio, transcribes via Whisper, and outputs structured transcript files. No cloud, no bots, no UI required.

---

## Design Philosophy

- **Daemon, not an app.** Runs in the background. Tray icon for status. No Electron, no browser window, no "meeting viewer."
- **Files as the interface.** Transcripts are `.json` files on disk. Grep them, git them, pipe them into whatever you want.
- **Composable by default.** The daemon does capture + transcription. Everything else (summarization, search, UI) is someone else's problem — or yours, via plugins/MCP.
- **Zero-config start, deep-config later.** `brew install ghostwriter && ghostwriter start` should just work. Power users can swap models, point at remote APIs, customize output formats.

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│                  Ghostwriter Daemon              │
│                                                  │
│  ┌──────────────┐    ┌────────────────────────┐  │
│  │   Detector    │───▶│    Audio Capture       │  │
│  │              │    │                        │  │
│  │ • Process    │    │ • System audio (WASAPI/ │  │
│  │   monitor    │    │   CoreAudio/PulseAudio) │  │
│  │ • Calendar   │    │ • Optional mic input    │  │
│  │   polling    │    │ • WAV buffer → segments │  │
│  │ • Manual     │    └───────────┬────────────┘  │
│  │   trigger    │                │               │
│  └──────────────┘                ▼               │
│                       ┌──────────────────────┐   │
│                       │   Transcription      │   │
│                       │                      │   │
│                       │ • Local: whisper.cpp  │   │
│                       │ • Local: faster-      │   │
│                       │   whisper             │   │
│                       │ • Remote: OpenAI /    │   │
│                       │   Deepgram / Gladia   │   │
│                       └──────────┬───────────┘   │
│                                  │               │
│                                  ▼               │
│                       ┌──────────────────────┐   │
│                       │   Output Pipeline     │   │
│                       │                      │   │
│                       │ • .transcript.json   │   │
│                       │ • Optional: .srt,    │   │
│                       │   .txt, .md          │   │
│                       │ • Webhook / callback │   │
│                       └──────────────────────┘   │
│                                                  │
│  ┌──────────────────────────────────────────┐    │
│  │            MCP Server                    │    │
│  │                                          │    │
│  │ • list_transcripts(date?, query?)        │    │
│  │ • get_transcript(id)                     │    │
│  │ • search_transcripts(query)              │    │
│  │ • get_meeting_context(meeting_id)        │    │
│  └──────────────────────────────────────────┘    │
└─────────────────────────────────────────────────┘
```

---

## Tech Stack

### Core Daemon — Go

Go is the right call here for a few reasons:

- **Single binary distribution.** `brew install` or download a binary. No Python environment, no Node runtime, no Docker. This is critical for adoption.
- **Long-running background process.** Goroutines and channels are a natural fit for a daemon that juggles process monitoring, audio capture, and transcription concurrently.
- **Cross-platform.** Go's cross-compilation story is mature. One codebase, three platforms.
- **Pragmatic FFI-free approach.** Instead of binding directly to system audio APIs, the daemon shells out to FFmpeg for audio capture and whisper-cli for transcription. Fewer build dependencies, easier to package.

Key dependencies:
- `cobra` — CLI framework (`github.com/spf13/cobra`)
- `encoding/json` — transcript serialization (stdlib)
- `os/exec` — subprocess management for FFmpeg and whisper-cli (stdlib)
- `net` — Unix domain socket IPC between CLI and daemon (stdlib)
- FFmpeg — audio capture via avfoundation (macOS) with BlackHole virtual audio device
- `whisper-cli` — local transcription via whisper.cpp command-line tool

### Transcription Engine — Pluggable

The daemon uses `whisper-cli` (the whisper.cpp command-line tool) as the default, but the transcription layer is a Go interface:

```go
type Transcriber interface {
    Transcribe(audio capture.AudioData) (*output.Transcript, error)
    TranscribeFile(path string) (*output.Transcript, error)
    Close() error
}

type WhisperCLITranscriber struct { /* model path, params */ }

type OpenAITranscriber struct { /* api key, model */ }
type DeepgramTranscriber struct { /* api key */ }
type GladiaTranscriber struct { /* api key */ }

type CustomTranscriber struct { /* arbitrary HTTP endpoint */ }
```

Default ships with `whisper-base.en` for speed. Users can download larger models (`whisper-large-v3`) for better accuracy. The CLI handles model management:

```bash
ghostwriter models list
ghostwriter models download large-v3
ghostwriter config set transcription.model large-v3
```

### Speaker Diarization

This is the one area where pure Go gets tricky. The best open-source diarization is `pyannote-audio`, which is Python/PyTorch. Options:

1. **Ship a small Python sidecar** that handles diarization only. The daemon sends audio segments over a local socket, gets back speaker labels. Optional — only starts if diarization is enabled.
2. **Use whisper.cpp's built-in VAD + simple clustering** for basic "Speaker A / Speaker B" detection. Less accurate but zero dependencies.
3. **Punt to remote APIs** — Deepgram and AssemblyAI both do diarization server-side.

Recommendation: Start with option 2 (good enough for most meetings), offer option 1 as an optional install (`ghostwriter install diarization`), and support option 3 for remote backends.

### Meeting Detection

```go
type Detector struct {
    processMonitor *ProcessMonitor
    pollInterval   time.Duration
}

type SignalType int

const (
    SignalStarted SignalType = iota
    SignalEnded
)

type Signal struct {
    Type SignalType
    App  string
}
```

**Process Monitor:**
- macOS: Polls running processes via `ps -eo comm` to detect known meeting apps
- Windows: WASAPI session enumeration — detect when apps like Zoom/Teams acquire audio devices
- Linux: PulseAudio/PipeWire client monitoring

Known meeting apps to watch (configurable):
```toml
[detection.processes]
apps = [
    "zoom.us", "Zoom",
    "Microsoft Teams", "Teams",
    "Slack",
    "Discord",
    "Google Chrome",  # needs additional heuristic for Meet
    "Firefox",
    "Arc",
    "Webex",
]
```

For browser-based meetings (Google Meet, etc.), process detection alone isn't enough. Pair with:
- **Active audio device usage** — browser process + active mic/speaker = likely meeting
- **Calendar correlation** — if there's a calendar event with a Meet/Zoom link right now and Chrome is using the mic, that's a meeting

**Calendar Integration:**
- Google Calendar: OAuth2 + polling every 60s (or push via webhooks if the user sets it up)
- Outlook/O365: Microsoft Graph API + polling
- CalDAV: For self-hosted calendar users
- ICS file watch: For the truly minimal — point at an exported `.ics` and poll it

Calendar gives you:
- Auto-arm before meetings start
- Meeting title + attendees as transcript metadata
- Auto-stop when the event ends (with a grace period for meetings that run over)

### Audio Capture

```go
type AudioData struct {
    Samples    []float32
    SampleRate int
    Channels   int
}

type Capture struct {
    mu        sync.Mutex
    recording bool
    cmd       *exec.Cmd
    wavPath   string
}
```

- Uses FFmpeg as a subprocess to capture system audio via the BlackHole virtual audio device
- Captures at 16kHz mono WAV (optimal for Whisper input)
- Writes to a temp file during recording, returns the path on stop
- Save raw audio alongside transcripts (configurable — some users want recordings, some don't)

Platform specifics:
- **macOS:** FFmpeg with avfoundation input, capturing from BlackHole virtual audio device. Requires BlackHole installed and configured as part of an aggregate audio device.
- **Windows:** WASAPI loopback capture via FFmpeg. Easiest platform for this.
- **Linux:** PipeWire/PulseAudio monitor via FFmpeg. Straightforward.

### Configuration

Single TOML file at `~/.config/ghostwriter/config.toml` (aspirational — current implementation has defaults hardcoded):

```toml
[general]
output_dir = "~/Documents/Ghostwriter"
auto_start = true
save_audio = false  # Keep raw .wav files alongside transcripts

[transcription]
backend = "local"           # "local" | "openai" | "deepgram" | "gladia" | "custom"
model = "base.en"           # For local: tiny, base, small, medium, large-v3
language = "en"             # ISO 639-1 code, or "auto"
diarization = true

[transcription.remote]      # Only used when backend != "local"
api_key = ""
endpoint = ""               # For custom backend

[detection]
mode = "auto"               # "auto" | "manual" | "always"
calendar = "google"         # "google" | "outlook" | "caldav" | "ics" | "none"
grace_period_minutes = 5    # Keep recording after calendar event ends

[detection.processes]
apps = ["zoom.us", "Microsoft Teams", "Slack", "Discord"]
browser_audio_threshold_seconds = 10  # Sustained browser audio = likely meeting

[output]
formats = ["json"]          # "json" | "txt" | "srt" | "md"
include_confidence = true
include_word_timestamps = true

[mcp]
enabled = true
port = 3847
```

---

## Output Format

The core output. This is the product.

### `.transcript.json`

```json
{
    "version": "1.0",
    "id": "20260303-143022-a1b2c3",
    "metadata": {
        "date": "2026-03-03T14:30:22Z",
        "duration_seconds": 1847,
        "title": "Sprint Planning",
        "source": "calendar",
        "calendar_event_id": "abc123@google.com",
        "attendees": ["ian@company.com", "sarah@company.com"],
        "detected_app": "zoom.us",
        "model": "whisper-large-v3",
        "language": "en"
    },
    "speakers": [
        { "id": "speaker_0", "label": "Speaker A" },
        { "id": "speaker_1", "label": "Speaker B" }
    ],
    "segments": [
        {
            "start": 0.0,
            "end": 4.52,
            "speaker": "speaker_0",
            "text": "Alright, let's get started with sprint planning.",
            "confidence": 0.94,
            "words": [
                { "word": "Alright", "start": 0.0, "end": 0.48, "confidence": 0.97 },
                { "word": "let's", "start": 0.52, "end": 0.81, "confidence": 0.95 },
                { "word": "get", "start": 0.85, "end": 1.02, "confidence": 0.96 },
                { "word": "started", "start": 1.05, "end": 1.48, "confidence": 0.93 },
                { "word": "with", "start": 1.52, "end": 1.71, "confidence": 0.91 },
                { "word": "sprint", "start": 1.75, "end": 2.18, "confidence": 0.89 },
                { "word": "planning", "start": 2.22, "end": 2.81, "confidence": 0.92 }
            ]
        },
        {
            "start": 5.10,
            "end": 12.33,
            "speaker": "speaker_1",
            "text": "Sure. So I've been looking at the backlog and I think we should prioritize the API migration.",
            "confidence": 0.91,
            "words": []
        }
    ],
    "full_text": "Alright, let's get started with sprint planning.\n\nSure. So I've been looking at the backlog and I think we should prioritize the API migration."
}
```

Key design decisions:
- **`full_text` field** — for people who just want to grep/search. No need to reassemble from segments.
- **Word-level timestamps** — enables precise seeking if someone builds a player on top.
- **Speaker labels are generic** — "Speaker A", not names. Naming speakers is a hard problem (requires enrollment or calendar correlation). Leave it for post-processing or plugins.
- **`source` field** — how the meeting was detected. Useful for debugging and for downstream tools.
- **Stable `id` format** — date + time + short hash. Human-readable and sortable.

### File System Layout

```
~/Documents/Ghostwriter/
├── 2026/
│   ├── 03/
│   │   ├── 20260303-143022-a1b2c3.transcript.json
│   │   ├── 20260303-143022-a1b2c3.wav          # Optional raw audio
│   │   ├── 20260303-143022-a1b2c3.srt          # Optional subtitle format
│   │   ├── 20260303-160000-d4e5f6.transcript.json
│   │   └── ...
│   └── 04/
│       └── ...
└── index.json   # Optional: fast lookup without scanning dirs
```

---

## CLI Interface

```bash
# Daemon control
ghostwriter start                    # Start daemon (background)
ghostwriter stop                     # Stop daemon
ghostwriter status                   # Show current state (idle, recording, processing)

# Manual control
ghostwriter record start             # Force-start a recording
ghostwriter record stop              # Force-stop current recording
ghostwriter record start --title "1:1 with Sarah"

# Transcription (standalone, no daemon needed)
ghostwriter transcribe meeting.wav                    # Transcribe a file
ghostwriter transcribe meeting.wav --output out.json  # Custom output path
ghostwriter transcribe meeting.wav --model large-v3   # Override model
ghostwriter transcribe meeting.wav --diarize          # With speaker diarization

# Model management
ghostwriter models list              # Show available models
ghostwriter models download large-v3 # Download a model
ghostwriter models remove base.en    # Remove a model

# Transcript access
ghostwriter list                     # List recent transcripts
ghostwriter list --since 2026-03-01  # Filter by date
ghostwriter show <id>                # Print transcript text
ghostwriter search "API migration"   # Full-text search across all transcripts

# Configuration
ghostwriter config show              # Print current config
ghostwriter config set transcription.model large-v3
ghostwriter config set detection.calendar google
ghostwriter config auth google       # OAuth flow for Google Calendar

# MCP server (usually auto-started by daemon)
ghostwriter mcp start                # Start MCP server standalone
ghostwriter mcp status
```

---

## MCP Server

Built into the daemon. Exposes meeting transcripts as tools for AI agents.

### Tools

```
list_meetings(
    since?: date,
    until?: date,
    query?: string,
    limit?: int = 20
) -> Meeting[]

get_transcript(
    meeting_id: string
) -> Transcript

search_transcripts(
    query: string,
    since?: date,
    speaker?: string,
    limit?: int = 10
) -> SearchResult[]

get_recording_status() -> {
    state: "idle" | "recording" | "processing",
    current_meeting?: { title, start_time, duration_so_far }
}

start_recording(title?: string) -> { meeting_id }
stop_recording() -> { meeting_id, transcript_path }
```

This means any MCP-compatible client (Claude, Cursor, custom agents) can:
- "What did we decide about the API migration in yesterday's standup?"
- "Summarize all my meetings from last week"
- "Find every time Sarah mentioned the Q2 deadline"
- "Start recording this call"

---

## Build & Distribution

### macOS
- Homebrew formula: `brew install ghostwriter`
- Ships as universal binary (arm64 + x86_64)
- FFmpeg + BlackHole virtual audio device for audio capture
- Transcription via `whisper-cli` with Metal acceleration
- Tray icon via native AppKit
- Needs permissions: Accessibility (for process monitoring), Microphone (for audio capture via BlackHole)

### Windows
- `winget install ghostwriter` or MSI installer
- WASAPI loopback for audio capture via FFmpeg
- Transcription via `whisper-cli` with DirectML or CUDA acceleration
- Tray icon via Win32 system tray API
- No special permissions needed for audio loopback

### Linux
- Flatpak or distro packages
- PipeWire/PulseAudio monitor for audio capture via FFmpeg
- Transcription via `whisper-cli` with CUDA or CPU
- Tray icon via `libappindicator` or XDG tray protocol

### CI/CD
- GitHub Actions for cross-platform builds
- Nightly builds from `main`
- Tagged releases with checksums
- Homebrew tap auto-updated on release

---

## Development Phases

### Phase 1 — Core Loop (Weeks 1-4)
- [ ] Daemon skeleton in Go (start/stop, tray icon, config loading)
- [ ] Audio capture on macOS (FFmpeg + BlackHole) — start with one platform
- [ ] Whisper.cpp integration via `whisper-cli`
- [ ] Basic output pipeline (`.transcript.json`)
- [ ] CLI: `ghostwriter start/stop/record/transcribe`
- [ ] Manual trigger only (no auto-detection yet)

**Milestone: You can start/stop recording from the CLI or tray and get a transcript file.**

### Phase 2 — Detection (Weeks 5-8)
- [ ] Process monitor (macOS first)
- [ ] Google Calendar integration (OAuth + polling)
- [ ] Auto-start/stop recording based on signals
- [ ] Calendar metadata in transcripts
- [ ] Grace period / silence detection for meeting end

**Milestone: Daemon auto-records your Google Meet/Zoom calls and produces transcripts with meeting titles.**

### Phase 3 — Quality & MCP (Weeks 9-12)
- [ ] Speaker diarization (whisper.cpp VAD + clustering)
- [ ] MCP server with core tools
- [ ] Full-text search across transcripts
- [ ] Remote transcription backends (OpenAI, Deepgram)
- [ ] Model management CLI

**Milestone: You can ask Claude "what was discussed in my last meeting?" and get an answer.**

### Phase 4 — Cross-Platform (Weeks 13-16)
- [ ] Windows audio capture (WASAPI)
- [ ] Windows process monitoring
- [ ] Linux audio capture (PipeWire)
- [ ] Homebrew, winget, Flatpak packaging
- [ ] CI/CD for all platforms

**Milestone: Works on all three major platforms with native packaging.**

### Phase 5 — Polish & Community (Ongoing)
- [ ] Outlook calendar support
- [ ] Optional pyannote diarization sidecar
- [ ] Webhook/callback support
- [ ] Plugin system for custom post-processing
- [ ] Reference web UI for transcript viewing
- [ ] Documentation site

---

## What This Intentionally Doesn't Do

- **No built-in summarization.** Use the MCP server + your preferred LLM.
- **No meeting bot that joins calls.** This captures audio locally. Completely different architecture and problem space.
- **No real-time streaming UI.** You get the transcript after the meeting. Real-time adds enormous complexity for marginal value.
- **No team/collaboration features.** This is a single-user local tool. Teams can build sharing on top via the file format.
- **No cloud sync.** Your files, your disk. Use Syncthing/Dropbox/git if you want sync.

---

## Competitive Landscape

| Tool | Approach | Limitation |
|------|----------|------------|
| Otter.ai | Cloud SaaS | No self-hosting, data leaves your machine |
| Fireflies | Cloud SaaS + bot | Bot joins calls, paid plans |
| Meetily | Open source desktop app | Monolithic, early stage, tries to do too much |
| Scriberr | Self-hosted web app | Needs Docker, no auto-detection, manual upload |
| Vexa | Open source meeting API | Focused on bot-joins-meeting model, complex infra |
| **Ghostwriter** | Local daemon + files | Just capture + transcribe. Does one thing well. |

---

## Open Questions

1. **Name.** "Ghostwriter" is placeholder. Needs something that isn't already taken.
2. **macOS audio capture permissions.** BlackHole requires setting up an aggregate audio device, which is a slightly confusing UX. Need to research if there's a cleaner path.
3. **Browser meeting detection.** Knowing that Chrome is using the mic isn't quite enough to know it's Google Meet vs. a random website. Do we care? Probably not — if you're in a browser call, transcribe it.
4. **Licensing.** MIT vs Apache 2.0. Leaning Apache 2.0 for patent protection.
5. **Whisper model bundling.** The base model is ~150MB. Do we ship it in the binary or download on first run? First-run download is better for binary size but worse for "just works" experience.
