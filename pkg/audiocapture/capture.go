//go:build darwin

package audiocapture

import (
	"errors"
	"fmt"
)

var (
	errSCKitStartFailed = errors.New("sckit: failed to start capture")
	errSCKitNoAudio     = errors.New("sckit: capture produced no audio data")
)

type CaptureTarget struct {
	AppName string
}

type AudioRecorder interface {
	Start(target CaptureTarget) error
	Stop() (string, error)
	IsRecording() bool
}

type AudioData struct {
	Samples    []float32
	SampleRate int
	Channels   int
}

type Backend string

const (
	BackendSCKit     Backend = "sckit"
	BackendBlackHole Backend = "blackhole"
)

func NewAudioRecorder(backend Backend) (AudioRecorder, error) {
	switch backend {
	case BackendSCKit:
		return NewSCKitRecorder()
	case BackendBlackHole:
		return NewBlackHoleRecorder(), nil
	default:
		return nil, fmt.Errorf("unknown audio backend: %q", backend)
	}
}

func DetectBackend() Backend {
	if SCKitIsAvailable() && SCKitHasPermission() {
		return BackendSCKit
	}
	return BackendBlackHole
}
