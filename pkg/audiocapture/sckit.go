//go:build darwin

package audiocapture

import (
	"fmt"
	"os"
	"sync"
)

type SCKitRecorder struct {
	mu        sync.Mutex
	recording bool
}

func NewSCKitRecorder() (*SCKitRecorder, error) {
	scKitEnsureAppInit()
	if !SCKitIsAvailable() {
		return nil, fmt.Errorf("ScreenCaptureKit is not available on this system (requires macOS 12.3+)")
	}
	if !SCKitHasPermission() {
		return nil, fmt.Errorf("screen recording permission not granted — open System Settings > Privacy & Security > Screen Recording")
	}
	return &SCKitRecorder{}, nil
}

func (r *SCKitRecorder) Start(target CaptureTarget) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return fmt.Errorf("already recording")
	}

	if err := scKitStartCapture(target.AppName); err != nil {
		return fmt.Errorf("failed to start sckit capture: %w", err)
	}

	r.recording = true
	return nil
}

func (r *SCKitRecorder) Stop() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return "", fmt.Errorf("not recording")
	}
	r.recording = false

	data, err := scKitStopCapture()
	if err != nil {
		return "", fmt.Errorf("sckit capture failed: %w", err)
	}

	resampled := Resample48kStereoTo16kMono(data.Samples, data.Channels)

	tmpFile, err := os.CreateTemp("", "audiocapture-*.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFile.Close()

	if err := writeWAVFromInt16(tmpFile.Name(), resampled, 16000); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write WAV: %w", err)
	}

	return tmpFile.Name(), nil
}

func (r *SCKitRecorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

func SCKitIsAvailable() bool {
	return scKitIsAvailable()
}

func SCKitHasPermission() bool {
	return scKitHasPermission()
}

func EnsureAppInit() {
	scKitEnsureAppInit()
}

func RunMainLoop() {
	scKitRunMainLoop()
}

func QuitMainLoop() {
	scKitQuitMainLoop()
}
