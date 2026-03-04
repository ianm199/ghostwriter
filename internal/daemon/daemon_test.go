package daemon_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
	"github.com/ianmclaughlin/ghostwriter/internal/detect"
	"github.com/ianmclaughlin/ghostwriter/internal/output"
	"github.com/ianmclaughlin/ghostwriter/internal/testutil"
)

type testEnv struct {
	Daemon      *daemon.Daemon
	Capturer    *testutil.FakeCapturer
	Transcriber *testutil.FakeTranscriber
	Detector    *testutil.FakeDetector
	Store       *output.Store
	SocketPath  string
	Cancel      context.CancelFunc
}

// newClient creates a fresh socket client for a single command.
func (e *testEnv) newClient(t *testing.T) *daemon.Client {
	t.Helper()
	c, err := daemon.NewClientAt(e.SocketPath)
	if err != nil {
		t.Fatalf("failed to connect to daemon socket: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// startTestDaemon creates and starts a daemon with fakes.
func startTestDaemon(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	outputDir := filepath.Join(tmpDir, "transcripts")

	fc := &testutil.FakeCapturer{}
	ft := &testutil.FakeTranscriber{Result: testutil.SampleTranscript()}
	fd := testutil.NewFakeDetector()
	store := output.NewStore(outputDir)

	d, err := daemon.New(daemon.Config{
		Detector:    fd,
		Capture:     fc,
		Transcriber: ft,
		Store:       store,
		SocketPath:  socketPath,
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		d.Run(ctx)
	}()

	// Wait for socket to be ready
	for i := 0; i < 100; i++ {
		c, cerr := daemon.NewClientAt(socketPath)
		if cerr == nil {
			c.Close()
			break
		}
		if i == 99 {
			cancel()
			t.Fatalf("daemon socket never became ready: %v", cerr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	env := &testEnv{
		Daemon:      d,
		Capturer:    fc,
		Transcriber: ft,
		Detector:    fd,
		Store:       store,
		SocketPath:  socketPath,
		Cancel:      cancel,
	}
	t.Cleanup(func() {
		cancel()
		// Wait for daemon to fully drain all goroutines
		// so temp dir cleanup doesn't race with transcript writes
		time.Sleep(100 * time.Millisecond)
	})
	return env
}

// Test 1: Full Recording Pipeline (Manual Trigger)
func TestFullRecordingPipeline(t *testing.T) {
	env := startTestDaemon(t)

	// Start recording
	c := env.newClient(t)
	err := c.StartRecording("Test Meeting")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	if env.Daemon.GetState() != daemon.StateRecording {
		t.Errorf("expected StateRecording, got %s", env.Daemon.GetState())
	}
	if env.Capturer.CallCount("start") != 1 {
		t.Errorf("expected 1 Start call, got %d", env.Capturer.CallCount("start"))
	}

	// Stop recording (new client — socket is one-shot)
	c2 := env.newClient(t)
	err = c2.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	if env.Capturer.CallCount("stop") != 1 {
		t.Errorf("expected 1 Stop call, got %d", env.Capturer.CallCount("stop"))
	}

	// Wait for async transcription
	if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
		t.Fatalf("daemon did not return to idle: %v", err)
	}

	if env.Transcriber.CallCount() != 1 {
		t.Errorf("expected 1 TranscribeFile call, got %d", env.Transcriber.CallCount())
	}

	// Verify transcript was written
	transcripts, err := env.Store.List(time.Time{})
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("expected 1 transcript, got %d", len(transcripts))
	}
	if transcripts[0].Metadata.Title != "Test Meeting" {
		t.Errorf("expected title 'Test Meeting', got %q", transcripts[0].Metadata.Title)
	}
	if transcripts[0].FullText == "" {
		t.Error("expected non-empty FullText")
	}
}

// Test 2: Auto-Detection Pipeline
func TestAutoDetectionPipeline(t *testing.T) {
	env := startTestDaemon(t)

	// Emit meeting started signal
	env.Detector.Emit(detect.Signal{Type: detect.SignalStarted, App: "zoom.us"})

	// Wait for recording to start
	deadline := time.After(2 * time.Second)
	for env.Daemon.GetState() != daemon.StateRecording {
		select {
		case <-deadline:
			t.Fatalf("daemon did not start recording after SignalStarted")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if env.Capturer.CallCount("start") != 1 {
		t.Errorf("expected 1 Start call, got %d", env.Capturer.CallCount("start"))
	}

	// Emit meeting ended signal
	env.Detector.Emit(detect.Signal{Type: detect.SignalEnded, App: "zoom.us"})

	// Wait for idle
	if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
		t.Fatalf("daemon did not return to idle: %v", err)
	}

	if env.Capturer.CallCount("stop") != 1 {
		t.Errorf("expected 1 Stop call, got %d", env.Capturer.CallCount("stop"))
	}
	if env.Transcriber.CallCount() != 1 {
		t.Errorf("expected 1 TranscribeFile call, got %d", env.Transcriber.CallCount())
	}

	// Verify transcript written
	transcripts, err := env.Store.List(time.Time{})
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("expected 1 transcript, got %d", len(transcripts))
	}
}

// Test 3: Status Reporting
func TestStatusReporting(t *testing.T) {
	env := startTestDaemon(t)

	// Idle status
	c := env.newClient(t)
	status, err := c.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.State != daemon.StateIdle {
		t.Errorf("expected idle, got %s", status.State)
	}

	// Start recording
	c2 := env.newClient(t)
	err = c2.StartRecording("Status Test")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	// Check state directly
	if env.Daemon.GetState() != daemon.StateRecording {
		t.Errorf("expected StateRecording, got %s", env.Daemon.GetState())
	}

	// Status via new client
	c3 := env.newClient(t)
	status, err = c3.Status()
	if err != nil {
		t.Fatalf("Status after start failed: %v", err)
	}
	if status.State != daemon.StateRecording {
		t.Errorf("expected recording, got %s", status.State)
	}
	if status.CurrentMeeting != "Status Test" {
		t.Errorf("expected 'Status Test', got %q", status.CurrentMeeting)
	}
	if status.Duration == "" {
		t.Error("expected non-empty duration")
	}
}

// Test 4: Concurrent Command Rejection
func TestConcurrentCommandRejection(t *testing.T) {
	env := startTestDaemon(t)

	// Start recording
	c := env.newClient(t)
	err := c.StartRecording("Meeting 1")
	if err != nil {
		t.Fatalf("first StartRecording failed: %v", err)
	}

	// Try to start again — should fail
	c2 := env.newClient(t)
	err = c2.StartRecording("Meeting 2")
	if err == nil {
		t.Fatal("expected error for second StartRecording, got nil")
	}

	// Stop recording
	c3 := env.newClient(t)
	err = c3.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}
	if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
		t.Fatalf("daemon did not return to idle: %v", err)
	}

	// Try to stop when idle — should fail
	c4 := env.newClient(t)
	err = c4.StopRecording()
	if err == nil {
		t.Fatal("expected error for StopRecording when idle, got nil")
	}
}

// Test 5: Capture Failure — Start Error
func TestCaptureStartFailure(t *testing.T) {
	env := startTestDaemon(t)
	env.Capturer.StartErr = fmt.Errorf("audio device not found")

	c := env.newClient(t)
	err := c.StartRecording("Failing Meeting")
	if err == nil {
		t.Fatal("expected error from StartRecording, got nil")
	}

	if env.Daemon.GetState() != daemon.StateIdle {
		t.Errorf("expected state to remain idle, got %s", env.Daemon.GetState())
	}
}

// Test 6: Capture Failure — Stop Error
func TestCaptureStopFailure(t *testing.T) {
	env := startTestDaemon(t)

	c := env.newClient(t)
	err := c.StartRecording("Meeting")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	// Set stop to fail
	env.Capturer.StopErr = fmt.Errorf("capture produced no audio data")

	c2 := env.newClient(t)
	err = c2.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording IPC failed: %v", err)
	}

	// Wait for daemon to recover to idle
	if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
		t.Fatalf("daemon did not recover to idle: %v", err)
	}

	// Transcription should NOT have been called
	if env.Transcriber.CallCount() != 0 {
		t.Errorf("expected 0 TranscribeFile calls, got %d", env.Transcriber.CallCount())
	}
}

// Test 7: Transcription Failure and Recovery
func TestTranscriptionFailure(t *testing.T) {
	env := startTestDaemon(t)
	env.Transcriber.Err = fmt.Errorf("whisper-cli crashed")
	env.Transcriber.Result = nil

	c := env.newClient(t)
	err := c.StartRecording("Meeting")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	c2 := env.newClient(t)
	err = c2.StopRecording()
	if err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	// Wait for daemon to recover
	if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
		t.Fatalf("daemon did not recover to idle: %v", err)
	}

	// No transcript should have been written
	transcripts, err := env.Store.List(time.Time{})
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(transcripts) != 0 {
		t.Errorf("expected 0 transcripts after failure, got %d", len(transcripts))
	}

	// Daemon should be able to record again
	env.Transcriber.Err = nil
	env.Transcriber.Result = testutil.SampleTranscript()

	c3 := env.newClient(t)
	err = c3.StartRecording("Retry Meeting")
	if err != nil {
		t.Fatalf("second StartRecording failed: %v", err)
	}
	c4 := env.newClient(t)
	err = c4.StopRecording()
	if err != nil {
		t.Fatalf("second StopRecording failed: %v", err)
	}

	if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
		t.Fatalf("daemon did not return to idle after retry: %v", err)
	}

	transcripts, err = env.Store.List(time.Time{})
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(transcripts) != 1 {
		t.Errorf("expected 1 transcript after retry, got %d", len(transcripts))
	}
}

