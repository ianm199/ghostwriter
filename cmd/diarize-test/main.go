package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ianmclaughlin/ghostwriter/pkg/diarize"
	"github.com/ianmclaughlin/ghostwriter/pkg/transcribe"
)

func main() {
	diarizeOnly := flag.Bool("diarize-only", false, "skip transcription, only run diarization")
	backend := flag.String("backend", "", "diarization backend: pyannote, sherpa (default: auto)")
	numSpeakers := flag.Int("num-speakers", 0, "number of speakers (0 = auto)")
	flag.Parse()

	wavPath := "testdata/audio/ES2002a.Mix-Headset.wav"
	if flag.NArg() > 0 {
		wavPath = flag.Arg(0)
	}

	if *backend == "sherpa" {
		if err := diarize.EnsureModels(); err != nil {
			log.Fatalf("Failed to ensure models: %v", err)
		}
	}

	cfg := diarize.DiarizeConfig{
		Backend:     *backend,
		NumSpeakers: *numSpeakers,
	}
	d, err := diarize.NewDiarizer(cfg)
	if err != nil {
		log.Fatalf("Failed to create diarizer: %v", err)
	}
	defer d.Close()

	fmt.Printf("Diarizing %s (backend=%s)...\n", wavPath, *backend)
	start := time.Now()
	segments, err := d.Diarize(wavPath)
	if err != nil {
		log.Fatalf("Diarization failed: %v", err)
	}
	elapsed := time.Since(start)

	speakerSet := make(map[int]bool)
	var totalDuration float64
	for _, seg := range segments {
		fmt.Printf("%.3f -- %.3f speaker_%02d\n", seg.Start, seg.End, seg.Speaker)
		speakerSet[seg.Speaker] = true
		totalDuration += seg.End - seg.Start
	}

	fmt.Printf("\nDiarization Summary:\n")
	fmt.Printf("  Speakers detected: %d\n", len(speakerSet))
	fmt.Printf("  Total segments: %d\n", len(segments))
	if len(segments) > 0 {
		fmt.Printf("  Avg segment duration: %.1fs\n", totalDuration/float64(len(segments)))
	}
	fmt.Printf("  Processing time: %s\n", elapsed.Round(time.Millisecond))

	if *diarizeOnly {
		return
	}

	runTranscriptionTest(wavPath)
}

func runTranscriptionTest(wavPath string) {
	tcfg := transcribe.TranscriberConfig{
		Backend: transcribe.BackendLocal,
		Whisper: transcribe.WhisperConfig{
			ModelPath: findWhisperModel(),
		},
		Diarize: true,
	}

	if tcfg.Whisper.ModelPath == "" {
		fmt.Println("\nSkipping transcription test: no whisper model found")
		return
	}

	fmt.Printf("\nRunning transcription + diarization with model: %s\n", tcfg.Whisper.ModelPath)

	t, err := transcribe.NewTranscriber(tcfg)
	if err != nil {
		fmt.Printf("Transcription setup failed: %v\n", err)
		return
	}
	defer t.Close()

	transcript, err := t.TranscribeFile(wavPath)
	if err != nil {
		fmt.Printf("Transcription failed: %v\n", err)
		return
	}

	fmt.Printf("\nTranscription + Diarization Results:\n")
	fmt.Printf("  Speakers: %d\n", len(transcript.Speakers))
	for _, s := range transcript.Speakers {
		fmt.Printf("    %s\n", s.Label)
	}
	fmt.Printf("  Segments: %d\n", len(transcript.Segments))

	limit := 20
	if len(transcript.Segments) < limit {
		limit = len(transcript.Segments)
	}
	fmt.Printf("\nFirst %d segments:\n", limit)
	for _, seg := range transcript.Segments[:limit] {
		fmt.Printf("  [%7.2f - %7.2f] %-12s %s\n", seg.Start, seg.End, seg.Speaker, seg.Text)
	}
}

func findWhisperModel() string {
	home, _ := os.UserHomeDir()
	dir := home + "/.local/share/ghostwriter/models"

	preferred := []string{
		"ggml-large-v3-turbo-q5_0.bin",
		"ggml-large-v3-turbo.bin",
		"ggml-small.en.bin",
		"ggml-base.en.bin",
		"ggml-tiny.en.bin",
	}
	for _, name := range preferred {
		path := dir + "/" + name
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
