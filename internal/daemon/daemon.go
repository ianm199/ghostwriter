package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
	"github.com/ianmclaughlin/ghostwriter/pkg/calendar"
	"github.com/ianmclaughlin/ghostwriter/pkg/pidlock"
	"github.com/ianmclaughlin/ghostwriter/pkg/sysaware"
	"github.com/ianmclaughlin/ghostwriter/pkg/transcribe"
)

type State string

const (
	StateIdle       State = "idle"
	StateRecording  State = "recording"
	StateProcessing State = "processing"
)

type Daemon struct {
	state       State
	mu          sync.RWMutex
	procs       sysaware.ProcessChecker
	mic         sysaware.MicDetector
	capture     audiocapture.AudioRecorder
	transcriber transcribe.Transcriber
	store       *transcribe.Store
	socket      *Socket
	calendar    calendar.CalendarSource
	meeting     *ActiveMeeting
	saveAudio   bool
	done        chan struct{}
}

type ActiveMeeting struct {
	ID              string
	Title           string
	StartedAt       time.Time
	DetectedApp     string
	CalendarEventID string
	Attendees       []string
	Source          string
}

type Config struct {
	OutputDir            string
	ModelPath            string
	AudioBackend         string
	TranscriptionBackend string
	SaveAudio            bool
	Diarize              bool
	GoogleTokenPath      string
}

func New(cfg Config) (*Daemon, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	tcfg := transcribe.TranscriberConfig{
		Backend: transcribe.Backend(cfg.TranscriptionBackend),
		Whisper: transcribe.WhisperConfig{
			ModelPath:  cfg.ModelPath,
			MaxContext: 0,
		},
		Diarize: cfg.Diarize,
	}
	switch tcfg.Backend {
	case transcribe.BackendAssemblyAI:
		tcfg.APIKey = os.Getenv("ASSEMBLYAI_API_KEY")
	case transcribe.BackendOpenAI:
		tcfg.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	t, err := transcribe.NewTranscriber(tcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize transcriber: %w", err)
	}

	sock, err := NewSocket()
	if err != nil {
		return nil, fmt.Errorf("failed to create control socket: %w", err)
	}

	audioBackend := audiocapture.DetectBackend()
	if cfg.AudioBackend != "" {
		audioBackend = audiocapture.Backend(cfg.AudioBackend)
	}
	rec, err := audiocapture.NewAudioRecorder(audioBackend)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audio backend %q: %w", audioBackend, err)
	}
	log.Printf("audio backend: %s", audioBackend)

	var cal calendar.CalendarSource
	if cfg.GoogleTokenPath != "" {
		store := calendar.NewTokenStore(cfg.GoogleTokenPath)
		gc, err := calendar.NewGoogleCalendar(store, calendar.DefaultOAuthCredentials())
		if err != nil {
			log.Printf("calendar disabled: %v", err)
		} else {
			cal = gc
			log.Println("google calendar integration active")
		}
	}

	return &Daemon{
		state:       StateIdle,
		procs:       sysaware.NewDarwinProcessChecker(),
		mic:         sysaware.NewDarwinMicDetector(),
		capture:     rec,
		transcriber: t,
		store:       transcribe.NewStore(cfg.OutputDir),
		socket:      sock,
		calendar:    cal,
		saveAudio:   cfg.SaveAudio,
		done:        make(chan struct{}),
	}, nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pid := pidlock.New("ghostwriter")
	if err := pid.Check(); err != nil {
		return err
	}
	if err := pid.Acquire(); err != nil {
		return err
	}
	defer pid.Release()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	meetings := startMeetingDetector(ctx, d.procs, d.mic)
	commands := d.socket.Listen(ctx)

	var calendarEvents <-chan CalendarState
	var currentCalEvent *calendar.Event
	if d.calendar != nil {
		calendarEvents = startCalendarPoller(ctx, d.calendar)
	}

	log.Println("ghostwriter daemon started")

	for {
		select {
		case signal := <-meetings:
			d.handleMeetingSignal(signal, currentCalEvent)
		case calState := <-calendarEvents:
			currentCalEvent = calState.CurrentEvent
		case cmd := <-commands:
			d.handleCommand(cmd)
		case <-signals:
			log.Println("shutting down")
			if d.getState() == StateRecording {
				d.stopRecording()
			}
			return nil
		case <-d.done:
			log.Println("shutting down")
			if d.getState() == StateRecording {
				d.stopRecording()
			}
			return nil
		}
	}
}

func (d *Daemon) handleMeetingSignal(sig Signal, calEvent *calendar.Event) {
	switch sig.Type {
	case SignalStarted:
		if d.getState() == StateIdle {
			title := sig.App
			source := "detection"
			var calendarEventID string
			var attendees []string

			if calEvent != nil && calEvent.MeetingURL != "" {
				title = calEvent.Title
				calendarEventID = calEvent.ID
				attendees = calEvent.Attendees
				source = "calendar"
				log.Printf("enriching with calendar event: %s", calEvent.Title)
			}

			if err := d.startRecording(sig.App, title, source, calendarEventID, attendees); err != nil {
				log.Printf("auto-record failed: %v", err)
			}
		}
	case SignalEnded:
		if d.getState() == StateRecording {
			d.stopRecording()
		}
	}
}

