package detect

import (
	"context"
	"log"
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

const micDebounceThreshold = 2

type Detector struct {
	processMonitor *ProcessMonitor
	micMonitor     *MicMonitor
	pollInterval   time.Duration
}

func New() *Detector {
	return &Detector{
		processMonitor: NewProcessMonitor(),
		micMonitor:     NewMicMonitor(),
		pollInterval:   5 * time.Second,
	}
}

// Start begins watching for meeting signals and returns a channel of events.
// Native meeting apps trigger immediately. Browser apps require the microphone
// to be active for 2 consecutive polls (10 seconds) before triggering, which
// filters out transient mic access like permission prompts.
func (d *Detector) Start(ctx context.Context) <-chan Signal {
	signals := make(chan Signal, 4)

	go func() {
		defer close(signals)

		var activeMeeting string
		var activeMeetingIsBrowser bool
		var micActiveCount int
		var micInactiveCount int

		ticker := time.NewTicker(d.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				app := d.processMonitor.DetectMeetingApp()
				micActive := d.micMonitor.IsMicActive()

				if activeMeeting == "" {
					d.handleNoActiveMeeting(app, micActive, &micActiveCount, &activeMeeting, &activeMeetingIsBrowser, signals)
				} else {
					d.handleActiveMeeting(app, micActive, &micInactiveCount, &activeMeeting, &activeMeetingIsBrowser, &micActiveCount, signals)
				}
			}
		}
	}()

	return signals
}

func (d *Detector) handleNoActiveMeeting(
	app AppMatch,
	micActive bool,
	micActiveCount *int,
	activeMeeting *string,
	activeMeetingIsBrowser *bool,
	signals chan<- Signal,
) {
	if app.Name == "" {
		*micActiveCount = 0
		return
	}

	if !app.IsBrowser {
		*activeMeeting = app.Name
		*activeMeetingIsBrowser = false
		*micActiveCount = 0
		signals <- Signal{Type: SignalStarted, App: app.Name}
		log.Printf("meeting detected (native app): %s", app.Name)
		return
	}

	if !micActive {
		*micActiveCount = 0
		return
	}

	*micActiveCount++
	if *micActiveCount >= micDebounceThreshold {
		*activeMeeting = app.Name
		*activeMeetingIsBrowser = true
		*micActiveCount = 0
		signals <- Signal{Type: SignalStarted, App: app.Name}
		log.Printf("meeting detected (browser + mic): %s", app.Name)
	}
}

func (d *Detector) handleActiveMeeting(
	app AppMatch,
	micActive bool,
	micInactiveCount *int,
	activeMeeting *string,
	activeMeetingIsBrowser *bool,
	micActiveCount *int,
	signals chan<- Signal,
) {
	ended := false

	if app.Name == "" {
		ended = true
	} else if *activeMeetingIsBrowser && !micActive {
		*micInactiveCount++
		if *micInactiveCount >= micDebounceThreshold {
			ended = true
		}
	} else {
		*micInactiveCount = 0
	}

	if ended {
		log.Printf("meeting ended: %s", *activeMeeting)
		signals <- Signal{Type: SignalEnded, App: *activeMeeting}
		*activeMeeting = ""
		*activeMeetingIsBrowser = false
		*micActiveCount = 0
		*micInactiveCount = 0
	}
}
