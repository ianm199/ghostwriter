//go:build darwin

package audiocapture

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

func Resample48kStereoTo16kMono(samples []float32, channels int) []int16 {
	var mono []float32
	if channels >= 2 {
		for i := 0; i+1 < len(samples); i += channels {
			mono = append(mono, (samples[i]+samples[i+1])/2)
		}
	} else {
		mono = samples
	}

	const decimationFactor = 3
	var out []int16
	for i := 1; i+1 < len(mono); i += decimationFactor {
		avg := (mono[i-1] + mono[i] + mono[i+1]) / 3
		clamped := math.Max(-1, math.Min(1, float64(avg)))
		out = append(out, int16(clamped*32767))
	}
	return out
}

func writeWAVFromInt16(path string, samples []int16, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create WAV file: %w", err)
	}
	defer f.Close()

	dataSize := uint32(len(samples) * 2)
	fileSize := 36 + dataSize

	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], fileSize)
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
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	if _, err := f.Write(header); err != nil {
		return fmt.Errorf("failed to write WAV header: %w", err)
	}

	if err := binary.Write(f, binary.LittleEndian, samples); err != nil {
		return fmt.Errorf("failed to write WAV data: %w", err)
	}

	return nil
}
