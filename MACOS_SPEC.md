# Ghostwriter macOS Spec — APIs, Detection, and Integration Tests

This document specs out every system integration needed for a great macOS experience, identifies the exact APIs and events, and defines integration tests that validate the full pipeline.

---

## Table of Contents

1. [Design Smells & Required Refactors](#1-design-smells--required-refactors)
2. [Audio Capture on macOS](#2-audio-capture-on-macos)
3. [Meeting Detection on macOS](#3-meeting-detection-on-macos)
4. [Calendar Integration](#4-calendar-integration)
5. [Browser Meeting Detection](#5-browser-meeting-detection)
6. [Integration Test Architecture](#6-integration-test-architecture)
7. [Integration Test Specs](#7-integration-test-specs)

---

## 1. Design Smells & Required Refactors

The current code has several testability problems that need fixing before we can write meaningful integration tests.

### 1.1 Daemon hardcodes all dependencies (CRITICAL)

**Problem:** `daemon.New()` constructs everything internally — concrete `*detect.Detector`, `*capture.Capture`, `*transcribe.WhisperTranscriber`. There's no way to inject test doubles.

```go
// Current: untestable
func New() (*Daemon, error) {
    w, err := transcribe.NewWhisperTranscriber(transcribe.WhisperConfig{})  // hardcoded
    // ...
    return &Daemon{
        detector: detect.New(),     // hardcoded
        capture:  capture.New(),    // hardcoded
        whisper:  w,
        store:    output.NewStore(outputDir),
    }, nil
}
```

**Fix:** Accept interfaces via a config/options struct:

```go
type DaemonConfig struct {
    Detector   MeetingDetector     // interface
    Capture    AudioCapture        // interface
    Transcriber transcribe.Transcriber  // already an interface!
    Store      TranscriptStore     // interface
    SocketPath string              // injectable for tests
    OutputDir  string
}

func New(cfg DaemonConfig) (*Daemon, error) { ... }
```

### 1.2 No interfaces for Capture or Detection

**Problem:** `capture.Capture` and `detect.Detector` are concrete structs. The daemon references them directly. You can't swap in a fake capturer that produces a known WAV file, or a fake detector that emits signals on demand.

**Fix:** Define interfaces:

```go
// internal/capture/capture.go
type AudioCapturer interface {
    Start() error
    Stop() (wavPath string, err error)
    IsRecording() bool
}

// internal/detect/detect.go
type MeetingDetector interface {
    Start(ctx context.Context) <-chan Signal
}
```

The concrete implementations satisfy these. Tests use fakes.

### 1.3 Transcriber interface is fine — but WhisperTranscriber can't be created without whisper-cli

**Problem:** `NewWhisperTranscriber` fails immediately if `whisper-cli` isn't in PATH or the model file doesn't exist. This makes it impossible to even construct a daemon in test environments.

**Fix:** This is already an interface (`transcribe.Transcriber`) — the daemon just needs to accept it via injection (see 1.1). Tests pass in a `FakeTranscriber` that returns canned transcripts.

### 1.4 process_darwin.go missing build constraint

**Problem:** `process_darwin.go` has no `//go:build darwin` tag, so the `detect` package won't compile on Linux. The `Detector` struct unconditionally references `*ProcessMonitor` which is only defined in `process_darwin.go`.

**Fix:** Add build constraint + create `process_stub.go` for non-darwin platforms (or use an interface).

### 1.5 Socket path is hardcoded

**Problem:** `socketPath()` returns a fixed path in `/tmp`. Two daemon instances (or a test running alongside a real daemon) collide.

**Fix:** Make socket path configurable via `DaemonConfig`.

### 1.6 `os.Exit(0)` in command handler

**Problem:** `handleCommand` calls `os.Exit(0)` for `CmdStop`. This kills the test process.

**Fix:** Use context cancellation instead — `CmdStop` cancels the root context, which causes `Run()` to return.

### Summary of Required Refactors

| Refactor | Why | Difficulty |
|----------|-----|------------|
| Interface for AudioCapturer | Mock audio in tests | Small |
| Interface for MeetingDetector | Inject fake signals in tests | Small |
| DaemonConfig with dependency injection | Enable full integration tests | Medium |
| Build constraints on process_darwin.go | Compile on non-macOS | Small |
| Configurable socket path | Test isolation | Small |
| Replace os.Exit with context cancel | Tests don't die | Small |

---

## 2. Audio Capture on macOS

### 2.1 Current Approach: ffmpeg + BlackHole

The current implementation shells out to ffmpeg with AVFoundation:

```
ffmpeg -f avfoundation -i :$DEVICE_INDEX -ar 16000 -ac 1 -y output.wav
```

This requires:
- ffmpeg installed (`brew install ffmpeg`)
- BlackHole virtual audio device installed (`brew install blackhole-2ch`)
- User manually creates an aggregate audio device pairing BlackHole with their speakers

**Verdict:** Works but poor UX. Requiring users to create aggregate audio devices is a dealbreaker for mainstream adoption.

### 2.2 Recommended: ScreenCaptureKit (macOS 13+)

**ScreenCaptureKit** (`SCStream`) is Apple's modern API for capturing screen and audio content. As of macOS 13 (Ventura), it supports **audio-only capture** and **per-application audio capture**.

#### Key APIs

| API | Purpose |
|-----|---------|
| `SCShareableContent.getExcludingDesktopWindows(_:onScreenWindowsOnly:)` | Enumerate capturable apps/windows |
| `SCContentFilter(desktopIndependentWindow:)` or `SCContentFilter(display:excludingApplications:exceptingWindows:)` | Filter to capture specific app audio |
| `SCStreamConfiguration` | Configure audio format (sample rate, channels, etc.) |
| `SCStream` | The capture stream itself |
| `SCStreamDelegate` / `SCStreamOutput` | Receive audio buffers (`CMSampleBuffer`) |

#### Configuration for Audio-Only

```swift
let config = SCStreamConfiguration()
config.capturesAudio = true
config.excludesCurrentProcessAudio = false
config.sampleRate = 16000         // Whisper wants 16kHz
config.channelCount = 1           // Mono is fine for speech
config.width = 1                  // Minimum — we don't want video
config.height = 1                 // Minimum — we don't want video
```

#### Per-App Audio Capture

ScreenCaptureKit can capture audio from **specific applications**:

```swift
// Get all shareable content
let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false)

// Find Zoom
let zoomApp = content.applications.first { $0.bundleIdentifier == "us.zoom.xos" }

// Create filter for just Zoom's audio
let filter = SCContentFilter(desktopIndependentWindow: /* ... */)
// Or capture everything except our own app:
let filter = SCContentFilter(
    display: content.displays[0],
    excludingApplications: [/* self */],
    exceptingWindows: []
)
```

#### Permissions

- Requires **Screen Recording** permission (System Settings > Privacy & Security > Screen Recording)
- On first launch, macOS prompts the user automatically
- The UX is slightly confusing ("Screen Recording" for an audio tool) but there's no way around it
- The permission prompt text can be customized via `Info.plist`:
  ```xml
  <key>NSScreenCaptureUsageDescription</key>
  <string>Ghostwriter needs Screen Recording permission to capture meeting audio.</string>
  ```

**Important limitations:**
- On macOS 13, you **cannot** capture audio-only. SCStream requires a video stream. Workaround: set `minimumFrameInterval` to 1 FPS, resolution to 2x2 pixels, and discard video frames.
- On macOS 14.2+, use Core Audio Taps instead (see 2.3) for true audio-only.
- macOS 15 (Sequoia) introduced **recurring permission prompts** — after every relaunch, the user is re-prompted. MDM-managed devices can suppress this.
- **Code signing matters:** Ad-hoc signed binaries can't properly use TCC. Need a proper Developer ID signature for permissions to persist.

#### Go Integration Strategy

ScreenCaptureKit is an Objective-C/Swift framework. Options for Go:

**Option A: Swift helper binary (RECOMMENDED)**

Build a small Swift CLI tool (`ghostwriter-capture`) that:
1. Takes args: `--app-bundle-id us.zoom.xos` or `--all-audio`
2. Captures audio via ScreenCaptureKit (macOS 13) or Core Audio Taps (macOS 14.2+)
3. Writes raw PCM to stdout (Go reads it via pipe)
4. Responds to SIGINT to stop gracefully

Go launches it as a subprocess (same as current ffmpeg approach, but native).

**Existing reference implementation: [AudioTee](https://github.com/makeusabrew/audiotee)**
- Swift CLI, no external dependencies
- Captures system audio using `AudioHardwareCreateProcessTap` (macOS 14.2+)
- Writes raw PCM to stdout in 0.2s chunks
- Logs to stderr (clean separation)
- Captures audio pre-mixer, works even at zero volume
- Can filter by specific PIDs

Benefits:
- Clean separation — test Swift helper and Go daemon independently
- Swift has first-class ScreenCaptureKit/CoreAudio support
- No cgo complexity
- Bundle as universal binary (arm64 + x86_64) — no Swift toolchain needed on user machines

**Option B: cgo with Objective-C**

Write Objective-C code called via cgo. More complex, tighter coupling, but single binary. Not recommended because ScreenCaptureKit uses async/await and delegates that are painful to bridge via cgo.

**Option C: Keep ffmpeg but use ScreenCaptureKit audio device**

On macOS 15+, ScreenCaptureKit can expose a virtual audio device. ffmpeg captures from it. But this is bleeding-edge and unreliable.

**Option D: [screencapturekit-go](https://github.com/tfsoares/screencapturekit-go)**

Go library that bundles a Swift helper binary internally. Supports audio-only recording. But still requires BlackHole for system audio (doesn't use Core Audio Taps). Requires Swift toolchain on build machine.

### 2.3 AudioHardwareCreateProcessTap (macOS 14.2+) — PREFERRED

New in macOS 14.2 (Sonoma), `AudioHardwareCreateProcessTap` is a Core Audio C API that lets you tap audio from specific processes.

```c
// Create a process tap for all system audio
CATapDescription tapDesc = {
    .processes = NULL,  // NULL = all processes
    .mono = true,
};
AudioObjectID tapID;
AudioHardwareCreateProcessTap(&tapDesc, &tapID);
// Then create an aggregate device with the tap as a sub-device
// and use AVAudioEngine to pull audio from it
```

**Advantages:**
- **True audio-only** — no video stream overhead
- Per-process audio capture via PID filtering
- Captures audio **pre-mixer** — works even at zero system volume
- Lower-level, lower-latency than ScreenCaptureKit
- Permission: "System Audio Recording" (clearer UX than "Screen Recording")
- No recurring permission prompts like ScreenCaptureKit on macOS 15

**Disadvantages:**
- macOS 14.2+ only (narrower than ScreenCaptureKit's 13+)
- C API — needs cgo or a helper binary
- Less documented, but growing ecosystem

**Existing open-source implementations:**
| Project | Description |
|---------|-------------|
| [AudioTee](https://github.com/makeusabrew/audiotee) | Swift CLI, PCM to stdout, PID filtering |
| [AudioCap](https://github.com/insidegui/AudioCap) | Swift, macOS 14.4+ |
| [audiograb](https://github.com/obsfx/audiograb) | Swift CLI |
| [Recap](https://github.com/RecapAI/Recap) | Swift app, Core Audio Taps + AVAudioEngine |

**Verdict:** This should be the **primary** capture method for macOS 14.2+. Fall back to ScreenCaptureKit for macOS 13-14.1.

### 2.4 Recommended Architecture

```
┌─────────────────────────────────────────────────┐
│ AudioCapturer interface                          │
│   Start() error                                  │
│   Stop() (wavPath string, err error)             │
│   IsRecording() bool                             │
├─────────────────────────────────────────────────┤
│ SCKCapturer          │ FFmpegCapturer            │
│ (Swift helper binary │ (Current: ffmpeg +        │
│  via ScreenCaptureKit)│  BlackHole fallback)     │
│ macOS 13+            │ Any macOS with ffmpeg     │
├──────────────────────┼──────────────────────────┤
│ ProcessTapCapturer   │ FakeCapturer              │
│ (Core Audio tap,     │ (Tests: returns a known   │
│  macOS 14.2+)        │  WAV file)                │
└──────────────────────┴──────────────────────────┘
```

Auto-selection logic:
1. If macOS 14.2+ → try `ProcessTapCapturer` (no extra permissions)
2. If macOS 13+ → try `SCKCapturer` (needs Screen Recording)
3. Fallback → `FFmpegCapturer` (needs BlackHole)

---

## 3. Meeting Detection on macOS

### 3.1 Process Monitoring

#### Current Approach
Shells out to `ps -eo comm` every 5 seconds and does substring matching.

#### Better: NSWorkspace Notifications

macOS provides real-time app launch/terminate notifications via `NSWorkspace`:

```objective-c
// Notifications (no polling needed)
NSWorkspace.shared.notificationCenter.addObserver(
    forName: NSWorkspace.didLaunchApplicationNotification
) { notification in
    let app = notification.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
    // app.bundleIdentifier == "us.zoom.xos"
}

NSWorkspace.shared.notificationCenter.addObserver(
    forName: NSWorkspace.didTerminateApplicationNotification
) { ... }
```

**Bundle identifiers for meeting apps:**

| App | Bundle ID |
|-----|-----------|
| Zoom | `us.zoom.xos` |
| Microsoft Teams | `com.microsoft.teams` / `com.microsoft.teams2` |
| Slack | `com.tinyspeck.slackmacgap` |
| Discord | `com.hheavy.Discord` |
| Webex | `com.webex.meetingmanager` |
| FaceTime | `com.apple.FaceTime` |
| Skype | `com.skype.skype` |

**Go integration:** Same Swift helper binary approach, or poll `NSWorkspace.shared.runningApplications` (simpler for Go via `ps` — the polling approach is fine for 5-second intervals).

### 3.2 Microphone Usage Detection (KEY for accuracy)

Knowing an app is running isn't enough — Zoom runs all day. We need to know when it's **actively in a call**.

#### Core Audio: Audio Device Property Listeners

```c
// Check if any audio device is currently active
AudioObjectPropertyAddress addr = {
    .mSelector = kAudioDevicePropertyDeviceIsRunningSomewhere,
    .mScope = kAudioObjectPropertyScopeGlobal,
    .mElement = kAudioObjectPropertyElementMain
};

// Register a listener for changes
AudioObjectAddPropertyListener(deviceID, &addr, callback, NULL);
```

#### Better: Enumerate Audio Tap Clients (macOS 14+)

```c
// Get list of processes tapping audio
AudioObjectPropertyAddress addr = {
    .mSelector = kAudioDevicePropertyStreams,
    // ...
};
```

#### Practical Approach: Check microphone access

On macOS, you can check which processes are using the microphone:

```bash
# Check if microphone is in use (simple signal)
log stream --predicate 'subsystem == "com.apple.coreaudio"' --info
```

Or via the `AVCaptureDevice` authorization API:
```swift
// Check mic authorization status for the current process
AVCaptureDevice.authorizationStatus(for: .audio)
```

**For detecting OTHER apps using the mic**, the practical approach is:
1. Use `IOKit` to check if the built-in microphone is active
2. Cross-reference with running meeting apps
3. If (meeting_app_running AND mic_active) → likely in a call

```swift
// Check if built-in mic is active using IORegistry
func isMicrophoneInUse() -> Bool {
    // Query IOAudioEngine / IOAudioDevice for active status
    // Or use the simpler approach: check the orange dot indicator
    let audioDevices = ... // enumerate via CoreAudio
    for device in audioDevices {
        if isInputDevice(device) && isDeviceRunning(device) {
            return true
        }
    }
    return false
}
```

#### Recommended Detection State Machine

```
          ┌───────────────┐
          │     IDLE      │
          │ (no meeting   │
          │  app running) │
          └───────┬───────┘
                  │ Meeting app launched
                  ▼
          ┌───────────────┐
          │    ARMED      │
          │ (app running, │
          │  mic not yet  │
          │  active)      │
          └───────┬───────┘
                  │ Mic becomes active
                  │ (or calendar event starts)
                  ▼
          ┌───────────────┐
          │  RECORDING    │
          │ (capturing    │
          │  audio)       │
          └───────┬───────┘
                  │ Mic inactive for >grace_period
                  │ AND (no calendar event OR event ended)
                  ▼
          ┌───────────────┐
          │  COOLDOWN     │
          │ (grace period │
          │  before stop) │
          └───────┬───────┘
                  │ Timer expires
                  ▼
              PROCESSING → IDLE
```

This avoids false positives (Zoom running but not in a call) and handles meetings that run over.

### 3.3 Signal Sources and Confidence

| Signal | How to detect | Confidence |
|--------|--------------|------------|
| Meeting app running | `ps` or NSWorkspace | Low (app may be idle) |
| Microphone active | CoreAudio property listener | Medium (could be non-meeting) |
| Meeting app + mic active | Combination | High |
| Calendar event now + mic | Calendar API + CoreAudio | Very High |
| Manual trigger | CLI/tray | Certain |

---

## 4. Calendar Integration

### 4.1 Google Calendar API

#### OAuth2 Flow for Desktop Apps

Google supports the "Desktop app" OAuth flow using a loopback redirect:

1. **Create OAuth credentials** (Google Cloud Console → APIs & Services → Credentials → Desktop app)
2. **Request authorization** — open browser to:
   ```
   https://accounts.google.com/o/oauth2/v2/auth?
     client_id=CLIENT_ID&
     redirect_uri=http://localhost:PORT/callback&
     response_type=code&
     scope=https://www.googleapis.com/auth/calendar.readonly&
     access_type=offline
   ```
3. **Start local HTTP server** on `localhost:PORT` to receive the callback
4. **Exchange code for tokens** at `https://oauth2.googleapis.com/token`
5. **Store refresh token** in `~/.config/ghostwriter/google_token.json`
6. **Refresh access token** automatically when expired

#### Polling for Events

```
GET https://www.googleapis.com/calendar/v3/calendars/primary/events?
  timeMin=2026-03-04T00:00:00Z&
  timeMax=2026-03-04T23:59:59Z&
  singleEvents=true&
  orderBy=startTime
```

Poll every 60 seconds. Google's rate limit is generous (~10k requests/day per user).

#### Detecting Video Call Links

Calendar events contain conference data:

```json
{
  "conferenceData": {
    "entryPoints": [
      {
        "entryPointType": "video",
        "uri": "https://meet.google.com/abc-defg-hij"
      }
    ],
    "conferenceSolution": {
      "name": "Google Meet"
    }
  }
}
```

Also check `description` and `location` fields for Zoom/Teams links:
- Zoom: `https://zoom.us/j/...` or `https://*.zoom.us/j/...`
- Teams: `https://teams.microsoft.com/l/meetup-join/...`

#### Incremental Sync

Google supports sync tokens to avoid re-fetching everything:
```go
// First call: full sync
events, err := srv.Events.List("primary").SingleEvents(true).Do()
syncToken := events.NextSyncToken  // save this

// Subsequent calls: only get changes
events, err = srv.Events.List("primary").SyncToken(syncToken).Do()
// Handle 410 GONE by doing full sync again
```

**Note:** SyncToken cannot be combined with TimeMin/TimeMax. For a daemon that only cares about "meetings in the next 5 minutes," a simple time-bounded poll every 60 seconds is simpler.

#### Push Notifications

Google Calendar supports webhooks via `Events.Watch`, but requires a **publicly accessible HTTPS endpoint** with valid SSL and domain verification. **Impractical for a local daemon.** Use polling instead.

#### Go Libraries

```
go get google.golang.org/api/calendar/v3
go get golang.org/x/oauth2
go get golang.org/x/oauth2/google
go get google.golang.org/api/option
```

```go
import "google.golang.org/api/calendar/v3"

srv, err := calendar.NewService(ctx, option.WithTokenSource(tokenSource))
events, err := srv.Events.List("primary").
    TimeMin(now.Format(time.RFC3339)).
    TimeMax(now.Add(5*time.Minute).Format(time.RFC3339)).
    SingleEvents(true).
    OrderBy("startTime").
    MaxResults(10).
    Do()
```

#### Rate Limits

- 1,000,000 requests/day per project
- 25,000 requests per 100 seconds per user
- Polling every 60s = ~1,440 requests/day — well within limits
- Randomize poll interval (45-75s) to avoid spikes

### 4.2 Microsoft Outlook / O365

#### Microsoft Graph API

Endpoint: `GET https://graph.microsoft.com/v1.0/me/calendarView?startDateTime=...&endDateTime=...`

OAuth2 flow: **Device code flow** (recommended for CLI/daemon — works headless):

```
POST https://login.microsoftonline.com/common/oauth2/v2.0/devicecode
Content-Type: application/x-www-form-urlencoded

client_id=APP_ID&scope=Calendars.Read offline_access
```

User sees: "Go to https://microsoft.com/devicelogin and enter code ABCD-EFGH"

Permission needed: `Calendars.Read` (delegated).

#### Detecting Teams Meetings

The event resource has explicit online meeting fields:

```json
{
  "isOnlineMeeting": true,
  "onlineMeetingProvider": "teamsForBusiness",
  "onlineMeeting": {
    "joinUrl": "https://teams.microsoft.com/l/meetup-join/..."
  }
}
```

**Note:** `isOnlineMeeting` and `onlineMeeting.joinUrl` only work with **work/school accounts**, not personal @outlook.com.

#### Go Libraries

```
go get github.com/microsoftgraph/msgraph-sdk-go
go get github.com/Azure/azure-sdk-for-go/sdk/azidentity
```

The official SDK is heavyweight. For simpler needs, call the REST API directly with `net/http`.

### 4.3 Apple Calendar (EventKit) — RECOMMENDED FOR macOS v1

macOS provides `EventKit` framework for reading local calendar data (including synced Google/Outlook calendars).

**Go library: [`go-eventkit`](https://github.com/BRO3886/go-eventkit)** — native macOS EventKit access via cgo.

```go
import "github.com/BRO3886/go-eventkit/calendar"

// List all calendars (iCloud, Google, Exchange, etc.)
calendars, err := calendar.Calendars()

// Query events in the next 5 minutes
start := time.Now()
end := start.Add(5 * time.Minute)
events, err := calendar.Events(start, end)

// Filter by calendar name
events, err := calendar.Events(start, end, calendar.WithCalendar("Work"))

// Watch for changes (live notifications via EKEventStoreChanged)
ctx, cancel := context.WithCancel(context.Background())
changes := calendar.WatchChanges(ctx)
for range changes {
    // Re-query events — something changed in the calendar database
}
```

**Requirements:** macOS, Go 1.24+, Xcode Command Line Tools.

**Permission:** "Calendars" in System Settings > Privacy & Security. Prompted automatically on first access.

**Key advantage:** If the user has Google Calendar or Outlook already synced to macOS Calendar.app, EventKit reads them **all** without separate OAuth flows. This is the simplest path to calendar integration on macOS.

**`WatchChanges`** subscribes to `EKEventStoreChangedNotification` — the daemon gets notified immediately when calendar data changes (new event added, event modified, etc.) instead of polling.

**Recommended approach for v1:** Start with EventKit — it covers Google, Outlook, and iCloud calendars if the user has them synced to macOS. Only add direct API access (Google Calendar API, Microsoft Graph) for users who don't use macOS Calendar sync or want features like attendee email addresses that may not sync fully.

### 4.4 CalDAV / ICS

For self-hosted calendars (Nextcloud, Radicale, etc.):

Go libraries:
- `github.com/emersion/go-webdav` — CalDAV client (MIT, actively maintained)
- `github.com/arran4/golang-ical` — ICS parsing (Apache 2.0, 152+ importers)
- `github.com/fsnotify/fsnotify` — watch local `.ics` files for changes

```go
// CalDAV: query events in a time range
import "github.com/emersion/go-webdav/caldav"

client, _ := caldav.NewClient(httpClient, "https://caldav.example.com/user/calendars/")
query := &caldav.CalendarQuery{
    CompFilter: caldav.CompFilter{
        Name: "VCALENDAR",
        Comps: []caldav.CompFilter{{
            Name: "VEVENT",
            TimeRange: caldav.TimeRange{Start: time.Now(), End: time.Now().Add(24*time.Hour)},
        }},
    },
}
objects, _ := client.QueryCalendar(ctx, calendarPath, query)

// ICS file parsing
import ics "github.com/arran4/golang-ical"
f, _ := os.Open("calendar.ics")
cal, _ := ics.ParseCalendar(f)
for _, event := range cal.Events() {
    startTime, _ := event.GetStartAt()
    summary := event.GetProperty(ics.ComponentPropertySummary)
}
```

---

## 5. Browser Meeting Detection

### 5.1 The Problem

When someone joins Google Meet or Zoom via Chrome/Safari/Firefox/Arc, there's no separate "meeting app" process. Chrome is just Chrome.

### 5.2 Signals Available

| Signal | How | Reliability |
|--------|-----|-------------|
| Browser using microphone | CoreAudio device listener | High — but could be non-meeting |
| Tab title contains meeting keywords | macOS Accessibility API (`AXUIElement`) | Medium — requires Accessibility permission |
| Calendar event with Meet/Zoom link happening now | Calendar API | High — but requires calendar access |
| Browser navigated to meet.google.com / zoom.us | Accessibility API to read URL bar | Low — fragile, browser-dependent |

### 5.3 Recommended Approach

**Combine signals with confidence scoring:**

```
browser_mic_active                           → confidence: 0.3
browser_mic_active + calendar_event_now      → confidence: 0.8
browser_mic_active + calendar_event_with_link → confidence: 0.95
browser_mic_active + tab_title_match         → confidence: 0.9
```

Start recording when confidence ≥ 0.7 (configurable threshold).

### 5.4 Accessibility API for Tab Titles

```swift
// Get Chrome's focused tab title via Accessibility API
let app = AXUIElementCreateApplication(chromePID)
var focusedWindow: AnyObject?
AXUIElementCopyAttributeValue(app, kAXFocusedWindowAttribute, &focusedWindow)
// Navigate AX tree to find the title/URL bar
```

**Requires:** Accessibility permission (System Settings > Privacy & Security > Accessibility).

**Known patterns in tab titles:**
- Google Meet: "Meet - xxx-xxxx-xxx" or meeting title
- Zoom Web: "Zoom Meeting" or "Zoom Webinar"
- Teams Web: "Meeting | Microsoft Teams"

### 5.5 Practical Recommendation for v1

Don't overcomplicate browser detection. The most reliable approach:

1. Detect microphone usage (via CoreAudio)
2. Check if any browser process is the one using the mic
3. If a calendar event with a video link is happening now → assume it's that meeting
4. If no calendar data → still record (mic + browser = "probably a meeting")

This gives ~90% accuracy without the fragility of Accessibility API scraping.

---

## 6. Integration Test Architecture

### 6.1 Philosophy

> Integration tests over unit tests. Test real pipelines, not mocked internals.

The tests should exercise the **actual code paths** — daemon event loop, socket IPC, transcript storage, detection signals — with only the system-boundary dependencies faked:

| Real in tests | Faked in tests |
|---|---|
| Daemon event loop | Audio capture hardware |
| Socket IPC (client ↔ daemon) | Whisper transcription binary |
| Transcript storage (real filesystem) | Meeting app processes |
| Detection state machine | Calendar API (use fixture data) |
| Signal → Record → Transcribe → Store pipeline | ScreenCaptureKit |

### 6.2 Test Double Strategy

We need three fakes:

#### FakeCapturer
```go
// Returns a pre-recorded WAV file (or generates silence)
type FakeCapturer struct {
    wavPath    string    // path to fixture WAV
    started    bool
    mu         sync.Mutex
    StartErr   error     // inject errors
    StopErr    error
    OnStart    func()    // hook for assertions
    OnStop     func()
}

func (f *FakeCapturer) Start() error {
    f.mu.Lock()
    defer f.mu.Unlock()
    if f.StartErr != nil { return f.StartErr }
    if f.OnStart != nil { f.OnStart() }
    f.started = true
    return nil
}

func (f *FakeCapturer) Stop() (string, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    if f.StopErr != nil { return "", f.StopErr }
    if f.OnStop != nil { f.OnStop() }
    f.started = false
    // Copy fixture WAV to temp location so it can be "consumed"
    tmp := copyToTemp(f.wavPath)
    return tmp, nil
}
```

#### FakeTranscriber
```go
// Returns a canned transcript for any input
type FakeTranscriber struct {
    Result    *output.Transcript
    Err       error
    Calls     []string   // recorded file paths for assertions
    Delay     time.Duration  // simulate processing time
}

func (f *FakeTranscriber) TranscribeFile(path string) (*output.Transcript, error) {
    f.Calls = append(f.Calls, path)
    if f.Delay > 0 { time.Sleep(f.Delay) }
    if f.Err != nil { return nil, f.Err }
    // Deep copy to avoid shared state
    result := *f.Result
    return &result, nil
}
```

#### FakeDetector
```go
// Emits signals on demand via a channel you control
type FakeDetector struct {
    signals chan detect.Signal
}

func NewFakeDetector() *FakeDetector {
    return &FakeDetector{signals: make(chan detect.Signal, 10)}
}

func (f *FakeDetector) Start(ctx context.Context) <-chan detect.Signal {
    return f.signals
}

func (f *FakeDetector) Emit(sig detect.Signal) {
    f.signals <- sig
}
```

### 6.3 Test Fixture: Sample WAV + Expected Transcript

Create a test fixture directory:

```
testdata/
├── fixtures/
│   ├── short_meeting.wav         # 5-second WAV with speech
│   ├── short_meeting.transcript.json  # Expected output for that WAV
│   ├── silence.wav               # WAV with no speech
│   └── calendar_events.json      # Sample Google Calendar response
```

For CI environments without whisper-cli, the `FakeTranscriber` returns the canned transcript. For local macOS dev machines, an optional test tag can run the **real** whisper-cli against the WAV fixture.

### 6.4 Test Helpers

```go
// testutil/daemon.go
func StartTestDaemon(t *testing.T, opts ...DaemonOption) (*daemon.Daemon, *daemon.Client, func()) {
    t.Helper()

    tmpDir := t.TempDir()
    socketPath := filepath.Join(tmpDir, "test.sock")
    outputDir := filepath.Join(tmpDir, "transcripts")

    cfg := daemon.DaemonConfig{
        Detector:    NewFakeDetector(),
        Capture:     NewFakeCapturer("testdata/fixtures/short_meeting.wav"),
        Transcriber: NewFakeTranscriber(loadFixture("testdata/fixtures/short_meeting.transcript.json")),
        Store:       output.NewStore(outputDir),
        SocketPath:  socketPath,
        OutputDir:   outputDir,
    }

    for _, opt := range opts {
        opt(&cfg)
    }

    d, err := daemon.New(cfg)
    require.NoError(t, err)

    ctx, cancel := context.WithCancel(context.Background())
    go d.Run(ctx)  // Run takes context, not signals

    // Wait for socket to be ready
    client := waitForClient(t, socketPath)

    cleanup := func() {
        cancel()
        // wait for daemon to stop
    }

    return d, client, cleanup
}
```

---

## 7. Integration Test Specs

### Test 1: Full Recording Pipeline (Manual Trigger)

**What it tests:** CLI → Socket → Daemon → Capture → Transcribe → Store

```
Given: daemon is running with FakeCapturer and FakeTranscriber
When:  client sends CmdStartRecording with title "Test Meeting"
Then:  daemon state becomes StateRecording
       AND FakeCapturer.Start() was called

When:  client sends CmdStopRecording
Then:  FakeCapturer.Stop() was called
       AND FakeTranscriber.TranscribeFile() was called with the WAV path
       AND a .transcript.json file exists in the store
       AND the transcript contains the expected text
       AND the transcript metadata has title "Test Meeting"
       AND daemon state eventually becomes StateIdle
```

### Test 2: Auto-Detection Pipeline

**What it tests:** Detection signal → automatic recording start/stop

```
Given: daemon is running with FakeDetector, FakeCapturer, FakeTranscriber
When:  FakeDetector emits Signal{Type: SignalStarted, App: "zoom.us"}
Then:  daemon state becomes StateRecording
       AND FakeCapturer.Start() was called

When:  FakeDetector emits Signal{Type: SignalEnded, App: "zoom.us"}
Then:  FakeCapturer.Stop() was called
       AND FakeTranscriber.TranscribeFile() was called
       AND transcript is written to disk
       AND transcript.Metadata.DetectedApp == "zoom.us"
```

### Test 3: Status Reporting

**What it tests:** Status queries reflect real state throughout lifecycle

```
Given: daemon is running

When:  client sends CmdStatus
Then:  response.Status.State == StateIdle

When:  client sends CmdStartRecording
       AND client sends CmdStatus
Then:  response.Status.State == StateRecording
       AND response.Status.CurrentMeeting is set
       AND response.Status.Duration is non-empty

When:  client sends CmdStopRecording
       AND wait for transcription to complete
       AND client sends CmdStatus
Then:  response.Status.State == StateIdle
```

### Test 4: Concurrent Command Rejection

**What it tests:** Can't start recording twice, can't stop when idle

```
Given: daemon is recording

When:  client sends CmdStartRecording
Then:  response.OK == false
       AND response.Error contains "already recording"

Given: daemon is idle

When:  client sends CmdStopRecording
Then:  response.OK == false
       AND response.Error contains "not recording"
```

### Test 5: Capture Failure Handling

**What it tests:** Daemon recovers gracefully from capture errors

```
Given: daemon with FakeCapturer configured to return error on Start()

When:  client sends CmdStartRecording
Then:  response.OK == false
       AND daemon state remains StateIdle

Given: daemon with FakeCapturer configured to return error on Stop()

When:  client sends CmdStartRecording (succeeds)
       AND client sends CmdStopRecording
Then:  daemon state eventually becomes StateIdle (not stuck in Recording/Processing)
       AND no transcript file is written
```

### Test 6: Transcription Failure Handling

**What it tests:** Daemon recovers from transcription errors without losing state

```
Given: daemon with FakeTranscriber configured to return error

When:  recording starts and stops successfully
Then:  daemon state eventually becomes StateIdle
       AND no transcript file is written
       AND daemon can start a new recording (not stuck)
```

### Test 7: Transcript Storage Round-Trip

**What it tests:** Write → List → Get → Search on real filesystem

```
Given: an empty Store in a temp directory

When:  Store.Write(transcript1) with title "Sprint Planning"
       AND Store.Write(transcript2) with title "1:1 with Sarah"

Then:  Store.List(zeroTime) returns both transcripts sorted by date (newest first)
       AND Store.Get(transcript1.ID) returns the correct transcript
       AND Store.Search("Sprint") returns SearchResult with transcript1
       AND Store.Search("nonexistent") returns empty
       AND files exist at expected year/month paths on disk
```

### Test 8: Socket IPC Round-Trip

**What it tests:** Full JSON encode/decode over Unix domain socket

```
Given: a Socket listening on a temp path

When:  a Client connects and sends CmdStatus
Then:  the daemon receives the command
       AND the response is correctly JSON-encoded
       AND the client receives and decodes the response

When:  multiple clients connect concurrently
Then:  all receive correct responses (no data corruption)
```

### Test 9: Graceful Shutdown During Recording

**What it tests:** Clean shutdown doesn't lose data

```
Given: daemon is actively recording

When:  context is cancelled (simulating SIGTERM)
Then:  FakeCapturer.Stop() is called
       AND transcription runs to completion
       AND transcript is written before daemon exits
       AND socket file is cleaned up
       AND PID file is cleaned up
```

### Test 10: Detection Signal Debouncing

**What it tests:** Rapid signals don't cause multiple recordings

```
Given: daemon is running with FakeDetector

When:  FakeDetector emits SignalStarted("zoom.us")
       AND immediately emits SignalStarted("Microsoft Teams")
Then:  only one recording is started
       AND FakeCapturer.Start() is called exactly once

When:  FakeDetector emits SignalEnded("Microsoft Teams")
       AND "zoom.us" is still "running"
Then:  recording does NOT stop (at least one meeting app still active)
```

### Test 11: Calendar-Correlated Recording (Future)

**What it tests:** Calendar events enhance transcript metadata

```
Given: daemon with a FakeCalendar returning an event:
       { title: "Sprint Planning", start: now, end: now+1h,
         attendees: ["ian@co.com", "sarah@co.com"],
         conferenceLink: "https://meet.google.com/abc" }

When:  FakeDetector emits SignalStarted("Google Chrome")
       AND mic becomes active (FakeMicDetector signals)

Then:  recording starts
       AND transcript.Metadata.Title == "Sprint Planning"
       AND transcript.Metadata.Attendees contains both emails
       AND transcript.Metadata.Source == "calendar+audio"
```

### Test 12: End-to-End with Real Whisper (build tag: `integration_whisper`)

**What it tests:** Actual transcription of a known WAV file

```
//go:build integration_whisper

Given: whisper-cli is installed AND base.en model is downloaded

When:  transcriber.TranscribeFile("testdata/fixtures/short_meeting.wav")
Then:  result.FullText contains expected phrases
       AND result.Segments has reasonable timestamps
       AND result.Segments[*].Confidence > 0.5
```

This test only runs on macOS dev machines with `go test -tags integration_whisper`.

### Test 13: Process Monitor on macOS (build tag: `integration_darwin`)

**What it tests:** Real process detection (not faked)

```
//go:build integration_darwin

Given: process monitor is running

When:  we check for meeting apps and none are running
Then:  DetectMeetingApp() returns ""

// Manual verification: start Zoom, run test, verify detection
```

---

## 8. Build Tags and Test Organization

```
internal/
├── capture/
│   ├── capture.go           # AudioCapturer interface + FFmpegCapturer
│   ├── sck_darwin.go        # ScreenCaptureKit capturer (macOS 13+)
│   └── capture_test.go      # Tests with FakeCapturer
├── daemon/
│   ├── daemon.go            # Daemon with DaemonConfig (injectable)
│   ├── daemon_test.go       # Integration tests (Tests 1-6, 9-10)
│   ├── socket.go
│   └── socket_test.go       # IPC tests (Test 8)
├── detect/
│   ├── detect.go            # MeetingDetector interface + state machine
│   ├── process_darwin.go    # macOS process monitor
│   ├── process_stub.go      # No-op for non-darwin
│   ├── mic_darwin.go        # CoreAudio mic detection
│   └── detect_test.go       # Tests with FakeDetector
├── output/
│   ├── store.go
│   ├── store_test.go        # Storage round-trip tests (Test 7)
│   └── transcript.go
├── transcribe/
│   ├── transcribe.go        # Transcriber interface
│   ├── whisper.go           # WhisperTranscriber
│   └── transcribe_test.go   # FakeTranscriber + optional real whisper test
├── calendar/                # NEW
│   ├── calendar.go          # CalendarSource interface
│   ├── eventkit_darwin.go   # Apple EventKit
│   ├── google.go            # Google Calendar API
│   └── calendar_test.go     # Tests with fixture data
└── testutil/                # NEW
    ├── fakes.go             # FakeCapturer, FakeTranscriber, FakeDetector
    ├── fixtures.go          # Load test fixtures
    └── helpers.go           # StartTestDaemon, etc.
```

### Running Tests

```bash
# All integration tests (no external dependencies needed)
go test ./...

# Include real whisper transcription tests (needs whisper-cli + model)
go test -tags integration_whisper ./internal/transcribe/

# Include real macOS process detection (needs macOS)
go test -tags integration_darwin ./internal/detect/

# Verbose with race detection
go test -race -v ./...
```

---

## 9. Go Dependencies Summary

| Purpose | Package | Phase |
|---------|---------|-------|
| Apple Calendar | `github.com/BRO3886/go-eventkit` | Phase 1 (macOS) |
| Google Calendar | `google.golang.org/api/calendar/v3` | Phase 2 |
| Google OAuth2 | `golang.org/x/oauth2`, `golang.org/x/oauth2/google` | Phase 2 |
| Microsoft Graph | `github.com/microsoftgraph/msgraph-sdk-go` | Phase 3 |
| Azure Identity | `github.com/Azure/azure-sdk-for-go/sdk/azidentity` | Phase 3 |
| CalDAV client | `github.com/emersion/go-webdav` | Phase 4 |
| ICS parser | `github.com/arran4/golang-ical` | Phase 4 |
| File watching | `github.com/fsnotify/fsnotify` | Phase 4 |
| Test assertions | `github.com/stretchr/testify` | Now |

---

## 10. Priority Order for Implementation

1. **Refactor Daemon for dependency injection** — unlocks all integration tests
2. **Define interfaces** (AudioCapturer, MeetingDetector) + write fakes
3. **Write integration tests** (Tests 1-10) — they'll fail initially, then drive implementation
4. **Fix build constraints** (process_darwin.go + process_stub.go)
5. **Add detection state machine** (IDLE → ARMED → RECORDING → COOLDOWN)
6. **Add mic detection** (CoreAudio device property listeners on macOS)
7. **Add calendar integration** (EventKit first — covers Google/Outlook if synced to macOS)
8. **Build Swift capture helper** (Core Audio Taps on macOS 14.2+, ScreenCaptureKit fallback)
9. **Browser meeting detection** (mic + calendar correlation)
10. **Direct Google Calendar API** (for users not using macOS Calendar sync)
