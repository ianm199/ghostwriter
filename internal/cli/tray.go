package cli

import (
	"fmt"
	"runtime"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
	"github.com/ianmclaughlin/ghostwriter/internal/tray"
	"github.com/ianmclaughlin/ghostwriter/pkg/audiocapture"
	"github.com/spf13/cobra"
)

var trayCmd = &cobra.Command{
	Use:   "tray",
	Short: "Run the daemon with the floating widget",
	RunE: func(cmd *cobra.Command, args []string) error {
		runtime.LockOSThread()
		audiocapture.EnsureAppInit()

		tray.Setup()

		backend, _ := cmd.Flags().GetString("audio-backend")
		saveAudio, _ := cmd.Flags().GetBool("save-audio")
		transcriptionBackend, _ := cmd.Flags().GetString("transcription-backend")
		d, err := daemon.New(daemon.Config{
			OutputDir:            defaultOutputDir(),
			ModelPath:            defaultModelPath(),
			AudioBackend:         backend,
			TranscriptionBackend: transcriptionBackend,
			SaveAudio:            saveAudio,
			GoogleTokenPath:      googleTokenPath(),
		})
		if err != nil {
			return fmt.Errorf("failed to initialize daemon: %w", err)
		}

		go func() {
			if err := d.Run(); err != nil {
				fmt.Printf("daemon error: %v\n", err)
			}
			audiocapture.QuitMainLoop()
		}()

		audiocapture.RunMainLoop()
		return nil
	},
}

func init() {
	trayCmd.Flags().String("audio-backend", "", "audio capture backend: sckit, blackhole (auto-detected if empty)")
	trayCmd.Flags().Bool("save-audio", false, "save raw WAV audio alongside transcripts")
	trayCmd.Flags().String("transcription-backend", "local", "transcription backend: local, assemblyai, openai")
}
