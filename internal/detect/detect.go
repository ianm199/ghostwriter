package detect

import (
	"context"
	"time"
)

type SignalType int

const (
	SignalStarted SignalType = iota
	SignalEnded
)

type Signal struct {
	Type SignalType
	App  string
}

type Detector struct {
	processMonitor *ProcessMonitor
	pollInterval   time.Duration
}

func New() *Detector {
	return &Detector{
		processMonitor: NewProcessMonitor(),
		pollInterval:   5 * time.Second,
	}
}

// Start begins watching for meeting signals and returns a channel of events.
func (d *Detector) Start(ctx context.Context) <-chan Signal {
	signals := make(chan Signal, 4)

	go func() {
		defer close(signals)

		var activeMeeting string
		ticker := time.NewTicker(d.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				app := d.processMonitor.DetectMeetingApp()

				if app != "" && activeMeeting == "" {
					activeMeeting = app
					signals <- Signal{Type: SignalStarted, App: app}
				}

				if app == "" && activeMeeting != "" {
					signals <- Signal{Type: SignalEnded, App: activeMeeting}
					activeMeeting = ""
				}
			}
		}
	}()

	return signals
}
