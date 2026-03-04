package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/ianmclaughlin/ghostwriter/pkg/sysaware"
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

var meetingProcesses = []string{
	"CptHost",
	"(WebexAppLauncher)",
}

var browserApps = []string{
	"Google Chrome",
	"Arc",
	"Firefox",
	"Safari",
	"Brave Browser",
	"Microsoft Edge",
}

var micRequiredApps = []string{
	"Slack",
	"Discord",
	"Microsoft Teams",
	"FaceTime",
}

type appMatch struct {
	name     string
	needsMic bool
}

const (
	pollInterval          = 5 * time.Second
	micDebounceThreshold  = 2
)

func detectMeetingApp(procs sysaware.ProcessChecker) appMatch {
	running := strings.Join(procs.RunningProcesses(), "\n")

	for _, proc := range meetingProcesses {
		if strings.Contains(running, proc) {
			return appMatch{name: proc, needsMic: false}
		}
	}
	for _, app := range browserApps {
		if strings.Contains(running, app) {
			return appMatch{name: app, needsMic: true}
		}
	}
	for _, app := range micRequiredApps {
		if strings.Contains(running, app) {
			return appMatch{name: app, needsMic: true}
		}
	}
	return appMatch{}
}

func startMeetingDetector(ctx context.Context, procs sysaware.ProcessChecker, mic sysaware.MicDetector) <-chan Signal {
	signals := make(chan Signal, 4)

	go func() {
		defer close(signals)

		var activeMeeting string
		var activeMeetingNeedsMic bool
		var micActiveCount int
		var micInactiveCount int

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				app := detectMeetingApp(procs)
				micActive := mic.IsActive()

				if activeMeeting == "" {
					handleNoActiveMeeting(app, micActive, &micActiveCount, &activeMeeting, &activeMeetingNeedsMic, signals)
				} else {
					handleActiveMeeting(app, micActive, &micInactiveCount, &activeMeeting, &activeMeetingNeedsMic, &micActiveCount, signals)
				}
			}
		}
	}()

	return signals
}

func handleNoActiveMeeting(
	app appMatch,
	micActive bool,
	micActiveCount *int,
	activeMeeting *string,
	activeMeetingNeedsMic *bool,
	signals chan<- Signal,
) {
	if app.name == "" {
		*micActiveCount = 0
		return
	}

	if !app.needsMic {
		*activeMeeting = app.name
		*activeMeetingNeedsMic = false
		*micActiveCount = 0
		signals <- Signal{Type: SignalStarted, App: app.name}
		log.Printf("meeting detected (meeting process): %s", app.name)
		return
	}

	if !micActive {
		*micActiveCount = 0
		return
	}

	*micActiveCount++
	if *micActiveCount >= micDebounceThreshold {
		*activeMeeting = app.name
		*activeMeetingNeedsMic = true
		*micActiveCount = 0
		signals <- Signal{Type: SignalStarted, App: app.name}
		log.Printf("meeting detected (app + mic): %s", app.name)
	}
}

func handleActiveMeeting(
	app appMatch,
	micActive bool,
	micInactiveCount *int,
	activeMeeting *string,
	activeMeetingNeedsMic *bool,
	micActiveCount *int,
	signals chan<- Signal,
) {
	ended := false

	if app.name == "" {
		ended = true
	} else if *activeMeetingNeedsMic && !micActive {
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
		*activeMeetingNeedsMic = false
		*micActiveCount = 0
		*micInactiveCount = 0
	}
}
