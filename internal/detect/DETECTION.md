# Meeting Detection Strategy

## Dual-Signal Detection: Process + Microphone

Ghostwriter detects meetings using two signal sources:

1. **Process monitoring** — checks every 5 seconds for known meeting apps via `ps`
2. **Microphone state** — queries CoreAudio `kAudioDevicePropertyDeviceIsRunningSomewhere` to determine if any input device is active

## Why Two Signals?

Native meeting apps (Zoom, Teams, Slack, Discord, Webex, FaceTime) are unambiguous — if the process is running, the user is likely in a meeting. A single signal is sufficient.

Browsers are different. Chrome or Arc are always running, so their presence alone means nothing. The mic check disambiguates: "Chrome playing YouTube" (no mic) vs "Chrome in a Google Meet call" (mic active).

This dual-signal approach (mic active + meeting-capable app running) is the industry standard pattern used by MacWhisper, trnscrb, and OverSight.

## Debounce

Browser-based meetings require the mic to be active for 2 consecutive polls (10+ seconds) before triggering. This prevents false positives from:

- Permission prompts that briefly activate the mic
- Quick mute/unmute toggling
- Voice search or other transient mic usage

The same debounce applies to meeting end detection — the mic must be inactive for 2 consecutive polls before the meeting is considered over.

## App Categories

**Native apps** (trigger immediately):
`zoom.us`, `Microsoft Teams`, `Slack`, `Discord`, `Webex`, `FaceTime`

**Browser apps** (require mic confirmation):
`Google Chrome`, `Arc`, `Firefox`, `Safari`, `Brave Browser`, `Microsoft Edge`

## Microphone Permissions

The mic state check does not require microphone TCC permission. It queries read-only device metadata (whether the device is in use), not the audio stream itself. This is the same API used by macOS menu bar mic indicators. Requires Xcode CLI tools for the cgo build.
