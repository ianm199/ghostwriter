package cli

import (
	"fmt"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the ghostwriter daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := daemon.New()
		if err != nil {
			return fmt.Errorf("failed to initialize daemon: %w", err)
		}
		return d.Run()
	},
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
