package transcribe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
)

const assemblyAIBaseURL = "https://api.assemblyai.com/v2"

type AssemblyAITranscriber struct {
	apiKey string
	client *http.Client
}

func NewAssemblyAITranscriber(apiKey string) *AssemblyAITranscriber {
	return &AssemblyAITranscriber{
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *AssemblyAITranscriber) Transcribe(audio audiocapture.AudioData) (*Transcript, error) {
	tmpFile, err := os.CreateTemp("", "assemblyai-*.wav")
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

	return a.TranscribeFile(tmpPath)
}

func (a *AssemblyAITranscriber) TranscribeFile(path string) (*Transcript, error) {
	uploadURL, err := a.upload(path)
	if err != nil {
		return nil, fmt.Errorf("assemblyai upload failed: %w", err)
	}

	jobID, err := a.submit(uploadURL)
	if err != nil {
		return nil, fmt.Errorf("assemblyai submit failed: %w", err)
	}

	result, err := a.poll(jobID)
	if err != nil {
		return nil, fmt.Errorf("assemblyai transcription failed: %w", err)
	}

	return a.parseResponse(result)
}

func (a *AssemblyAITranscriber) Close() error {
	return nil
}

type assemblyAIUploadResponse struct {
	UploadURL string `json:"upload_url"`
}

func (a *AssemblyAITranscriber) upload(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	req, err := http.NewRequest("POST", assemblyAIBaseURL+"/upload", f)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("POST /v2/upload returned %d: %s", resp.StatusCode, body)
	}

	var result assemblyAIUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	return result.UploadURL, nil
}

type assemblyAISubmitRequest struct {
	AudioURL      string `json:"audio_url"`
	SpeakerLabels bool   `json:"speaker_labels"`
}

type assemblyAITranscriptResponse struct {
	ID         string                  `json:"id"`
	Status     string                  `json:"status"`
	Error      string                  `json:"error"`
	Text       string                  `json:"text"`
	AudioDuration float64             `json:"audio_duration"`
	Utterances []assemblyAIUtterance   `json:"utterances"`
}

type assemblyAIUtterance struct {
	Speaker    string             `json:"speaker"`
	Start      int                `json:"start"`
	End        int                `json:"end"`
	Text       string             `json:"text"`
	Confidence float64            `json:"confidence"`
	Words      []assemblyAIWord   `json:"words"`
}

type assemblyAIWord struct {
	Text       string  `json:"text"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Confidence float64 `json:"confidence"`
	Speaker    string  `json:"speaker"`
}

func (a *AssemblyAITranscriber) submit(uploadURL string) (string, error) {
	body, err := json.Marshal(assemblyAISubmitRequest{
		AudioURL:      uploadURL,
		SpeakerLabels: true,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", assemblyAIBaseURL+"/transcript", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", a.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("POST /v2/transcript returned %d: %s", resp.StatusCode, respBody)
	}

	var result assemblyAITranscriptResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode submit response: %w", err)
	}
	return result.ID, nil
}

func (a *AssemblyAITranscriber) poll(jobID string) (*assemblyAITranscriptResponse, error) {
	pollURL := assemblyAIBaseURL + "/transcript/" + jobID
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("transcription timed out after 10 minutes (job %s)", jobID)
		}

		req, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", a.apiKey)

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}

		var result assemblyAITranscriptResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode poll response: %w", err)
		}

		switch result.Status {
		case "completed":
			return &result, nil
		case "error":
			return nil, fmt.Errorf("assemblyai error: %s", result.Error)
		}

		time.Sleep(3 * time.Second)
	}
}

func (a *AssemblyAITranscriber) parseResponse(resp *assemblyAITranscriptResponse) (*Transcript, error) {
	t := &Transcript{
		Version: "1.0",
		ID:      GenerateID(),
		Metadata: Metadata{
			Date:            time.Now(),
			Source:          "assemblyai",
			DurationSeconds: int(resp.AudioDuration),
		},
	}

	speakerSet := make(map[string]bool)

	var fullText strings.Builder
	for _, utt := range resp.Utterances {
		seg := Segment{
			Start:      float64(utt.Start) / 1000.0,
			End:        float64(utt.End) / 1000.0,
			Speaker:    utt.Speaker,
			Text:       utt.Text,
			Confidence: utt.Confidence,
		}

		for _, w := range utt.Words {
			seg.Words = append(seg.Words, Word{
				Word:       w.Text,
				Start:      float64(w.Start) / 1000.0,
				End:        float64(w.End) / 1000.0,
				Confidence: w.Confidence,
			})
		}

		t.Segments = append(t.Segments, seg)
		speakerSet[utt.Speaker] = true

		if fullText.Len() > 0 {
			fullText.WriteString(" ")
		}
		fullText.WriteString(utt.Text)
	}

	for spk := range speakerSet {
		t.Speakers = append(t.Speakers, Speaker{ID: spk, Label: spk})
	}

	t.FullText = fullText.String()
	return t, nil
}
