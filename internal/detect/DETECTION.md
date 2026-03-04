# Meeting Detection Strategy

## Overview

Ghostwriter detects active meetings by combining two signals: **process monitoring** and **microphone state**. Not all meeting apps provide the same quality of signal, so each app is classified into a detection tier based on how reliably its process presence indicates an active call.

## Detection Tiers

### Tier 1: Meeting-Specific Processes (immediate trigger)

These processes only exist during an active meeting. Their presence alone is a strong signal.

| Process Name | App | Notes |
|---|---|---|
| `CptHost` | Zoom | Content Provider Technology Host. Lives inside `zoom.us.app/Contents/Frameworks/CptHost.app`. Only spawns when a meeting is joined. During meetings it runs with a `-key <meeting_id>` argument. When Zoom is open but idle, `capHost` may run with `-key rpc` instead. Source: brunerd.com, Home Assistant community. |
| `(WebexAppLauncher)` | Webex | The Meeting Center process rewrites its name with parentheses during an active meeting. Check via `ps auxww`. `WebexHelper` also appears in `pmset -g` sleep prevention assertions during meetings. |

### Tier 2: Always-Running Apps (require mic confirmation)

These apps run continuously as background processes. Their presence in `ps` says nothing about whether a call is active. We require microphone activity (2 consecutive polls, 10 seconds) to confirm an active meeting.

| App | Process Name | Why Mic is Needed |
|---|---|---|
| Slack | `Slack` | Electron-based. Huddles use WebRTC inside the existing renderer process — no new process spawns. There is no process-level distinction between idle and huddle. |
| Discord | `Discord` | Electron-based, identical architecture to Slack. Voice channels happen within the existing renderer. |
| Microsoft Teams | `Microsoft Teams` | New Teams (post-2023 rewrite) no longer writes call state to log files. No meeting-specific child process. |
| FaceTime | `FaceTime` | Process may be running without an active call (e.g., app left open). |

### Tier 3: Browsers (require mic confirmation)

Browsers are always running. The mic check disambiguates "Chrome playing YouTube" (no mic) from "Chrome in a Google Meet call" (mic active).

| Process Name | Browser |
|---|---|
| `Google Chrome` | Chrome |
| `Arc` | Arc |
| `Firefox` | Firefox |
| `Safari` | Safari |
| `Brave Browser` | Brave |
| `Microsoft Edge` | Edge |

## Microphone State Detection

We use `github.com/antonfisher/go-media-devices-state` which queries CoreAudio's `kAudioDevicePropertyDeviceIsRunningSomewhere` property. This is the same API that drives the macOS orange dot indicator.

**Permissions:** No microphone TCC permission required. This is a read-only device metadata query (is the device in use), not an audio stream capture. Requires Xcode CLI tools for the cgo build.

**Known limitation:** Bluetooth microphones may always report as inactive due to a CoreAudio quirk documented in Apple Developer Forums.

### What the mic query returns (example output)

```
# | is used | description
2 | NO      | BlackHole16ch_UID (name: 'BlackHole 16ch')
3 | YES     | BlackHole2ch_UID (name: 'BlackHole 2ch')        ← FFmpeg recording
4 | YES     | BuiltInMicrophoneDevice (name: 'MacBook Pro Mic') ← Google Meet
7 | NO      | MSLoopbackDriverDevice_UID (name: 'Microsoft Teams Audio')
```

When Google Meet activates the mic, the built-in microphone shows `YES`. BlackHole 2ch shows `YES` when our own FFmpeg capture is running. The library correctly enumerates all audio input devices including virtual devices, aggregate devices, and iPhone Continuity mic.

## Debounce Logic

For Tier 2 and Tier 3 apps, we debounce both start and end signals:

- **Start:** mic must be active for 2 consecutive polls (10+ seconds) before triggering `SignalStarted`. This filters out permission prompts, brief unmute/mute, voice search, and other transient mic access.
- **End:** mic must be inactive for 2 consecutive polls (10+ seconds) before triggering `SignalEnded`. This avoids false stops from brief mute toggles.

Tier 1 apps trigger immediately since their process presence is already an unambiguous signal.

## Alternative Detection Approaches (researched, not implemented)

These approaches were investigated but not implemented due to complexity or reliability concerns.

### CoreAudio Log Stream (per-process mic attribution)

```bash
log stream --predicate 'sender == "CoreAudio" && eventMessage contains "running: "'
```

Outputs lines like:
```
SystemStatusWrapper::PublishRecordingClientInfo: Report client 39644 running: yes
```

The PID identifies which specific app is using the mic. This would let us attribute mic usage to Slack vs Chrome vs some other app. More precise than boolean `IsMicrophoneOn()` but requires parsing a live log stream and PID lookups.

Source: nickcampbell18/microphone-slack-status on GitHub.

### Power Management Assertions

```bash
pmset -g assertions
```

During active calls, apps assert sleep prevention. App names appear in the output (Zoom as `zoom.us`, Teams as `MSTeams`, Webex as `WebexHelper`). Used by BetterTouchTool community scripts. Limitation: not all apps assert this, and the output format is fragile.

### Teams Log Parsing (legacy only)

Old Electron-based Teams wrote call state to `~/Library/Application Support/Microsoft/Teams/logs.txt`:
- `eventData: s::;m::1;a::1` = call started
- `eventData: s::;m::1;a::3` = call ended

No longer works with new Teams (2024+). Source: mre/teams-call on GitHub.

### Network-Based Detection (UDP connections)

WebRTC-based apps (Slack, Discord, Teams, browsers) establish UDP connections to media servers during calls. Detecting non-loopback UDP connections from a specific process via `lsof -i UDP -c Slack` could indicate an active call. Not implemented due to performance cost of `lsof` every 5 seconds.

## Related Projects

- **OverSight** (objective-see.org) — open-source macOS tool that monitors mic/camera activation and reports which process triggered it. Uses CoreAudio and IOKit APIs via Objective-C.
- **beandev/call-detector** (codeberg.org) — detects active calls via UDP connection monitoring. Notes that Zoom briefly establishes UDP connections on launch (2-3 second false positive window).
- **brunerd.com** — blog post documenting `CptHost` detection for Zoom meeting state.
- **MacWhisper, trnscrb** — commercial transcription tools using the same mic+process dual-signal pattern.

## Tested On

- macOS Sequoia 15.3 (Darwin 24.3.0), Apple Silicon
- Google Chrome with Google Meet (confirmed: mic shows as active, 10-second debounce works)
- Slack idle (confirmed: process always present, mic correctly shows inactive when not in huddle)
- BlackHole 2ch virtual audio device (confirmed: correctly detected as active during FFmpeg capture)
