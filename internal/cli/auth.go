package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/ianmclaughlin/ghostwriter/pkg/calendar"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for external services",
}

var authGoogleCmd = &cobra.Command{
	Use:   "google",
	Short: "Authenticate with Google Calendar",
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		store := calendar.NewTokenStore(googleTokenPath())

		if !force {
			if _, err := store.Load(); err == nil {
				fmt.Println("Google Calendar is already authenticated.")
				fmt.Println("Use --force to re-authenticate.")
				return nil
			}
		}

		fmt.Println("Opening browser for Google sign-in...")
		token, err := calendar.Authorize(context.Background(), calendar.DefaultOAuthCredentials())
		if err != nil {
			return fmt.Errorf("authorization failed: %w", err)
		}

		if err := store.Save(token); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}

		fmt.Printf("Authenticated successfully. Token saved to %s\n", googleTokenPath())
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	Run: func(cmd *cobra.Command, args []string) {
		store := calendar.NewTokenStore(googleTokenPath())
		if _, err := store.Load(); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Google Calendar: not authenticated")
				return
			}
			fmt.Printf("Google Calendar: error loading token: %v\n", err)
			return
		}
		fmt.Println("Google Calendar: authenticated")
	},
}

func init() {
	authGoogleCmd.Flags().Bool("force", false, "re-authenticate even if token exists")
	authCmd.AddCommand(authGoogleCmd)
	authCmd.AddCommand(authStatusCmd)
}
