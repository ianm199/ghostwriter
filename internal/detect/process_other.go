//go:build !darwin

package detect

type ProcessMonitor struct {
	apps []string
}

func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{}
}

// DetectMeetingApp is a no-op on non-darwin platforms.
func (p *ProcessMonitor) DetectMeetingApp() string {
	return ""
}
