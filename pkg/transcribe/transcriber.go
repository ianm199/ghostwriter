package transcribe

import (
	"fmt"

	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
	"github.com/ianmclaughlin/ghostwriter/pkg/diarize"
)

type Transcriber interface {
	Transcribe(audio audiocapture.AudioData) (*Transcript, error)
	TranscribeFile(path string) (*Transcript, error)
	Close() error
}

type Backend string

const (
	BackendLocal      Backend = "local"
	BackendAssemblyAI Backend = "assemblyai"
	BackendOpenAI     Backend = "openai"
)

type TranscriberConfig struct {
	Backend       Backend
	Whisper       WhisperConfig
	APIKey        string
	Diarize       bool
	DiarizeConfig diarize.DiarizeConfig
}

func NewTranscriber(cfg TranscriberConfig) (Transcriber, error) {
	var t Transcriber

	switch cfg.Backend {
	case BackendLocal, "":
		wt, err := NewWhisperTranscriber(cfg.Whisper)
		if err != nil {
			return nil, err
		}
		t = wt
	case BackendAssemblyAI:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("ASSEMBLYAI_API_KEY environment variable is required for assemblyai backend")
		}
		t = NewAssemblyAITranscriber(cfg.APIKey)
	case BackendOpenAI:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required for openai backend")
		}
		t = NewOpenAITranscriber(cfg.APIKey)
	default:
		return nil, fmt.Errorf("unknown transcription backend: %q", cfg.Backend)
	}

	if cfg.Diarize {
		return NewDiarizingTranscriber(t, cfg.DiarizeConfig)
	}

	return t, nil
}
