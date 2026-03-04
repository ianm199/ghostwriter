package transcribe

import (
	"github.com/ianmclaughlin/ghostwriter/internal/output"
	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
)

type Transcriber interface {
	Transcribe(audio audiocapture.AudioData) (*output.Transcript, error)
	TranscribeFile(path string) (*output.Transcript, error)
	Close() error
}
