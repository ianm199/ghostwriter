# Ghostwriter — Project Status

Last updated: 2026-03-05

## What It Is

A modular desktop audio toolkit packaged as importable Go libraries. The core primitives — system audio capture, process/microphone awareness, and local transcription — live in `pkg/` and can be imported by any Go project. A meeting transcription daemon (`cmd/ghostwriter`) ships as the reference use case, built entirely on top of these libraries.

## Architecture

```
pkg/ (public, importable libraries)
  ├── audiocapture/           System audio recording (FFmpeg + BlackHole)
  ├── sysaware/               Process monitoring + mic state detection
  └── transcribe/             Whisper integration, transcript model, storage

cmd/ghostwriter/              CLI binary (Cobra)
  │
  ├── ghostwriter start/stop/status     → Daemon control
  ├── ghostwriter record start/stop     → Manual recording
  ├── ghostwriter transcribe <file>     → Standalone transcription
  ├── ghostwriter list/show/search      → Transcript queries
  └── ghostwriter models list/download  → Whisper model management

internal/ (meeting-specific policy)
  ├── daemon/                 Event loop, IPC, meeting detection state machine
  ├── cli/                    Command implementations, product paths
  └── tray/                   System tray (stub)

Storage: ~/Documents/Ghostwriter/{YYYY}/{MM}/{ID}.transcript.json
```

## Public Packages (pkg/)

### audiocapture — System Audio Recording

- `AudioRecorder` interface: `Start`, `Stop`, `IsRecording`
- `SCKitRecorder` (default): native ScreenCaptureKit via cgo — zero external dependencies, can target specific apps by name
- `Recorder` (fallback): FFmpeg → BlackHole → 16kHz mono WAV
- Auto-detects backend: SCKit if available (macOS 12.3+), falls back to BlackHole
- Includes 48kHz stereo → 16kHz mono resampling for Whisper-optimal output
- macOS only (`//go:build darwin`)

### sysaware — System Awareness

- `ProcessChecker` interface: `IsRunning(name)`, `RunningProcesses()`
- `MicDetector` interface: `IsActive()`
- `DarwinProcessChecker`: queries `ps -eo comm`
- `DarwinMicDetector`: queries CoreAudio `kAudioDevicePropertyDeviceIsRunningSomewhere` via `go-media-devices-state` (no TCC permission needed)
- macOS only (`//go:build darwin`)

### transcribe — Transcription & Storage

- `Transcriber` interface: `Transcribe(AudioData)`, `TranscribeFile(path)`, `Close()`
- `Backend` type + `TranscriberConfig` + `NewTranscriber()` factory for pluggable backends
- Three backends:
  - `WhisperTranscriber` (`local`): shells out to `whisper-cli`, parses JSON output. Anti-hallucination flags (beam-size 5, max-context 0, temperature 0, VAD via Silero)
  - `AssemblyAITranscriber` (`assemblyai`): pure `net/http` REST — upload → submit (with speaker labels) → poll. 10-min timeout. Speaker diarization included
  - `OpenAITranscriber` (`openai`): multipart POST to `whisper-1` with `verbose_json` + word timestamps. 25MB file size pre-check. No diarization (API limitation)
- API keys resolved from environment variables (`ASSEMBLYAI_API_KEY`, `OPENAI_API_KEY`) at startup
- `Transcript`, `Metadata`, `Segment`, `Word`, `Speaker` data model
- `Store`: filesystem persistence with date-organized directories
- Full-text search across all transcripts

## Meeting Detection (internal/daemon/)

Three-tier detection model using `sysaware` primitives:

- **Tier 1:** Meeting-specific processes → immediate trigger (`CptHost` for Zoom, `(WebexAppLauncher)` for Webex)
- **Tier 2:** Browsers + mic active for 10s → trigger (`Google Chrome`, `Arc`, `Firefox`, `Safari`, `Brave`, `Edge`)
- **Tier 3:** Desktop apps + mic active for 10s → trigger (`Slack`, `Discord`, `Microsoft Teams`, `FaceTime`)

10-second debounce (2 consecutive polls) for start and end signals. Filters permission prompts and brief mute toggles.

Tested live: Google Meet in Chrome detected after mic active for 10s. Slack idle correctly ignored. See `internal/daemon/DETECTION.md` for full research.

## CLI Commands (internal/cli/)

| Command | Status |
|---|---|
| `ghostwriter start` | Working — starts daemon (supports `--transcription-backend`) |
| `ghostwriter stop` | Working — graceful shutdown via IPC |
| `ghostwriter status` | Working — shows state + meeting duration |
| `ghostwriter record start/stop` | Working — manual recording control |
| `ghostwriter transcribe <file>` | Working — standalone transcription (supports `--transcription-backend`) |
| `ghostwriter list [--since]` | Working — lists transcripts |
| `ghostwriter show <id>` | Working — prints transcript text |
| `ghostwriter search <query>` | Working — full-text search |
| `ghostwriter models list` | Working — shows available/downloaded |
| `ghostwriter models download <name>` | Working — downloads from HuggingFace |
| `ghostwriter models remove <name>` | Working — deletes local model |

