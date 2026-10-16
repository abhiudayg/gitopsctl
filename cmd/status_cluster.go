package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/utils"
	"github.com/spf13/cobra"
)

var statusClusterOpts utils.ListOptions

var statusClusterCmd = &cobra.Command{
	Use:     "status-clusters",
	GroupID: "clusterGroup",
	Short:   "Show status of registered Kubernetes clusters",
	Long:    `Displays the current health status, last checked time, and messages for all registered Kubernetes clusters.`,
	Example: `
  # Show status of all registered clusters
  gitopsctl status-clusters

  # Show status of clusters with details
  gitopsctl status-clusters --details

  # Filter clusters by status (healthy, error, pending)
  gitopsctl status-clusters --status healthy

  # Sort clusters by name or status
  gitopsctl status-clusters --sort-by name

  # Output in JSON format
  gitopsctl status-clusters --output json

  # Compact view without headers
  gitopsctl status-clusters --no-header
`,
	RunE: func(cmdCobra *cobra.Command, args []string) error {
		clusters, err := cluster.LoadClusters(cluster.DefaultClusterConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load cluster configurations: %w", err)
		}

		if len(clusters.List()) == 0 {
			logger.Info("No clusters registered. Use 'gitopsctl register-cluster' to add one.")
			return nil
		}

		fmt.Println("--- Kubernetes Cluster Health Status ---")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.TabIndent)

		_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tMESSAGE\tLAST CHECKED\tKUBECONFIG PATH")
		_, _ = fmt.Fprintln(w, "----\t------\t-------\t------------\t---------------")

		for _, cl := range clusters.List() {
			lastChecked := "N/A"
			if !cl.LastCheckedAt.IsZero() {
				lastChecked = cl.LastCheckedAt.Format("2006-01-02 15:04:05 MST")
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				cl.Name,
				cl.Status,
				cl.Message,
				lastChecked,
				cl.KubeconfigPath,
			)
		}
		_ = w.Flush()
		fmt.Println("--------------------------------------")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusClusterCmd)
	utils.AddListFlags(statusClusterCmd, &statusClusterOpts, "name")

	statusClusterCmd.Flags().Lookup("details").Hidden = true

	statusClusterCmd.Flags().Lookup("output").Usage = "Output format: table, json, yaml (default: table)"
	statusClusterCmd.Flags().Lookup("status").Usage = "Filter by status: all, active, unreachable, error, pending"
	statusClusterCmd.Flags().Lookup("sort-by").Usage = "Sort by: name, status, registered"

	_ = statusClusterCmd.RegisterFlagCompletionFunc("status", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"all", "active", "unreachable", "error", "pending"}, cobra.ShellCompDirectiveDefault
	})
	_ = statusClusterCmd.RegisterFlagCompletionFunc("sort-by", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"name", "status", "registered"}, cobra.ShellCompDirectiveDefault
	})
}
