package capture

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type AudioData struct {
	Samples    []float32
	SampleRate int
	Channels   int
}

type Capture struct {
	mu        sync.Mutex
	recording bool
	cmd       *exec.Cmd
	wavPath   string
}

func New() *Capture {
	return &Capture{}
}

func (c *Capture) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.recording {
		return fmt.Errorf("already recording")
	}

	deviceIndex, err := resolveBlackHoleDevice()
	if err != nil {
		return fmt.Errorf("failed to find BlackHole audio device: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "ghostwriter-capture-*.wav")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFile.Close()
	c.wavPath = tmpFile.Name()

	c.cmd = exec.Command("ffmpeg",
		"-f", "avfoundation",
		"-i", ":"+deviceIndex,
		"-ar", "16000",
		"-ac", "1",
		"-y",
		c.wavPath,
	)

	if err := c.cmd.Start(); err != nil {
		os.Remove(c.wavPath)
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	c.recording = true
	return nil
}

func (c *Capture) Stop() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.recording {
		return "", fmt.Errorf("not recording")
	}

	c.cmd.Process.Signal(os.Interrupt)
	c.cmd.Wait()

	c.recording = false
	path := c.wavPath
	c.cmd = nil
	c.wavPath = ""

	info, err := os.Stat(path)
	if err != nil || info.Size() < 44 {
		os.Remove(path)
		return "", fmt.Errorf("capture produced no audio data")
	}

	return path, nil
}

func (c *Capture) IsRecording() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recording
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