## Known Issues

### Detection Attribution Bug
When multiple mic-required apps are running (e.g., Chrome + Slack), the first match wins. If Chrome is in a Google Meet call, it correctly attributes to Chrome (browsers checked before desktop apps). But if you're in a Slack huddle with Chrome also running, it'll attribute to Chrome instead of Slack.

**Root cause:** `MicDetector.IsActive()` is a global boolean — it doesn't tell us WHICH app is using the mic.

**Fix path:** CoreAudio log stream can attribute mic usage to a specific PID:
```bash
log stream --predicate 'sender == "CoreAudio" && eventMessage contains "running: "'
```
This is documented in `internal/daemon/DETECTION.md` but not implemented.

### BlackHole Fallback
The BlackHole audio backend requires the virtual audio device installed and configured as an aggregate device. SCKit (now the default) has no such dependency — it only needs Screen Recording permission.

### No Tests
Zero test files in the codebase.

## What's Not Built

### Floating Widget (internal/tray/)
Native AppKit floating panel (Loom-style pill). Shows status dot, label, and start/stop button. Polls daemon via Unix socket. Run with `ghostwriter tray`.

### Speaker Diarization
`Speaker` struct exists in the transcript model but is never populated.

### MCP Server
Designed in SPEC.md but no code. Would expose transcripts as MCP resources and tools for any MCP-compatible client.

### Calendar Integration
Designed in SPEC.md. Would enrich transcript metadata with meeting titles, attendee lists, and scheduled times.

### Configuration File
All settings hardcoded. SPEC.md describes `~/.config/ghostwriter/config.toml`.

### Remote Transcription Backends
AssemblyAI and OpenAI backends implemented. Deepgram, Gladia, and custom HTTP endpoint backends are not.

### Cross-Platform
macOS only. No Windows (WASAPI) or Linux (PipeWire) implementations of `ProcessChecker`, `MicDetector`, or `AudioRecorder`.

### Packaging
No Homebrew formula, CI/CD, or installers.

## File Inventory

| File | Purpose |
|---|---|
| `cmd/ghostwriter/main.go` | Entry point |
| `pkg/audiocapture/capture.go` | AudioRecorder interface + FFmpeg Recorder |
| `pkg/sysaware/process.go` | ProcessChecker interface |
| `pkg/sysaware/process_darwin.go` | macOS process monitoring |
| `pkg/sysaware/mic.go` | MicDetector interface |
| `pkg/sysaware/mic_darwin.go` | macOS CoreAudio mic state |
| `pkg/transcribe/transcript.go` | Transcript data model |
| `pkg/transcribe/store.go` | Filesystem storage + search |
| `pkg/transcribe/transcriber.go` | Transcriber interface, Backend type, factory |
| `pkg/transcribe/whisper.go` | Whisper.cpp integration (local backend) |
| `pkg/transcribe/assemblyai.go` | AssemblyAI cloud backend |
| `pkg/transcribe/openai.go` | OpenAI Whisper API backend |
| `internal/cli/root.go` | CLI framework |
| `internal/cli/daemon.go` | start/stop/status commands |
| `internal/cli/record.go` | Manual recording commands |
| `internal/cli/transcribe.go` | Standalone transcription |
| `internal/cli/query.go` | list/show/search commands |
| `internal/cli/models.go` | Whisper model management |
| `internal/cli/paths.go` | Product-specific path defaults |
| `internal/daemon/daemon.go` | Core daemon event loop |
| `internal/daemon/detect.go` | Meeting detection state machine |
| `internal/daemon/socket.go` | Unix socket IPC |
| `internal/daemon/DETECTION.md` | Detection research & design |
| `internal/tray/tray.go` | System tray (stub) |
| `SPEC.md` | Full architecture design doc |

## Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/antonfisher/go-media-devices-state` | CoreAudio mic state (cgo) |
| `ffmpeg` (runtime) | Audio capture |
| `whisper-cli` (runtime) | Transcription |
| BlackHole (runtime) | Virtual audio loopback |

## Phase Status

| Phase | Description | Status |
|---|---|---|
| **1. Core Loop** | Daemon, capture, transcription, output, CLI | Done |
| **2. Detection** | Process monitoring, mic confirmation, auto-record | Done |
| **2.5. Modular Toolkit** | Extract pkg/audiocapture, sysaware, transcribe | Done |
| **3. Quality & MCP** | Diarization, MCP server, remote backends, search | Partial (search done, AssemblyAI + OpenAI backends done) |
| **4. Cross-Platform** | Windows, Linux, CI/CD, packaging | Not started |
| **5. Polish** | Calendar, webhooks, plugins, tray UI, docs site | Not started |
