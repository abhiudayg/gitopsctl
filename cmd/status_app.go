package cmd

import (
	"abhiudayg.com/github/gitopsctl/internal/utils"
	"github.com/spf13/cobra"
)

var statusAppOpts utils.ListOptions

var statusAppCmd = &cobra.Command{
	Use:     "status-apps",
	GroupID: "appGroup",
	Args:    cobra.NoArgs,
	Short:   "Show status of registered GitOps applications",
	Long:    `Displays the current status, last synced commit, and messages for all registered GitOps applications.`,
	Example: `
  # Show status of all registered applications
  gitopsctl status-apps

  # Show status of applications with details
  gitopsctl status-apps --details

  # Filter applications by status (synced, error, pending, stopped)
  gitopsctl status-apps --status synced

  # Sort applications by name or status
  gitopsctl status-apps --sort-by name

  # Output in JSON format
  gitopsctl status-apps --output json

  # Compact view without headers
  gitopsctl status-apps --no-header
	`,
	RunE: runStatusAppsCommand,
}

func runStatusAppsCommand(cmd *cobra.Command, args []string) error {
	statusAppOpts.ShowDetails = true
	return utils.RunListCommand(
		logger,
		statusAppOpts,
		loadAppsForList,
		filterAppsForList,
		sortAppsForList,
		handleEmptyAppsForList,
	)

}

func init() {
	rootCmd.AddCommand(statusAppCmd)
	utils.AddListFlags(statusAppCmd, &statusAppOpts, "name")

	statusAppCmd.Flags().Lookup("details").Hidden = true
	statusAppCmd.Flags().Lookup("output").Usage = "Output format: table, json, yaml (default: table)"
	statusAppCmd.Flags().Lookup("status").Usage = "Filter by status: all, synced, error, pending, stopped"
	statusAppCmd.Flags().Lookup("sort-by").Usage = "Sort by: name, status, branch"

	_ = statusAppCmd.RegisterFlagCompletionFunc("status", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"all", "synced", "error", "pending", "stopped"}, cobra.ShellCompDirectiveDefault
	})
	_ = statusAppCmd.RegisterFlagCompletionFunc("sort-by", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"name", "status", "branch"}, cobra.ShellCompDirectiveDefault
	})
}
