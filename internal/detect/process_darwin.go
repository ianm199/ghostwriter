package detect

import (
	"os/exec"
	"strings"
)

var meetingApps = []string{
	"zoom.us",
	"Microsoft Teams",
	"Slack",
	"Discord",
	"Webex",
}

type ProcessMonitor struct {
	apps []string
}

func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{apps: meetingApps}
}

// DetectMeetingApp checks if any known meeting application is currently running.
// Returns the app name if found, empty string otherwise.
func (p *ProcessMonitor) DetectMeetingApp() string {
	out, err := exec.Command("ps", "-eo", "comm").Output()
	if err != nil {
		return ""
	}

	processes := string(out)
	for _, app := range p.apps {
		if strings.Contains(processes, app) {
			return app
		}
	}
	return ""
}