func (d *Daemon) handleCommand(cmd Command) {
	switch cmd.Type {
	case CmdStartRecording:
		if d.getState() == StateIdle {
			if err := d.startRecording("", cmd.Title, "manual", "", nil); err != nil {
				cmd.Reply <- Response{OK: false, Error: err.Error()}
			} else {
				cmd.Reply <- Response{OK: true}
			}
		} else {
			cmd.Reply <- Response{OK: false, Error: "already recording"}
		}
	case CmdStopRecording:
		if d.getState() == StateRecording {
			d.stopRecording()
			cmd.Reply <- Response{OK: true}
		} else {
			cmd.Reply <- Response{OK: false, Error: "not recording"}
		}
	case CmdStatus:
		cmd.Reply <- d.statusResponse()
	case CmdListTranscripts:
		d.handleListTranscripts(cmd)
	case CmdGetTranscript:
		d.handleGetTranscript(cmd)
	case CmdListEvents:
		d.handleListEvents(cmd)
	case CmdStop:
		cmd.Reply <- Response{OK: true}
		close(d.done)
	}
}

func (d *Daemon) startRecording(app, title, source, calendarEventID string, attendees []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.capture.Start(audiocapture.CaptureTarget{AppName: app}); err != nil {
		return fmt.Errorf("failed to start capture: %w", err)
	}

	id := transcribe.GenerateID()
	d.meeting = &ActiveMeeting{
		ID:              id,
		Title:           title,
		StartedAt:       time.Now(),
		DetectedApp:     app,
		CalendarEventID: calendarEventID,
		Attendees:       attendees,
		Source:          source,
	}
	d.state = StateRecording
	log.Printf("recording started: %s", id)
	return nil
}

func (d *Daemon) stopRecording() {
	d.mu.Lock()
	meeting := d.meeting
	d.state = StateProcessing
	d.mu.Unlock()

	wavPath, err := d.capture.Stop()
	if err != nil {
		log.Printf("capture stop failed: %v", err)
		d.mu.Lock()
		d.state = StateIdle
		d.meeting = nil
		d.mu.Unlock()
		return
	}

	go func() {
		transcript, err := d.transcriber.TranscribeFile(wavPath)
		if err != nil {
			log.Printf("transcription failed: %v", err)
			os.Remove(wavPath)
			d.mu.Lock()
			d.state = StateIdle
			d.meeting = nil
			d.mu.Unlock()
			return
		}

		transcript.ID = meeting.ID
		transcript.Metadata.Title = meeting.Title
		transcript.Metadata.Date = meeting.StartedAt
		transcript.Metadata.DurationSeconds = int(time.Since(meeting.StartedAt).Seconds())
		transcript.Metadata.Source = meeting.Source
		transcript.Metadata.DetectedApp = meeting.DetectedApp
		transcript.Metadata.CalendarEventID = meeting.CalendarEventID
		transcript.Metadata.Attendees = meeting.Attendees

		if err := d.store.Write(transcript); err != nil {
			log.Printf("failed to write transcript: %v", err)
		} else {
			log.Printf("transcript written: %s", meeting.ID)
		}

		if d.saveAudio {
			audioDest := d.store.AudioPath(transcript)
			if err := copyFile(wavPath, audioDest); err != nil {
				log.Printf("failed to save audio: %v", err)
			} else {
				log.Printf("audio saved: %s", audioDest)
			}
		}
		os.Remove(wavPath)

		d.mu.Lock()
		d.state = StateIdle
		d.meeting = nil
		d.mu.Unlock()
	}()
}

func (d *Daemon) handleListTranscripts(cmd Command) {
	limit := cmd.Limit
	if limit <= 0 {
		limit = 20
	}

	transcripts, err := d.store.List(time.Time{})
	if err != nil {
		cmd.Reply <- Response{OK: false, Error: err.Error()}
		return
	}

	if len(transcripts) > limit {
		transcripts = transcripts[:limit]
	}

	summaries := make([]TranscriptSummary, len(transcripts))
	for i, t := range transcripts {
		summaries[i] = TranscriptSummary{
			ID:              t.ID,
			Title:           t.Metadata.Title,
			Date:            t.Metadata.Date.Format(time.RFC3339),
			DurationSeconds: t.Metadata.DurationSeconds,
			Source:          t.Metadata.Source,
		}
	}

	cmd.Reply <- Response{OK: true, Transcripts: summaries}
}

func (d *Daemon) handleGetTranscript(cmd Command) {
	t, err := d.store.Get(cmd.ID)
	if err != nil {
		cmd.Reply <- Response{OK: false, Error: err.Error()}
		return
	}

	detail := &TranscriptDetail{
		ID:              t.ID,
		Title:           t.Metadata.Title,
		Date:            t.Metadata.Date.Format(time.RFC3339),
		DurationSeconds: t.Metadata.DurationSeconds,
		FullText:        t.FullText,
		Source:          t.Metadata.Source,
	}

	cmd.Reply <- Response{OK: true, Transcript: detail}
}

func (d *Daemon) handleListEvents(cmd Command) {
	if d.calendar == nil {
		cmd.Reply <- Response{OK: true, Events: []EventInfo{}}
		return
	}

	now := time.Now()
	events, err := d.calendar.Events(now, now.Add(24*time.Hour))
	if err != nil {
		cmd.Reply <- Response{OK: false, Error: err.Error()}
		return
	}

	infos := make([]EventInfo, len(events))
	for i, e := range events {
		infos[i] = EventInfo{
			ID:         e.ID,
			Title:      e.Title,
			Start:      e.Start.Format(time.RFC3339),
			End:        e.End.Format(time.RFC3339),
			MeetingURL: e.MeetingURL,
		}
	}

	cmd.Reply <- Response{OK: true, Events: infos}
}

func (d *Daemon) getState() State {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

func (d *Daemon) statusResponse() Response {
	d.mu.RLock()
	defer d.mu.RUnlock()

	resp := Response{OK: true, Status: &StatusInfo{State: d.state}}
	if d.meeting != nil {
		resp.Status.CurrentMeeting = d.meeting.Title
		resp.Status.Duration = time.Since(d.meeting.StartedAt).Round(time.Second).String()
	}
	return resp
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

