# Ghostwriter

Local-first meeting transcription daemon for macOS. Detects meetings, captures system audio, transcribes via Whisper, and outputs structured transcript files.

## Prerequisites

```bash
# Install whisper.cpp (provides whisper-cli)
brew install whisper-cpp

# Install ffmpeg (for audio capture)
brew install ffmpeg

# Install BlackHole (virtual audio device for system audio capture)
brew install blackhole-2ch
```

After installing BlackHole, create an aggregate audio device in Audio MIDI Setup that combines your output device with BlackHole. This lets Ghostwriter capture system audio.

## Build

```bash
go build -o ghostwriter ./cmd/ghostwriter
```

## Quick Start

### 1. Download a Whisper model

```bash
# See available models
./ghostwriter models list

# Download the default model (base.en — good balance of speed and accuracy)
./ghostwriter models download base.en

# For faster transcription on longer meetings, try tiny.en
./ghostwriter models download tiny.en
```

### 2. Test with a standalone transcription

The easiest way to verify everything works — no daemon needed:

```bash
# Transcribe any audio file
./ghostwriter transcribe recording.wav

# Specify output path
./ghostwriter transcribe recording.wav -o my-transcript.json
```

This writes a `.transcript.json` file with timestamped segments, word-level timing, and confidence scores.

### 3. Run the daemon

```bash
# Start the daemon (foreground — you'll see logs)
./ghostwriter start

# In another terminal, check status
./ghostwriter status

# Manually start a recording
./ghostwriter record start --title "Sprint Planning"

# Stop recording (triggers transcription)
./ghostwriter record stop

# Stop the daemon
./ghostwriter stop
```

The daemon also auto-detects meetings — when it sees Zoom, Teams, Slack, Discord, or Webex running, it starts recording automatically and stops when the app exits.

## Commands

| Command | Description |
|---|---|
| `ghostwriter start` | Start the daemon |
| `ghostwriter stop` | Stop the daemon |
| `ghostwriter status` | Show daemon state (idle/recording/processing) |
| `ghostwriter record start --title "..."` | Manually start recording |
| `ghostwriter record stop` | Stop recording |
| `ghostwriter transcribe <file>` | Transcribe an audio file (no daemon needed) |
| `ghostwriter list` | List all transcripts |
| `ghostwriter list --since 2025-01-01` | List transcripts after a date |
| `ghostwriter show <id>` | Print a transcript's full text |
| `ghostwriter search <query>` | Search across all transcripts |
| `ghostwriter models list` | Show available Whisper models |
| `ghostwriter models download <model>` | Download a model |
| `ghostwriter models remove <model>` | Delete a downloaded model |

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./internal/daemon/
go test -v ./internal/output/
```

## Transcripts

Transcripts are saved to `~/Documents/Ghostwriter/` in a year/month structure:

```
~/Documents/Ghostwriter/
  2025/
    06/
      abc123.transcript.json
```

Each transcript JSON contains:
- Metadata (date, duration, title, model used)
- Timestamped segments with confidence scores
- Word-level timing
- Full text

## Architecture

- **Daemon** — event loop that coordinates detection, capture, and transcription
- **Detector** — polls for running meeting apps (Zoom, Teams, Slack, Discord, Webex)
- **Capture** — records system audio via ffmpeg + BlackHole
- **Transcriber** — runs whisper-cli on captured audio
- **Store** — persists transcripts as JSON files
- **Socket** — Unix domain socket IPC between CLI and daemon
