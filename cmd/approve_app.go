package cmd

import (
	"fmt"
	"net/url"

	"aeswibon.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
)

var (
	approveAppName    string
	approveCommitHash string
)

var approveAppCmd = &cobra.Command{
	Use:     "approve-app",
	GroupID: "appGroup",
	Short:   "Approve a pending sync for an application (via controller API)",
	Long: `Sends an HTTP request to a running gitopsctl controller to approve a specific commit for an application.
This is used when the application's sync policy is set to 'manual'.`,
	Example: `
  gitopsctl approve-app -n myapp -c 827e3ef
  gitopsctl --api-url http://127.0.0.1:9090 approve-app -n myapp -c 827e3ef`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, err := postAccepted(
			fmt.Sprintf("/api/v1/applications/%s/approve", url.PathEscape(approveAppName)),
			map[string]string{"commitHash": approveCommitHash},
		)
		if err != nil {
			return err
		}
		emitCommandEvent(events.TypeAppSyncRequested, map[string]any{
			"app":     approveAppName,
			"commit":  approveCommitHash,
			"api_url": apiBaseURL,
		})
		fmt.Println(msg)
		return nil
	},
}

func init() {
	approveAppCmd.Flags().StringVarP(&approveAppName, "name", "n", "", "Application name (required)")
	approveAppCmd.Flags().StringVarP(&approveCommitHash, "commit", "c", "", "Commit hash to approve (required)")
	_ = approveAppCmd.MarkFlagRequired("name")
	_ = approveAppCmd.MarkFlagRequired("commit")
	rootCmd.AddCommand(approveAppCmd)
}
