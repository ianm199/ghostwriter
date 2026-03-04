package transcribe

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianmclaughlin/ghostwriter/internal/capture"
	"github.com/ianmclaughlin/ghostwriter/internal/output"
)

type WhisperConfig struct {
	ModelPath string
	Language  string
}

type WhisperTranscriber struct {
	config     WhisperConfig
	binaryPath string
}

func NewWhisperTranscriber(config WhisperConfig) (*WhisperTranscriber, error) {
	binaryPath, err := exec.LookPath("whisper-cli")
	if err != nil {
		return nil, fmt.Errorf("whisper-cli not found in PATH — install with 'brew install whisper-cpp'")
	}

	if config.ModelPath == "" {
		config.ModelPath = defaultModelPath()
	}
	if config.Language == "" {
		config.Language = "en"
	}

	if _, err := os.Stat(config.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("whisper model not found at %s — run 'ghostwriter models download base.en'", config.ModelPath)
	}

	return &WhisperTranscriber{config: config, binaryPath: binaryPath}, nil
}

func (w *WhisperTranscriber) Transcribe(audio capture.AudioData) (*output.Transcript, error) {
	tmpFile, err := os.CreateTemp("", "ghostwriter-*.wav")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp WAV file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := writeWAV(tmpFile, audio.Samples, audio.SampleRate); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("failed to write WAV: %w", err)
	}
	tmpFile.Close()

	return w.TranscribeFile(tmpPath)
}

func (w *WhisperTranscriber) TranscribeFile(path string) (*output.Transcript, error) {
	outputPrefix := strings.TrimSuffix(path, filepath.Ext(path))

	cmd := exec.Command(w.binaryPath,
		"-m", w.config.ModelPath,
		"-f", path,
		"-l", w.config.Language,
		"-ojf",
		"-of", outputPrefix,
	)
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("whisper-cli failed: %w\noutput: %s", err, string(cmdOutput))
	}

	jsonPath := outputPrefix + ".json"
	defer os.Remove(jsonPath)

	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("whisper-cli JSON output not found at %s: %w", jsonPath, err)
	}

	return parseWhisperJSON(jsonData, w.config.Language)
}

func (w *WhisperTranscriber) Close() error {
	return nil
}

type whisperJSON struct {
	Model struct {
		Type string `json:"type"`
	} `json:"model"`
	Transcription []whisperSegment `json:"transcription"`
}

type whisperSegment struct {
	Offsets struct {
		From int `json:"from"`
		To   int `json:"to"`
	} `json:"offsets"`
	Text   string         `json:"text"`
	Tokens []whisperToken `json:"tokens"`
}

type whisperToken struct {
	Text    string `json:"text"`
	Offsets struct {
		From int `json:"from"`
		To   int `json:"to"`
	} `json:"offsets"`
	P float64 `json:"p"`
}

func parseWhisperJSON(data []byte, language string) (*output.Transcript, error) {
	var wj whisperJSON
	if err := json.Unmarshal(data, &wj); err != nil {
		return nil, fmt.Errorf("failed to parse whisper JSON: %w", err)
	}

	t := &output.Transcript{
		Version: "1.0",
		ID:      output.GenerateID(),
		Metadata: output.Metadata{
			Date:     time.Now(),
			Source:   "whisper-cpp",
			Language: language,
			Model:    wj.Model.Type,
		},
	}

	var fullText strings.Builder
	for _, seg := range wj.Transcription {
		startSec := float64(seg.Offsets.From) / 1000.0
		endSec := float64(seg.Offsets.To) / 1000.0

		segment := output.Segment{
			Start: startSec,
			End:   endSec,
			Text:  strings.TrimSpace(seg.Text),
		}

		for _, tok := range seg.Tokens {
			segment.Words = append(segment.Words, output.Word{
				Word:       tok.Text,
				Start:      float64(tok.Offsets.From) / 1000.0,
				End:        float64(tok.Offsets.To) / 1000.0,
				Confidence: tok.P,
			})
		}

		if len(seg.Tokens) > 0 {
			var sum float64
			for _, tok := range seg.Tokens {
				sum += tok.P
			}
			segment.Confidence = sum / float64(len(seg.Tokens))
		}

		t.Segments = append(t.Segments, segment)
		if fullText.Len() > 0 {
			fullText.WriteString(" ")
		}
		fullText.WriteString(segment.Text)
	}

	t.FullText = fullText.String()

	if len(t.Segments) > 0 {
		t.Metadata.DurationSeconds = int(t.Segments[len(t.Segments)-1].End)
	}

	return t, nil
}

func writeWAV(f *os.File, samples []float32, sampleRate int) error {
	numSamples := len(samples)
	dataSize := numSamples * 2
	fileSize := 36 + dataSize

	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(fileSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	if _, err := f.Write(header); err != nil {
		return err
	}

	pcm := make([]byte, dataSize)
	for i, s := range samples {
		val := int16(math.Max(-1, math.Min(1, float64(s))) * 32767)
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(val))
	}

	_, err := f.Write(pcm)
	return err
}

func defaultModelPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ghostwriter", "models", "ggml-base.en.bin")
}
