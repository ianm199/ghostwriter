package tray

// TODO: system tray integration
// Options for Go on macOS:
// - github.com/getlantern/systray — most popular, works well
// - Use ObjC bridge via cgo for native NSStatusItem
//
// Tray states:
// - Idle: grey icon, menu shows "Start Recording"
// - Recording: red icon (or pulsing), menu shows "Stop Recording" + duration
// - Processing: yellow icon, menu shows "Transcribing..."
//
// Menu items:
// - Start/Stop Recording
// - Status line (current state + duration)
// - Open transcript folder
// - Quit
