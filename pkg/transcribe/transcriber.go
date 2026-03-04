// Package transcribe provides audio transcription via Whisper with structured
// output, filesystem storage, and full-text search.
package transcribe

import "github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"

type Transcriber interface {
	Transcribe(audio audiocapture.AudioData) (*Transcript, error)
	TranscribeFile(path string) (*Transcript, error)
	Close() error
}
