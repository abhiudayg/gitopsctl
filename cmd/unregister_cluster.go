package cmd

import (
	"fmt"

	"abhiudayg.com/github/gitopsctl/internal/common"
	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	clusterUnregName       string
	forceUnregisterCluster bool
)

var unregisterClusterCmd = &cobra.Command{
	Use:     "unregister-cluster",
	GroupID: "clusterGroup",
	Short:   "Unregister a Kubernetes cluster from gitopsctl management",
	Long: `Removes a registered Kubernetes cluster from gitopsctl's management.

This command removes the cluster configuration from the controller, but does not
affect the actual Kubernetes cluster. Any applications associated with this
cluster will become dysfunctional until they are updated to reference a valid cluster.

Use the --force flag to skip confirmation prompts.`,
	Example: `  # Unregister a cluster with confirmation
  gitopsctl unregister-cluster --name my-cluster

  # Unregister a cluster without confirmation
  gitopsctl unregister-cluster --name my-cluster --force`,
	Args: cobra.NoArgs,
	RunE: unregisterCluster,
}

func unregisterCluster(cm *cobra.Command, args []string) error {
	if err := common.ValidateName(clusterUnregName); err != nil {
		return err
	}

	clusters, err := cluster.LoadClusters(cluster.DefaultClusterConfigFile)
	if err != nil {
		logger.Error("Failed to load cluster configurations",
			zap.Error(err),
			zap.String("file", cluster.DefaultClusterConfigFile))
		return fmt.Errorf("failed to load cluster configurations: %w", err)
	}

	clusters.Lock()
	defer clusters.Unlock()

	clusterConfig, exists := clusters.Get(clusterUnregName)
	if !exists {
		logger.Warn("Cluster not found in registry",
			zap.String("name", clusterUnregName))
		fmt.Printf("Cluster '%s' is not registered. Nothing to unregister.\n", clusterUnregName)
		return nil
	}

	if !forceUnregisterCluster {
		fmt.Printf("Cluster to unregister:\n")
		fmt.Printf("  Name: %s\n", clusterUnregName)
		if clusterConfig != nil {
			fmt.Printf("  Status: Registered\n")
		}
		fmt.Printf("\nWarning: Applications associated with this cluster may become dysfunctional.\n")

		if !common.ConfirmAction("Are you sure you want to unregister this cluster?") {
			fmt.Println("Operation cancelled.")
			return nil
		}
	}

	clusters.Delete(clusterUnregName)

	// Save clusters with better error handling
	if err := cluster.SaveClusters(clusters, cluster.DefaultClusterConfigFile); err != nil {
		logger.Error("Failed to save cluster configuration after unregister",
			zap.String("cluster", clusterUnregName),
			zap.Error(err))
		return fmt.Errorf("failed to save clusters after unregister: %w", err)
	}

	logger.Info("Cluster unregistered successfully",
		zap.String("name", clusterUnregName))
	fmt.Printf("✓ Cluster '%s' has been unregistered successfully.\n", clusterUnregName)
	emitCommandEvent(events.TypeClusterUnregistered, map[string]any{
		"cluster": clusterUnregName,
	})

	return nil
}

func init() {
	rootCmd.AddCommand(unregisterClusterCmd)

	unregisterClusterCmd.Flags().StringVarP(&clusterUnregName, "name", "n", "",
		"Name of the cluster to unregister (required)")
	unregisterClusterCmd.Flags().BoolVarP(&forceUnregisterCluster, "force", "f", false,
		"Skip confirmation prompts")

	_ = unregisterClusterCmd.MarkFlagRequired("name")
}
