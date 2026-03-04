package cli

import (
	"fmt"

	"github.com/ianmclaughlin/ghostwriter/internal/output"
	"github.com/ianmclaughlin/ghostwriter/internal/transcribe"
	"github.com/spf13/cobra"
)

var transcribeOutput string

var transcribeCmd = &cobra.Command{
	Use:   "transcribe [file]",
	Short: "Transcribe an audio file (standalone, no daemon needed)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		w, err := transcribe.NewWhisperTranscriber(transcribe.WhisperConfig{})
		if err != nil {
			return fmt.Errorf("failed to initialize whisper: %w", err)
		}
		defer w.Close()

		transcript, err := w.TranscribeFile(args[0])
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}

		dest := transcribeOutput
		if dest == "" {
			dest = args[0] + ".transcript.json"
		}

		if err := output.WriteTranscript(transcript, dest); err != nil {
			return fmt.Errorf("failed to write transcript: %w", err)
		}

		fmt.Printf("Transcript written to %s\n", dest)
		return nil
	},
}

func init() {
	transcribeCmd.Flags().StringVarP(&transcribeOutput, "output", "o", "", "output file path")
}
