package diarize

import (
	"fmt"
	"log"
)

type DiarizeConfig struct {
	Backend     string // "pyannote" (default), "sherpa"
	NumSpeakers int    // 0 = auto-detect

	SegmentationModelPath string  // sherpa only
	EmbeddingModelPath    string  // sherpa only
	Threshold             float32 // sherpa only
}

type SpeakerSegment struct {
	Start   float64
	End     float64
	Speaker int
}

type Diarizer struct {
	diarize func(wavPath string) ([]SpeakerSegment, error)
	close   func()
}

func NewDiarizer(cfg DiarizeConfig) (*Diarizer, error) {
	switch cfg.Backend {
	case "pyannote":
		return newPyannoteDiarizer(cfg)
	case "sherpa":
		return newSherpaDiarizer(cfg)
	case "":
		d, err := newPyannoteDiarizer(cfg)
		if err != nil {
			log.Printf("pyannote not available (%v), falling back to sherpa", err)
			return newSherpaDiarizer(cfg)
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unknown diarize backend: %q", cfg.Backend)
	}
}

func (d *Diarizer) Diarize(wavPath string) ([]SpeakerSegment, error) {
	return d.diarize(wavPath)
}

func (d *Diarizer) DiarizeAudio(samples []float32, sampleRate int) ([]SpeakerSegment, error) {
	return nil, fmt.Errorf("DiarizeAudio not supported — use Diarize with a WAV file path")
}

func (d *Diarizer) Close() {
	if d.close != nil {
		d.close()
	}
}
