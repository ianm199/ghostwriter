package testutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ianmclaughlin/ghostwriter/internal/capture"
	"github.com/ianmclaughlin/ghostwriter/internal/detect"
	"github.com/ianmclaughlin/ghostwriter/internal/output"
)

// FakeCapturer implements capture.AudioCapturer for tests.
type FakeCapturer struct {
	mu        sync.Mutex
	recording bool
	Calls     []string // "start" or "stop"
	StartErr  error
	StopErr   error
	// WavData is written to a temp file and returned from Stop.
	// If nil, a minimal valid WAV header is used.
	WavData []byte
}

var _ capture.AudioCapturer = (*FakeCapturer)(nil)

func (f *FakeCapturer) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "start")
	if f.StartErr != nil {
		return f.StartErr
	}
	f.recording = true
	return nil
}

func (f *FakeCapturer) Stop() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "stop")
	if f.StopErr != nil {
		return "", f.StopErr
	}
	f.recording = false

	data := f.WavData
	if data == nil {
		data = minimalWAV()
	}
	tmp, err := os.CreateTemp("", "fake-capture-*.wav")
	if err != nil {
		return "", err
	}
	tmp.Write(data)
	tmp.Close()
	return tmp.Name(), nil
}

func (f *FakeCapturer) IsRecording() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recording
}

func (f *FakeCapturer) CallCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if c == method {
			n++
		}
	}
	return n
}

// minimalWAV returns a valid WAV file with silence (just a header + a few zero samples).
func minimalWAV() []byte {
	// 44-byte header + 100 bytes of silence
	numSamples := 50
	dataSize := numSamples * 2
	fileSize := 36 + dataSize
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	putLE32(buf[4:8], uint32(fileSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	putLE32(buf[16:20], 16)
	putLE16(buf[20:22], 1)     // PCM
	putLE16(buf[22:24], 1)     // mono
	putLE32(buf[24:28], 16000) // sample rate
	putLE32(buf[28:32], 32000) // byte rate
	putLE16(buf[32:34], 2)     // block align
	putLE16(buf[34:36], 16)    // bits per sample
	copy(buf[36:40], "data")
	putLE32(buf[40:44], uint32(dataSize))
	// samples are zero (silence)
	return buf
}

func putLE16(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func putLE32(b []byte, v uint32) { b[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24) }

// FakeTranscriber implements transcribe.Transcriber for tests.
type FakeTranscriber struct {
	mu     sync.Mutex
	Result *output.Transcript
	Err    error
	Calls  []string // file paths passed to TranscribeFile
	Delay  time.Duration
}

func (f *FakeTranscriber) Transcribe(audio capture.AudioData) (*output.Transcript, error) {
	return f.TranscribeFile("<in-memory>")
}

func (f *FakeTranscriber) TranscribeFile(path string) (*output.Transcript, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, path)
	delay := f.Delay
	err := f.Err
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Result == nil {
		return nil, fmt.Errorf("no result configured")
	}
	// deep copy to avoid shared state
	result := *f.Result
	result.Metadata = f.Result.Metadata
	result.Segments = make([]output.Segment, len(f.Result.Segments))
	copy(result.Segments, f.Result.Segments)
	return &result, nil
}

func (f *FakeTranscriber) Close() error { return nil }

func (f *FakeTranscriber) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// FakeDetector implements detect.MeetingDetector for tests.
// Emit signals by calling Emit().
type FakeDetector struct {
	signals chan detect.Signal
}

var _ detect.MeetingDetector = (*FakeDetector)(nil)

func NewFakeDetector() *FakeDetector {
	return &FakeDetector{signals: make(chan detect.Signal, 10)}
}

func (f *FakeDetector) Start(ctx context.Context) <-chan detect.Signal {
	out := make(chan detect.Signal, 10)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-f.signals:
				out <- sig
			}
		}
	}()
	return out
}

func (f *FakeDetector) Emit(sig detect.Signal) {
	f.signals <- sig
}

// SampleTranscript returns a realistic transcript for testing.
func SampleTranscript() *output.Transcript {
	return &output.Transcript{
		Version: "1.0",
		Metadata: output.Metadata{
			Date:            time.Now(),
			DurationSeconds: 120,
			Source:          "test",
			Model:           "fake",
			Language:        "en",
		},
		Segments: []output.Segment{
			{
				Start:      0.0,
				End:        4.5,
				Text:       "Let's get started with sprint planning.",
				Confidence: 0.95,
			},
			{
				Start:      5.0,
				End:        12.0,
				Text:       "I think we should prioritize the API migration.",
				Confidence: 0.91,
			},
		},
		FullText: "Let's get started with sprint planning. I think we should prioritize the API migration.",
	}
}

// CopyFile copies src to dst. Useful for test fixtures.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
