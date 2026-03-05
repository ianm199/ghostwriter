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
	state    State
	mu       sync.RWMutex
	procs    sysaware.ProcessChecker
	mic      sysaware.MicDetector
	capture  audiocapture.AudioRecorder
	whisper  transcribe.Transcriber
	store    *transcribe.Store
	socket   *Socket
	meeting  *ActiveMeeting
	done     chan struct{}
}

type ActiveMeeting struct {
	ID        string
	Title     string
	StartedAt time.Time
}

type Config struct {
	OutputDir    string
	ModelPath    string
	AudioBackend string
}

func New(cfg Config) (*Daemon, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	w, err := transcribe.NewWhisperTranscriber(transcribe.WhisperConfig{
		ModelPath: cfg.ModelPath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize whisper: %w", err)
	}

	sock, err := NewSocket()
	if err != nil {
		return nil, fmt.Errorf("failed to create control socket: %w", err)
	}

	backend := audiocapture.DetectBackend()
	if cfg.AudioBackend != "" {
		backend = audiocapture.Backend(cfg.AudioBackend)
	}
	rec, err := audiocapture.NewAudioRecorder(backend)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audio backend %q: %w", backend, err)
	}
	log.Printf("audio backend: %s", backend)

	return &Daemon{
		state:   StateIdle,
		procs:   sysaware.NewDarwinProcessChecker(),
		mic:     sysaware.NewDarwinMicDetector(),
		capture: rec,
		whisper: w,
		store:   transcribe.NewStore(cfg.OutputDir),
		socket:  sock,
		done:    make(chan struct{}),
	}, nil
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := writePIDFile(); err != nil {
		return err
	}
	defer removePIDFile()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	meetings := startMeetingDetector(ctx, d.procs, d.mic)
	commands := d.socket.Listen(ctx)

	log.Println("ghostwriter daemon started")

	for {
		select {
		case signal := <-meetings:
			d.handleMeetingSignal(signal)
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

func (d *Daemon) handleMeetingSignal(sig Signal) {
	switch sig.Type {
	case SignalStarted:
		if d.getState() == StateIdle {
			if err := d.startRecording(sig.App, sig.App); err != nil {
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
			if err := d.startRecording("", cmd.Title); err != nil {
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
	case CmdStop:
		cmd.Reply <- Response{OK: true}
		close(d.done)
	}
}

func (d *Daemon) startRecording(app, title string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.capture.Start(audiocapture.CaptureTarget{AppName: app}); err != nil {
		return fmt.Errorf("failed to start capture: %w", err)
	}

	id := transcribe.GenerateID()
	d.meeting = &ActiveMeeting{
		ID:        id,
		Title:     title,
		StartedAt: time.Now(),
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
		defer os.Remove(wavPath)

		transcript, err := d.whisper.TranscribeFile(wavPath)
		if err != nil {
			log.Printf("transcription failed: %v", err)
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

		if err := d.store.Write(transcript); err != nil {
			log.Printf("failed to write transcript: %v", err)
		} else {
			log.Printf("transcript written: %s", meeting.ID)
		}

		d.mu.Lock()
		d.state = StateIdle
		d.meeting = nil
		d.mu.Unlock()
	}()
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

func pidFilePath() string {
	return filepath.Join(os.TempDir(), "ghostwriter.pid")
}

func writePIDFile() error {
	return os.WriteFile(pidFilePath(), []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

func removePIDFile() {
	os.Remove(pidFilePath())
}