// Test 8: Graceful Shutdown During Recording
func TestGracefulShutdownDuringRecording(t *testing.T) {
	env := startTestDaemon(t)

	c := env.newClient(t)
	err := c.StartRecording("In-Progress Meeting")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	if env.Daemon.GetState() != daemon.StateRecording {
		t.Fatalf("expected recording, got %s", env.Daemon.GetState())
	}

	// Cancel context (simulates SIGTERM)
	env.Cancel()

	// Give the daemon time to shut down and process
	time.Sleep(500 * time.Millisecond)

	// Capture should have been stopped
	if env.Capturer.CallCount("stop") < 1 {
		t.Errorf("expected Stop to be called during shutdown, got %d calls", env.Capturer.CallCount("stop"))
	}

	// Transcription should have run
	if env.Transcriber.CallCount() < 1 {
		t.Errorf("expected TranscribeFile to be called during shutdown, got %d calls", env.Transcriber.CallCount())
	}

	// Transcript should have been written
	transcripts, err := env.Store.List(time.Time{})
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(transcripts) != 1 {
		t.Errorf("expected 1 transcript after graceful shutdown, got %d", len(transcripts))
	}
}

// Test 9: Detection Signal Debouncing (no double start)
func TestDetectionSignalDebouncing(t *testing.T) {
	env := startTestDaemon(t)

	// Emit first meeting start
	env.Detector.Emit(detect.Signal{Type: detect.SignalStarted, App: "zoom.us"})

	deadline := time.After(2 * time.Second)
	for env.Daemon.GetState() != daemon.StateRecording {
		select {
		case <-deadline:
			t.Fatalf("daemon did not start recording")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Emit a second start while already recording — should be ignored
	env.Detector.Emit(detect.Signal{Type: detect.SignalStarted, App: "Microsoft Teams"})
	time.Sleep(100 * time.Millisecond)

	if env.Capturer.CallCount("start") != 1 {
		t.Errorf("expected exactly 1 Start call (second should be ignored), got %d", env.Capturer.CallCount("start"))
	}

	// Stop the recording to clean up before test exits
	env.Detector.Emit(detect.Signal{Type: detect.SignalEnded, App: "zoom.us"})
	env.Daemon.WaitForIdle(5 * time.Second)
}

// Test 10: Multiple Recording Cycles
func TestMultipleRecordingCycles(t *testing.T) {
	env := startTestDaemon(t)

	for i := 0; i < 3; i++ {
		title := fmt.Sprintf("Meeting %d", i+1)

		c := env.newClient(t)
		err := c.StartRecording(title)
		if err != nil {
			t.Fatalf("cycle %d: StartRecording failed: %v", i, err)
		}

		c2 := env.newClient(t)
		err = c2.StopRecording()
		if err != nil {
			t.Fatalf("cycle %d: StopRecording failed: %v", i, err)
		}

		if err := env.Daemon.WaitForIdle(5 * time.Second); err != nil {
			t.Fatalf("cycle %d: daemon did not return to idle: %v", i, err)
		}
	}

	if env.Capturer.CallCount("start") != 3 {
		t.Errorf("expected 3 Start calls, got %d", env.Capturer.CallCount("start"))
	}
	if env.Capturer.CallCount("stop") != 3 {
		t.Errorf("expected 3 Stop calls, got %d", env.Capturer.CallCount("stop"))
	}
	if env.Transcriber.CallCount() != 3 {
		t.Errorf("expected 3 TranscribeFile calls, got %d", env.Transcriber.CallCount())
	}

	transcripts, err := env.Store.List(time.Time{})
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(transcripts) != 3 {
		t.Errorf("expected 3 transcripts, got %d", len(transcripts))
	}
}

// Test 11: Stop via CmdStop (context cancellation, not os.Exit)
func TestStopCommand(t *testing.T) {
	env := startTestDaemon(t)

	c := env.newClient(t)
	err := c.Stop()
	if err != nil {
		t.Fatalf("Stop command failed: %v", err)
	}

	// If we get here, the test process didn't crash — os.Exit is gone.
	time.Sleep(100 * time.Millisecond)
}
