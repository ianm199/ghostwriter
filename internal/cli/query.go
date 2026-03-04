package cli

import (
	"fmt"
	"time"

	"github.com/ianmclaughlin/ghostwriter/internal/output"
	"github.com/spf13/cobra"
)

var listSince string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent transcripts",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := output.NewStore(output.DefaultOutputDir())

		var since time.Time
		if listSince != "" {
			var err error
			since, err = time.Parse("2006-01-02", listSince)
			if err != nil {
				return fmt.Errorf("invalid date format (use YYYY-MM-DD): %w", err)
			}
		}

		transcripts, err := store.List(since)
		if err != nil {
			return err
		}

		for _, t := range transcripts {
			title := t.Metadata.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Printf("%s  %s  %s  %ds\n",
				t.ID,
				t.Metadata.Date.Format("2006-01-02 15:04"),
				title,
				t.Metadata.DurationSeconds,
			)
		}
		return nil
	},
}

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Print transcript text",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := output.NewStore(output.DefaultOutputDir())
		transcript, err := store.Get(args[0])
		if err != nil {
			return err
		}
		fmt.Println(transcript.FullText)
		return nil
	},
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search across all transcripts",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := output.NewStore(output.DefaultOutputDir())
		results, err := store.Search(args[0])
		if err != nil {
			return err
		}

		for _, r := range results {
			fmt.Printf("%s  %s  ...%s...\n", r.ID, r.Title, r.Snippet)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listSince, "since", "", "filter by date (YYYY-MM-DD)")
}
