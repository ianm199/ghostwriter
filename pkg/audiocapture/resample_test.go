//go:build darwin

package audiocapture

import (
	"math"
	"os"
	"testing"
)

func TestResample48kStereoTo16kMono(t *testing.T) {
	numSamples := 48000
	stereo := make([]float32, numSamples*2)
	for i := 0; i < numSamples; i++ {
		val := float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
		stereo[i*2] = val
		stereo[i*2+1] = val
	}

	out := Resample48kStereoTo16kMono(stereo, 2)

	expectedLen := (numSamples - 2) / 3
	if math.Abs(float64(len(out)-expectedLen)) > 1 {
		t.Errorf("expected ~%d samples, got %d", expectedLen, len(out))
	}

	for i, s := range out {
		if s < -32767 || s > 32767 {
			t.Errorf("sample %d out of int16 range: %d", i, s)
			break
		}
	}
}

func TestResampleMono(t *testing.T) {
	mono := make([]float32, 480)
	for i := range mono {
		mono[i] = 0.5
	}

	out := Resample48kStereoTo16kMono(mono, 1)
	if len(out) == 0 {
		t.Fatal("expected non-empty output for mono input")
	}

	for _, s := range out {
		expected := int16(math.Round(0.5 * 32767))
		diff := int(s) - int(expected)
		if diff < -1 || diff > 1 {
			t.Errorf("expected ~%d, got %d", expected, s)
			break
		}
	}
}

func TestWriteWAVFromInt16(t *testing.T) {
	samples := make([]int16, 16000)
	for i := range samples {
		samples[i] = int16(i % 1000)
	}

	path := t.TempDir() + "/test.wav"
	if err := writeWAVFromInt16(path, samples, 16000); err != nil {
		t.Fatalf("writeWAVFromInt16 failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	expectedSize := int64(44 + len(samples)*2)
	if info.Size() != expectedSize {
		t.Errorf("expected file size %d, got %d", expectedSize, info.Size())
	}
}
