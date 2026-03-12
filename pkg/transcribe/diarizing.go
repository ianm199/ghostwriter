package transcribe

import (
	"fmt"
	"os"
	"sort"

	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
	"github.com/ianmclaughlin/ghostwriter/pkg/diarize"
)

type DiarizingTranscriber struct {
	inner    Transcriber
	diarizer *diarize.Diarizer
}

func NewDiarizingTranscriber(inner Transcriber, cfg diarize.DiarizeConfig) (*DiarizingTranscriber, error) {
	d, err := diarize.NewDiarizer(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize diarizer: %w", err)
	}
	return &DiarizingTranscriber{inner: inner, diarizer: d}, nil
}

func (dt *DiarizingTranscriber) Transcribe(audio audiocapture.AudioData) (*Transcript, error) {
	tmpFile, err := os.CreateTemp("", "diarize-*.wav")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp WAV: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := writeWAV(tmpFile, audio.Samples, audio.SampleRate); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write WAV: %w", err)
	}
	tmpFile.Close()

	transcript, err := dt.inner.TranscribeFile(tmpPath)
	if err != nil {
		return nil, err
	}

	segments, err := dt.diarizer.Diarize(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("diarization failed: %w", err)
	}

	mergeTranscript(transcript, segments)
	return transcript, nil
}

func (dt *DiarizingTranscriber) TranscribeFile(path string) (*Transcript, error) {
	transcript, err := dt.inner.TranscribeFile(path)
	if err != nil {
		return nil, err
	}

	segments, err := dt.diarizer.Diarize(path)
	if err != nil {
		return nil, fmt.Errorf("diarization failed: %w", err)
	}

	mergeTranscript(transcript, segments)
	return transcript, nil
}

func (dt *DiarizingTranscriber) Close() error {
	dt.diarizer.Close()
	return dt.inner.Close()
}

func mergeTranscript(transcript *Transcript, segments []diarize.SpeakerSegment) {
	speakerSet := make(map[int]bool)

	for i := range transcript.Segments {
		seg := &transcript.Segments[i]
		bestSpeaker := -1
		bestOverlap := 0.0

		for _, ds := range segments {
			overlapStart := seg.Start
			if ds.Start > overlapStart {
				overlapStart = ds.Start
			}
			overlapEnd := seg.End
			if ds.End < overlapEnd {
				overlapEnd = ds.End
			}
			overlap := overlapEnd - overlapStart
			if overlap > bestOverlap {
				bestOverlap = overlap
				bestSpeaker = ds.Speaker
			}
		}

		if bestSpeaker >= 0 {
			seg.Speaker = fmt.Sprintf("speaker_%d", bestSpeaker)
			speakerSet[bestSpeaker] = true
		}
	}

	transcript.Speakers = nil
	ids := make([]int, 0, len(speakerSet))
	for id := range speakerSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	for _, id := range ids {
		label := fmt.Sprintf("speaker_%d", id)
		transcript.Speakers = append(transcript.Speakers, Speaker{
			ID:    label,
			Label: label,
		})
	}
}
