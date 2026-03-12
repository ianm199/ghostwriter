package diarize

import (
	"fmt"
	"os"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

func newSherpaDiarizer(cfg DiarizeConfig) (*Diarizer, error) {
	if cfg.SegmentationModelPath == "" {
		cfg.SegmentationModelPath = DefaultSegmentationModelPath()
	}
	if cfg.EmbeddingModelPath == "" {
		cfg.EmbeddingModelPath = DefaultEmbeddingModelPath()
	}
	if cfg.Threshold == 0 && cfg.NumSpeakers == 0 {
		cfg.Threshold = 0.9
	}

	if _, err := os.Stat(cfg.SegmentationModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("segmentation model not found at %s — run 'ghostwriter models download diarize'", cfg.SegmentationModelPath)
	}
	if _, err := os.Stat(cfg.EmbeddingModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("embedding model not found at %s — run 'ghostwriter models download diarize'", cfg.EmbeddingModelPath)
	}

	scfg := &sherpa.OfflineSpeakerDiarizationConfig{}
	scfg.Segmentation.Pyannote.Model = cfg.SegmentationModelPath
	scfg.Embedding.Model = cfg.EmbeddingModelPath
	scfg.MinDurationOn = 0.3
	scfg.MinDurationOff = 0.5

	if cfg.NumSpeakers > 0 {
		scfg.Clustering.NumClusters = cfg.NumSpeakers
	} else {
		scfg.Clustering.Threshold = cfg.Threshold
	}

	sd := sherpa.NewOfflineSpeakerDiarization(scfg)
	if sd == nil {
		return nil, fmt.Errorf("failed to initialize speaker diarization — check model paths")
	}

	return &Diarizer{
		diarize: func(wavPath string) ([]SpeakerSegment, error) {
			wave := sherpa.ReadWave(wavPath)
			if wave == nil {
				return nil, fmt.Errorf("failed to read WAV file: %s", wavPath)
			}
			if wave.SampleRate != sd.SampleRate() {
				return nil, fmt.Errorf("sample rate mismatch: file has %d, model expects %d", wave.SampleRate, sd.SampleRate())
			}
			raw := sd.Process(wave.Samples)
			segments := make([]SpeakerSegment, len(raw))
			for i, s := range raw {
				segments[i] = SpeakerSegment{
					Start:   float64(s.Start),
					End:     float64(s.End),
					Speaker: s.Speaker,
				}
			}
			return segments, nil
		},
		close: func() {
			sherpa.DeleteOfflineSpeakerDiarization(sd)
		},
	}, nil
}
