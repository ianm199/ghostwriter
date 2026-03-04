package detect

import (
	"os/exec"
	"strings"

	mediadevicesstate "github.com/antonfisher/go-media-devices-state"
)

var meetingProcesses = []string{
	"CptHost",
	"(WebexAppLauncher)",
}

var micRequiredApps = []string{
	"Slack",
	"Discord",
	"Microsoft Teams",
	"FaceTime",
}

var browserApps = []string{
	"Google Chrome",
	"Arc",
	"Firefox",
	"Safari",
	"Brave Browser",
	"Microsoft Edge",
}

type AppMatch struct {
	Name       string
	NeedsMic   bool
}

type ProcessMonitor struct {
	meetingProcesses []string
	micRequiredApps  []string
	browserApps      []string
}

func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{
		meetingProcesses: meetingProcesses,
		micRequiredApps:  micRequiredApps,
		browserApps:      browserApps,
	}
}

// DetectMeetingApp checks if any known meeting application is currently running.
// Returns the matched app and whether mic confirmation is needed.
//
// Priority order:
//  1. Meeting-specific processes (CptHost, WebexAppLauncher) — unambiguous
//  2. Browsers — checked before desktop apps because when both are running
//     and the mic is active, the browser is almost always the meeting source
//     (Google Meet, Zoom Web, etc.)
//  3. Desktop apps (Slack, Discord, Teams) — only match if no browser is running
func (p *ProcessMonitor) DetectMeetingApp() AppMatch {
	out, err := exec.Command("ps", "-eo", "comm").Output()
	if err != nil {
		return AppMatch{}
	}

	processes := string(out)

	for _, proc := range p.meetingProcesses {
		if strings.Contains(processes, proc) {
			return AppMatch{Name: proc, NeedsMic: false}
		}
	}

	for _, app := range p.browserApps {
		if strings.Contains(processes, app) {
			return AppMatch{Name: app, NeedsMic: true}
		}
	}

	for _, app := range p.micRequiredApps {
		if strings.Contains(processes, app) {
			return AppMatch{Name: app, NeedsMic: true}
		}
	}

	return AppMatch{}
}

type MicMonitor struct{}

func NewMicMonitor() *MicMonitor {
	return &MicMonitor{}
}

// IsMicActive returns true if any input audio device is currently in use.
func (m *MicMonitor) IsMicActive() bool {
	on, err := mediadevicesstate.IsMicrophoneOn()
	if err != nil {
		return false
	}
	return on
}
