package transcribe

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
)

const (
	openAITranscriptionsURL = "https://api.openai.com/v1/audio/transcriptions"
	openAIMaxFileSize       = 25 * 1024 * 1024
)

type OpenAITranscriber struct {
	apiKey string
	client *http.Client
}

func NewOpenAITranscriber(apiKey string) *OpenAITranscriber {
	return &OpenAITranscriber{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (o *OpenAITranscriber) Transcribe(audio audiocapture.AudioData) (*Transcript, error) {
	tmpFile, err := os.CreateTemp("", "openai-*.wav")
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

	return o.TranscribeFile(tmpPath)
}

func (o *OpenAITranscriber) TranscribeFile(path string) (*Transcript, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > openAIMaxFileSize {
		return nil, fmt.Errorf("file %s is %d MB, exceeds OpenAI 25 MB limit", path, info.Size()/(1024*1024))
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		part, err := writer.CreateFormFile("file", "audio.wav")
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		writer.WriteField("model", "whisper-1")
		writer.WriteField("response_format", "verbose_json")
		writer.WriteField("timestamp_granularities[]", "word")
		pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequest("POST", openAITranscriptionsURL, pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("POST /v1/audio/transcriptions returned %d: %s", resp.StatusCode, body)
	}

	var result openAITranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return o.parseResponse(&result), nil
}

func (o *OpenAITranscriber) Close() error {
	return nil
}

type openAITranscriptionResponse struct {
	Text     string       `json:"text"`
	Language string       `json:"language"`
	Duration float64      `json:"duration"`
	Words    []openAIWord `json:"words"`
}

type openAIWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func (o *OpenAITranscriber) parseResponse(resp *openAITranscriptionResponse) *Transcript {
	t := &Transcript{
		Version: "1.0",
		ID:      GenerateID(),
		Metadata: Metadata{
			Date:            time.Now(),
			Source:          "openai",
			Model:           "whisper-1",
			Language:        resp.Language,
			DurationSeconds: int(resp.Duration),
		},
		FullText: resp.Text,
	}

	segments := groupWordsIntoSegments(resp.Words)
	t.Segments = segments

	return t
}

func groupWordsIntoSegments(words []openAIWord) []Segment {
	if len(words) == 0 {
		return nil
	}

	var segments []Segment
	var current []openAIWord

	for _, w := range words {
		current = append(current, w)
		if endsWithSentenceBoundary(w.Word) {
			segments = append(segments, buildSegment(current))
			current = nil
		}
	}

	if len(current) > 0 {
		segments = append(segments, buildSegment(current))
	}

	return segments
}

func buildSegment(words []openAIWord) Segment {
	var texts []string
	var segWords []Word
	for _, w := range words {
		texts = append(texts, w.Word)
		segWords = append(segWords, Word{
			Word:  w.Word,
			Start: w.Start,
			End:   w.End,
		})
	}

	return Segment{
		Start: words[0].Start,
		End:   words[len(words)-1].End,
		Text:  strings.Join(texts, " "),
		Words: segWords,
	}
}

func endsWithSentenceBoundary(word string) bool {
	trimmed := strings.TrimRightFunc(word, unicode.IsPunct)
	if trimmed == word {
		return false
	}
	suffix := word[len(trimmed):]
	return strings.ContainsAny(suffix, ".!?")
}
