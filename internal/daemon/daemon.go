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

	"github.com/ianmclaughlin/ghostwriter/internal/capture"
	"github.com/ianmclaughlin/ghostwriter/internal/detect"
	"github.com/ianmclaughlin/ghostwriter/internal/output"
	"github.com/ianmclaughlin/ghostwriter/internal/transcribe"
)

type State string

const (
	StateIdle       State = "idle"
	StateRecording  State = "recording"
	StateProcessing State = "processing"
)

// Config holds all injectable dependencies for the daemon.
type Config struct {
	Detector    detect.MeetingDetector
	Capture     capture.AudioCapturer
	Transcriber transcribe.Transcriber
	Store       *output.Store
	SocketPath  string
	OutputDir   string
}

type Daemon struct {
	state      State
	mu         sync.RWMutex
	detector   detect.MeetingDetector
	capture    capture.AudioCapturer
	whisper    transcribe.Transcriber
	store      *output.Store
	socket     *Socket
	meeting    *ActiveMeeting
	cancel     context.CancelFunc // for CmdStop to trigger shutdown
	done       chan struct{}       // closed when transcription goroutines finish
	wg         sync.WaitGroup     // tracks in-flight transcription goroutines
}

type ActiveMeeting struct {
	ID        string
	Title     string
	StartedAt time.Time
}

// New creates a daemon from a Config with injected dependencies.
// All fields in Config are optional — nil values get production defaults.
func New(cfg Config) (*Daemon, error) {
	if cfg.OutputDir == "" {
		cfg.OutputDir = output.DefaultOutputDir()
	}
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	if cfg.Transcriber == nil {
		w, err := transcribe.NewWhisperTranscriber(transcribe.WhisperConfig{})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize whisper: %w", err)
		}
		cfg.Transcriber = w
	}

	if cfg.Detector == nil {
		cfg.Detector = detect.New()
	}

	if cfg.Capture == nil {
		cfg.Capture = capture.New()
	}

	if cfg.Store == nil {
		cfg.Store = output.NewStore(cfg.OutputDir)
	}

	sock, err := NewSocketAt(cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create control socket: %w", err)
	}

	return &Daemon{
		state:    StateIdle,
		detector: cfg.Detector,
		capture:  cfg.Capture,
		whisper:  cfg.Transcriber,
		store:    cfg.Store,
		socket:   sock,
		done:     make(chan struct{}),
	}, nil
}

// Run starts the daemon and blocks until shutdown.
// It accepts a context for cancellation — when ctx is done, the daemon shuts down.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer cancel()

	if err := writePIDFile(); err != nil {
		return err
	}
	defer removePIDFile()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	meetings := d.detector.Start(ctx)
	commands := d.socket.Listen(ctx)

	log.Println("ghostwriter daemon started")

	for {
		select {
		case sig, ok := <-meetings:
			if !ok {
				meetings = nil
				continue
			}
			d.handleMeetingSignal(sig)
		case cmd, ok := <-commands:
			if !ok {
				commands = nil
				continue
			}
			d.handleCommand(cmd)
		case <-sigs:
			log.Println("shutting down")
			d.shutdownRecording()
			return nil
		case <-ctx.Done():
			log.Println("context cancelled, shutting down")
			d.shutdownRecording()
			return nil
		}
	}
}

func (d *Daemon) handleMeetingSignal(sig detect.Signal) {
	switch sig.Type {
	case detect.SignalStarted:
		if d.getState() == StateIdle {
			if err := d.startRecording(sig.App); err != nil {
				log.Printf("auto-record failed: %v", err)
			}
		}
	case detect.SignalEnded:
		if d.getState() == StateRecording {
			d.stopRecording()
		}
	}
}

func (d *Daemon) handleCommand(cmd Command) {
	switch cmd.Type {
	case CmdStartRecording:
		if d.getState() == StateIdle {
			if err := d.startRecording(cmd.Title); err != nil {
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
		if d.getState() == StateRecording {
			d.stopRecording()
		}
		cmd.Reply <- Response{OK: true}
		// Cancel the run context to trigger graceful shutdown
		if d.cancel != nil {
			d.cancel()
		}
	}
}

func (d *Daemon) startRecording(title string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.capture.Start(); err != nil {
		return fmt.Errorf("failed to start capture: %w", err)
	}

	id := output.GenerateID()
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

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
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

// shutdownRecording stops any active recording and waits for transcription to finish.
func (d *Daemon) shutdownRecording() {
	if d.getState() == StateRecording {
		d.stopRecording()
	}
	d.wg.Wait()
}

// WaitForIdle blocks until the daemon returns to idle state.
// Useful in tests to wait for async transcription to complete.
func (d *Daemon) WaitForIdle(timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		if d.getState() == StateIdle {
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("timed out waiting for idle state (current: %s)", d.getState())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (d *Daemon) getState() State {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// GetState returns the current daemon state (exported for tests).
func (d *Daemon) GetState() State {
	return d.getState()
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
