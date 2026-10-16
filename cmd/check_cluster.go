package cmd

import (
	"fmt"
	"net/url"

	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
)

var checkClusterName string

var checkClusterCmd = &cobra.Command{
	Use:     "check-cluster",
	GroupID: "clusterGroup",
	Short:   "Request an immediate connectivity check for a cluster (via controller API)",
	Long: `Sends an HTTP request to a running gitopsctl controller to health-check one cluster.
Requires 'gitopsctl start' with API reachable (--api-url must match the controller).`,
	Example: `
  gitopsctl check-cluster -n production
  gitopsctl --api-url http://127.0.0.1:9090 check-cluster -n staging`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, err := postAccepted(fmt.Sprintf("/api/v1/clusters/%s/check", url.PathEscape(checkClusterName)), nil)
		if err != nil {
			return err
		}
		emitCommandEvent(events.TypeClusterHealthCheckRequested, map[string]any{
			"cluster": checkClusterName,
			"api_url": apiBaseURL,
		})
		fmt.Println(msg)
		return nil
	},
}

func init() {
	checkClusterCmd.Flags().StringVarP(&checkClusterName, "name", "n", "", "Cluster name (required)")
	_ = checkClusterCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(checkClusterCmd)
}
