package diarize

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func newPyannoteDiarizer(cfg DiarizeConfig) (*Diarizer, error) {
	scriptPath, err := findPyannoteScript()
	if err != nil {
		return nil, err
	}

	python, err := exec.LookPath("python3.11")
	if err != nil {
		python, err = exec.LookPath("python3")
		if err != nil {
			return nil, fmt.Errorf("python3 not found in PATH")
		}
	}

	return &Diarizer{
		diarize: func(wavPath string) ([]SpeakerSegment, error) {
			return runPyannote(python, scriptPath, wavPath, cfg.NumSpeakers)
		},
		close: func() {},
	}, nil
}

type pyannoteOutput struct {
	Segments    []pyannoteSegment `json:"segments"`
	NumSpeakers int               `json:"num_speakers"`
}

type pyannoteSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
}

func runPyannote(python, scriptPath, wavPath string, numSpeakers int) ([]SpeakerSegment, error) {
	args := []string{scriptPath, wavPath}
	if numSpeakers > 0 {
		args = append(args, "--num-speakers", fmt.Sprintf("%d", numSpeakers))
	}

	cmd := exec.Command(python, args...)
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pyannote-diarize failed: %w", err)
	}

	var result pyannoteOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse pyannote output: %w", err)
	}

	segments := make([]SpeakerSegment, len(result.Segments))
	for i, s := range result.Segments {
		segments[i] = SpeakerSegment{
			Start:   s.Start,
			End:     s.End,
			Speaker: s.Speaker,
		}
	}
	return segments, nil
}

func findPyannoteScript() (string, error) {
	candidates := []string{}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "scripts", "pyannote-diarize.py"),
			filepath.Join(dir, "..", "scripts", "pyannote-diarize.py"),
		)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	if thisFile != "" {
		projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
		candidates = append(candidates, filepath.Join(projectRoot, "scripts", "pyannote-diarize.py"))
	}

	candidates = append(candidates, "scripts/pyannote-diarize.py")

	for _, path := range candidates {
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}

	return "", fmt.Errorf("pyannote-diarize.py not found — install pyannote.audio and ensure scripts/pyannote-diarize.py is present")
}
