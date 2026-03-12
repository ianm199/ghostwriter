package cli

import (
	"fmt"
	"os"

	"github.com/ianmclaughlin/ghostwriter/pkg/transcribe"
	"github.com/spf13/cobra"
)

var transcribeOutput string

var transcribeCmd = &cobra.Command{
	Use:   "transcribe [file]",
	Short: "Transcribe an audio file (standalone, no daemon needed)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backend, _ := cmd.Flags().GetString("transcription-backend")
		diarize, _ := cmd.Flags().GetBool("diarize")

		cfg := transcribe.TranscriberConfig{
			Backend: transcribe.Backend(backend),
			Whisper: transcribe.WhisperConfig{
				ModelPath:  defaultModelPath(),
				MaxContext: 0,
			},
			Diarize: diarize,
		}
		switch cfg.Backend {
		case transcribe.BackendAssemblyAI:
			cfg.APIKey = os.Getenv("ASSEMBLYAI_API_KEY")
		case transcribe.BackendOpenAI:
			cfg.APIKey = os.Getenv("OPENAI_API_KEY")
		}

		t, err := transcribe.NewTranscriber(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize transcriber: %w", err)
		}
		defer t.Close()

		transcript, err := t.TranscribeFile(args[0])
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}

		format, _ := cmd.Flags().GetString("format")

		dest := transcribeOutput
		if dest == "" {
			switch format {
			case "text":
				dest = args[0] + ".transcript.txt"
			default:
				dest = args[0] + ".transcript.json"
			}
		}

		switch format {
		case "text":
			if err := os.WriteFile(dest, []byte(transcript.FormatText()), 0644); err != nil {
				return fmt.Errorf("failed to write transcript: %w", err)
			}
		default:
			if err := transcribe.WriteTranscript(transcript, dest); err != nil {
				return fmt.Errorf("failed to write transcript: %w", err)
			}
		}

		fmt.Printf("Transcript written to %s\n", dest)
		return nil
	},
}

func init() {
	transcribeCmd.Flags().StringVarP(&transcribeOutput, "output", "o", "", "output file path")
	transcribeCmd.Flags().String("transcription-backend", "local", "transcription backend: local, assemblyai, openai")
	transcribeCmd.Flags().Bool("diarize", false, "enable speaker diarization")
	transcribeCmd.Flags().String("format", "json", "output format: json, text")
}
