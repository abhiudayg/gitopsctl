package cmd

import (
	"fmt"
	"sort"
	"strings"

	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/utils"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var listClusterOpts utils.ListOptions

var listClusterCmd = &cobra.Command{
	Use:     "list-clusters",
	GroupID: "clusterGroup",
	Short:   "List all registered Kubernetes clusters",
	Long: `Displays information about all Kubernetes clusters registered with gitopsctl.

This command shows cluster names, kubeconfig paths, connection status, and registration details.
You can filter, sort, and format the output according to your needs.`,
	Example: `
  # List all clusters in table format
  gitopsctl list-clusters

  # List only active clusters
  gitopsctl list-clusters --status active

  # List clusters sorted by registration date
  gitopsctl list-clusters --sort-by registered

  # Show detailed information
  gitopsctl list-clusters --details

  # Output as JSON for automation
  gitopsctl list-clusters --output json

  # Compact view without headers
  gitopsctl list-clusters --no-header
	`,
	RunE: runListClustersCommand,
}

func runListClustersCommand(cmd *cobra.Command, args []string) error {
	return utils.RunListCommand(
		logger,
		listClusterOpts,
		loadClustersForList,
		filterClustersForList,
		sortClustersForList,
		handleEmptyClustersForList,
	)
}

// loadClustersForList loads clusters and converts them to cliutils.Renderable.
func loadClustersForList() ([]utils.Renderable, error) {
	clusters, err := cluster.LoadClusters(cluster.DefaultClusterConfigFile)
	if err != nil {
		logger.Error("Failed to load cluster configurations", zap.Error(err))
		return nil, fmt.Errorf("failed to load cluster configurations: %w", err)
	}

	if len(clusters.List()) == 0 {
		return nil, fmt.Errorf("no clusters registered") // Signal empty state
	}
	logger.Info("Loaded clusters successfully", zap.Int("count", len(clusters.List())))

	renderableClusters := make([]utils.Renderable, len(clusters.List()))
	for i, cl := range clusters.List() {
		renderableClusters[i] = cl
	}
	return renderableClusters, nil
}

// filterClustersForList filters a slice of Renderable (cluster.Cluster) by status.
func filterClustersForList(items []utils.Renderable, statusFilter string) []utils.Renderable {
	if statusFilter == "" || strings.ToLower(statusFilter) == "all" {
		return items
	}

	var filtered []utils.Renderable
	targetStatus := strings.ToLower(statusFilter)

	for _, item := range items {
		if clItem, ok := item.(*cluster.Cluster); ok {
			if strings.ToLower(clItem.Status) == targetStatus {
				filtered = append(filtered, clItem)
			}
		}
	}
	return filtered
}

// sortClustersForList sorts a slice of Renderable (cluster.Cluster) by a given field.
func sortClustersForList(items []utils.Renderable, sortField string) {
	sort.Slice(items, func(i, j int) bool {
		clI := items[i].(*cluster.Cluster)
		clJ := items[j].(*cluster.Cluster)

		switch strings.ToLower(sortField) {
		case "status":
			if clI.Status == clJ.Status {
				return clI.Name < clJ.Name
			}
			return clI.Status < clJ.Status
		case "registered", "date":
			return clI.RegisteredAt.Before(clJ.RegisteredAt)
		default: // Default to name
			return clI.Name < clJ.Name
		}
	})
}

// handleEmptyClustersForList displays a message if no clusters are found.
func handleEmptyClustersForList(statusFilter string) error {
	if statusFilter == "" || strings.ToLower(statusFilter) == "all" {
		fmt.Println("📋 No clusters registered yet")
		fmt.Println("\n💡 Get started:")
		fmt.Println("   gitopsctl register-cluster --help")
		fmt.Println("   gitopsctl register-cluster -n mycluster -k ~/.kube/config")
	} else {
		fmt.Printf("📋 No clusters found with status '%s'\n", statusFilter)
		fmt.Println("\n💡 Try:")
		fmt.Println("   gitopsctl list-clusters --status all")
		fmt.Println("   gitopsctl list-clusters")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listClusterCmd)
	utils.AddListFlags(listClusterCmd, &listClusterOpts, "name")
	_ = listClusterCmd.RegisterFlagCompletionFunc("sort-by", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"name", "status", "registered"}, cobra.ShellCompDirectiveDefault
	})
}
