//go:build darwin

package audiocapture

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type BlackHoleRecorder struct {
	mu        sync.Mutex
	recording bool
	cmd       *exec.Cmd
	wavPath   string
}

func NewBlackHoleRecorder() *BlackHoleRecorder {
	return &BlackHoleRecorder{}
}

func (r *BlackHoleRecorder) Start(target CaptureTarget) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return fmt.Errorf("already recording")
	}

	deviceIndex, err := resolveBlackHoleDevice()
	if err != nil {
		return fmt.Errorf("failed to find BlackHole audio device: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "audiocapture-*.wav")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFile.Close()
	r.wavPath = tmpFile.Name()

	r.cmd = exec.Command("ffmpeg",
		"-f", "avfoundation",
		"-i", ":"+deviceIndex,
		"-ar", "16000",
		"-ac", "1",
		"-y",
		r.wavPath,
	)

	if err := r.cmd.Start(); err != nil {
		os.Remove(r.wavPath)
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	r.recording = true
	return nil
}

func (r *BlackHoleRecorder) Stop() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return "", fmt.Errorf("not recording")
	}

	r.cmd.Process.Signal(os.Interrupt)
	r.cmd.Wait()

	r.recording = false
	path := r.wavPath
	r.cmd = nil
	r.wavPath = ""

	info, err := os.Stat(path)
	if err != nil || info.Size() < 44 {
		os.Remove(path)
		return "", fmt.Errorf("capture produced no audio data")
	}

	return path, nil
}

func (r *BlackHoleRecorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

func resolveBlackHoleDevice() (string, error) {
	cmd := exec.Command("ffmpeg", "-f", "avfoundation", "-list_devices", "true", "-i", "")
	raw, _ := cmd.CombinedOutput()

	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	inAudio := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "AVFoundation audio devices:") {
			inAudio = true
			continue
		}
		if !inAudio {
			continue
		}
		if strings.Contains(line, "BlackHole") {
			firstClose := strings.Index(line, "]")
			if firstClose == -1 {
				continue
			}
			rest := line[firstClose+1:]
			start := strings.Index(rest, "[")
			end := strings.Index(rest, "]")
			if start != -1 && end != -1 && start < end {
				return strings.TrimSpace(rest[start+1 : end]), nil
			}
		}
	}

	return "", fmt.Errorf("BlackHole audio device not found — install BlackHole and configure an aggregate audio device")
}
