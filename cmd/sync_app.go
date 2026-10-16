package cmd

import (
	"fmt"
	"net/url"

	"aeswibon.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
)

var syncAppName string

var syncAppCmd = &cobra.Command{
	Use:     "sync-app",
	GroupID: "appGroup",
	Short:   "Request an immediate Git sync for an application (via controller API)",
	Long: `Sends an HTTP request to a running gitopsctl controller to reconcile one application now.

Requires 'gitopsctl start' with API reachable (--api-url must match the controller).`,
	Example: `
  gitopsctl sync-app -n myapp
  gitopsctl --api-url http://127.0.0.1:9090 sync-app -n myapp`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, err := postAccepted(fmt.Sprintf("/api/v1/applications/%s/sync", url.PathEscape(syncAppName)), nil)
		if err != nil {
			return err
		}
		emitCommandEvent(events.TypeAppSyncRequested, map[string]any{
			"app":     syncAppName,
			"api_url": apiBaseURL,
		})
		fmt.Println(msg)
		return nil
	},
}

func init() {
	syncAppCmd.Flags().StringVarP(&syncAppName, "name", "n", "", "Application name (required)")
	_ = syncAppCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(syncAppCmd)
}
