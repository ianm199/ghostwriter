package cli

import (
	"fmt"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
	"github.com/spf13/cobra"
)

var recordTitle string

var recordCmd = &cobra.Command{
	Use:   "record",
	Short: "Control recording",
}

var recordStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start recording",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := daemon.NewClient()
		if err != nil {
			return fmt.Errorf("daemon is not running: %w", err)
		}
		defer client.Close()
		return client.StartRecording(recordTitle)
	},
}

var recordStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop recording",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := daemon.NewClient()
		if err != nil {
			return fmt.Errorf("daemon is not running: %w", err)
		}
		defer client.Close()
		return client.StopRecording()
	},
}

func init() {
	recordStartCmd.Flags().StringVar(&recordTitle, "title", "", "meeting title")
	recordCmd.AddCommand(recordStartCmd)
	recordCmd.AddCommand(recordStopCmd)
}
