package transcribe

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

type Transcript struct {
	Version  string    `json:"version"`
	ID       string    `json:"id"`
	Metadata Metadata  `json:"metadata"`
	Speakers []Speaker `json:"speakers"`
	Segments []Segment `json:"segments"`
	FullText string    `json:"full_text"`
}

type Metadata struct {
	Date            time.Time `json:"date"`
	DurationSeconds int       `json:"duration_seconds"`
	Title           string    `json:"title,omitempty"`
	Source          string    `json:"source"`
	DetectedApp     string    `json:"detected_app,omitempty"`
	CalendarEventID string    `json:"calendar_event_id,omitempty"`
	Model           string    `json:"model"`
	Language        string    `json:"language"`
	Attendees       []string  `json:"attendees,omitempty"`
}

type Speaker struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Segment struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Speaker    string  `json:"speaker"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Words      []Word  `json:"words,omitempty"`
}

type Word struct {
	Word       string  `json:"word"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
}

func (t *Transcript) FormatText() string {
	var b strings.Builder

	if t.Metadata.Title != "" {
		b.WriteString(t.Metadata.Title)
		b.WriteString("\n")
		b.WriteString(t.Metadata.Date.Format("January 2, 2006 3:04 PM"))
		b.WriteString("\n\n")
	}

	lastSpeaker := ""
	for _, seg := range t.Segments {
		if seg.Speaker != lastSpeaker {
			if lastSpeaker != "" {
				b.WriteString("\n")
			}
			b.WriteString(seg.Speaker)
			b.WriteString(":\n")
			lastSpeaker = seg.Speaker
		}
		b.WriteString(strings.TrimSpace(seg.Text))
		b.WriteString(" ")
	}
	b.WriteString("\n")
	return b.String()
}

func GenerateID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", time.Now().Format("20060102-150405"), b)
}
