package cli

import (
	"fmt"
	"runtime"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the ghostwriter daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		runtime.LockOSThread()
		audiocapture.EnsureAppInit()

		backend, _ := cmd.Flags().GetString("audio-backend")
		saveAudio, _ := cmd.Flags().GetBool("save-audio")
		transcriptionBackend, _ := cmd.Flags().GetString("transcription-backend")
		diarize, _ := cmd.Flags().GetBool("diarize")
		d, err := daemon.New(daemon.Config{
			OutputDir:            defaultOutputDir(),
			ModelPath:            defaultModelPath(),
			AudioBackend:         backend,
			TranscriptionBackend: transcriptionBackend,
			SaveAudio:            saveAudio,
			Diarize:              diarize,
			GoogleTokenPath:      googleTokenPath(),
		})
		if err != nil {
			return fmt.Errorf("failed to initialize daemon: %w", err)
		}

		var runErr error
		go func() {
			runErr = d.Run()
			audiocapture.QuitMainLoop()
		}()

		audiocapture.RunMainLoop()
		return runErr
	},
}

func init() {
	startCmd.Flags().String("audio-backend", "", "audio capture backend: sckit, blackhole (auto-detected if empty)")
	startCmd.Flags().Bool("save-audio", false, "save raw WAV audio alongside transcripts")
	startCmd.Flags().String("transcription-backend", "local", "transcription backend: local, assemblyai, openai")
	startCmd.Flags().Bool("diarize", false, "enable speaker diarization")
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the ghostwriter daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := daemon.NewClient()
		if err != nil {
			return fmt.Errorf("daemon is not running: %w", err)
		}
		defer client.Close()
		return client.Stop()
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := daemon.NewClient()
		if err != nil {
			fmt.Println("Status: not running")
			return nil
		}
		defer client.Close()

		status, err := client.Status()
		if err != nil {
			return err
		}
		fmt.Printf("Status: %s\n", status.State)
		if status.CurrentMeeting != "" {
			fmt.Printf("Recording: %s (%s)\n", status.CurrentMeeting, status.Duration)
		}
		return nil
	},
}
