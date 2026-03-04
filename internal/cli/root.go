package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ghostwriter",
	Short: "Meeting transcription daemon",
	Long:  `Ghostwriter is a local-first daemon that detects meetings, captures audio, transcribes via Whisper, and outputs structured transcript files.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(recordCmd)
	rootCmd.AddCommand(transcribeCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(modelsCmd)
}
