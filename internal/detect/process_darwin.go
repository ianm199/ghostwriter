package detect

import (
	"os/exec"
	"strings"

	mediadevicesstate "github.com/antonfisher/go-media-devices-state"
)

var nativeApps = []string{
	"zoom.us",
	"Microsoft Teams",
	"Slack",
	"Discord",
	"Webex",
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
	Name      string
	IsBrowser bool
}

type ProcessMonitor struct {
	nativeApps  []string
	browserApps []string
}

func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{
		nativeApps:  nativeApps,
		browserApps: browserApps,
	}
}

// DetectMeetingApp checks if any known meeting application is currently running.
// Returns the matched app and whether it's a browser. Returns empty AppMatch if
// no meeting app is found. Native apps are checked first since they're
// unambiguous meeting signals.
func (p *ProcessMonitor) DetectMeetingApp() AppMatch {
	out, err := exec.Command("ps", "-eo", "comm").Output()
	if err != nil {
		return AppMatch{}
	}

	processes := string(out)

	for _, app := range p.nativeApps {
		if strings.Contains(processes, app) {
			return AppMatch{Name: app, IsBrowser: false}
		}
	}

	for _, app := range p.browserApps {
		if strings.Contains(processes, app) {
			return AppMatch{Name: app, IsBrowser: true}
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
