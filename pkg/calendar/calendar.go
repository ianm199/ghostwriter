package calendar

import "time"

type CalendarSource interface {
	Events(start, end time.Time) ([]Event, error)
	Close() error
}

type Event struct {
	ID         string
	Title      string
	Start      time.Time
	End        time.Time
	Attendees  []string
	MeetingURL string
}
