package cmd

import (
	"log"

	"aeswibon.com/github/gitopsctl/internal/tui"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the interactive terminal dashboard",
	Long:  `The dashboard provides a real-time view of all registered applications and clusters, allowing for easy monitoring and manual actions.`,
	Run: func(cmd *cobra.Command, args []string) {
		url := cmd.Flag("api-url").Value.String()
		if err := tui.Run(url, apiToken); err != nil {
			log.Fatalf("Error running dashboard: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}
