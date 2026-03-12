package daemon

import (
	"context"
	"log"
	"time"

	"github.com/ianmclaughlin/ghostwriter/pkg/calendar"
)

const calendarPollInterval = 60 * time.Second

type CalendarState struct {
	CurrentEvent *calendar.Event
}

func startCalendarPoller(ctx context.Context, cal calendar.CalendarSource) <-chan CalendarState {
	ch := make(chan CalendarState, 1)

	go func() {
		defer close(ch)

		poll := func() {
			now := time.Now()
			events, err := cal.Events(now, now.Add(5*time.Minute))
			if err != nil {
				log.Printf("calendar poll failed: %v", err)
				return
			}

			var current *calendar.Event
			for i := range events {
				if events[i].Start.Before(now) || events[i].Start.Equal(now) {
					current = &events[i]
					break
				}
			}

			select {
			case ch <- CalendarState{CurrentEvent: current}:
			default:
			}
		}

		poll()

		ticker := time.NewTicker(calendarPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				poll()
			}
		}
	}()

	return ch
}
