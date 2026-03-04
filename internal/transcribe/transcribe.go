package transcribe

import (
	"github.com/ianmclaughlin/ghostwriter/internal/capture"
	"github.com/ianmclaughlin/ghostwriter/internal/output"
)

type Transcriber interface {
	Transcribe(audio capture.AudioData) (*output.Transcript, error)
	TranscribeFile(path string) (*output.Transcript, error)
	Close() error
}
