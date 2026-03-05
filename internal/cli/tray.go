package cli

import (
	"github.com/ianmclaughlin/ghostwriter/internal/tray"
	"github.com/spf13/cobra"
)

var trayCmd = &cobra.Command{
	Use:   "tray",
	Short: "Run the menu bar app",
	RunE: func(cmd *cobra.Command, args []string) error {
		tray.Run()
		return nil
	},
}
