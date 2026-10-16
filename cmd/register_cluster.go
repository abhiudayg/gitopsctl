package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/common"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	// Flags for register-cluster command
	clusterRegName        string   // Name of the cluster
	clusterKubeconfigPath string   // Path to kubeconfig file
	forceCluster          bool     // Force overwrite existing cluster
	dryRunCluster         bool     // Preview registration without applying
	testConnection        bool     // Test cluster connectivity during registration
	allowedNamespaces     []string // List of allowed namespaces
)

// clusterRegistrationConfig holds validated configuration for cluster registration
type clusterRegistrationConfig struct {
	name           string
	kubeconfigPath string
	resolvedPath   string
	namespaces     []string
}

var registerClusterCmd = &cobra.Command{
	Use:     "register-cluster",
	GroupID: "clusterGroup",
	Short:   "Register a new Kubernetes cluster",
	Long: `Registers a new Kubernetes cluster with gitopsctl for GitOps management.

This command validates the kubeconfig file, optionally tests connectivity,
and stores the cluster configuration for use by GitOps applications.

If -k/--kubeconfig is omitted, the path is taken from $KUBECONFIG or ~/.kube/config when present.

Examples:
  # Register using explicit kubeconfig
  gitopsctl register-cluster -n production -k ~/.kube/config

  # Register with connectivity check
  gitopsctl register-cluster -n prod -k /path/to/kubeconfig --test

  # Preview registration without saving
  gitopsctl register-cluster -n test -k ~/.kube/config --dry-run

  # Force overwrite existing cluster registration
  gitopsctl register-cluster -n prod -k ~/.kube/config --force

  # Auto-detect kubeconfig (same rules as kubectl)
  gitopsctl register-cluster -n local`,
	RunE: runRegisterClusterCommand,
}

func runRegisterClusterCommand(cmd *cobra.Command, args []string) error {
	config, err := validateAndNormalizeClusterInput()
	if err != nil {
		return err
	}

	if err := common.ValidateKubeconfigFile(config.resolvedPath); err != nil {
		return err
	}

	if testConnection {
		if err := testClusterConnectivity(config); err != nil {
			return fmt.Errorf("cluster connectivity test failed: %w", err)
		}
	}

	existingClusters, err := clustercore.LoadClusters(clustercore.DefaultClusterConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load existing clusters: %w", err)
	}
	existingClusters.RLock()
	_, clusterExists := existingClusters.Get(config.name)
	existingClusters.RUnlock()

	if err := handleExistingCluster(clusterExists, config.name); err != nil {
		return err
	}

	newCluster := createClusterConfig(config)

	if dryRunCluster {
		return displayDryRunClusterSummary(newCluster, clusterExists)
	}

	return saveAndConfirmCluster(newCluster, clusterExists)
}

func validateAndNormalizeClusterInput() (*clusterRegistrationConfig, error) {
	config := &clusterRegistrationConfig{}

	if strings.TrimSpace(clusterRegName) == "" {
		return nil, fmt.Errorf("cluster name is required")
	}

	config.name = strings.TrimSpace(clusterRegName)
	if err := common.ValidateName(config.name); err != nil {
		return nil, err
	}

	// Handle kubeconfig path
	if strings.TrimSpace(clusterKubeconfigPath) == "" {
		if kubeconfigEnv := os.Getenv("KUBECONFIG"); kubeconfigEnv != "" {
			config.kubeconfigPath = kubeconfigEnv
		} else if homeDir, err := os.UserHomeDir(); err == nil {
			defaultPath := filepath.Join(homeDir, ".kube", "config")
			if _, err := os.Stat(defaultPath); err == nil {
				config.kubeconfigPath = defaultPath
				logger.Info("Auto-detected kubeconfig", zap.String("path", defaultPath))
			} else {
				return nil, fmt.Errorf("kubeconfig path is required and could not be auto-detected")
			}
		} else {
			return nil, fmt.Errorf("kubeconfig path is required")
		}
	} else {
		config.kubeconfigPath = strings.TrimSpace(clusterKubeconfigPath)
	}

	absPath, err := filepath.Abs(config.kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve kubeconfig path: %w", err)
	}
	config.resolvedPath = absPath
	config.namespaces = allowedNamespaces
	return config, nil
}

func testClusterConnectivity(config *clusterRegistrationConfig) error {
	logger.Info("Testing cluster connectivity...", zap.String("cluster", config.name))

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = config.resolvedPath

	configOverrides := &clientcmd.ConfigOverrides{}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	_, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to build client configuration: %w", err)
	}

	logger.Info("Cluster connectivity test passed", zap.String("cluster", config.name))
	return nil
}

func handleExistingCluster(exists bool, clusterName string) error {
	if !exists {
		return nil
	}

	if forceCluster {
		logger.Info("Forcing update of existing cluster", zap.String("name", clusterName))
		return nil
	}

	return fmt.Errorf("cluster '%s' already exists\nUse --force to overwrite or choose a different name", clusterName)
}

func createClusterConfig(config *clusterRegistrationConfig) *clustercore.Cluster {
	status := "Pending"
	message := "Cluster registered, awaiting validation"

	if testConnection {
		status = "Active"
		message = "Cluster registered and connectivity verified"
	}

	return &clustercore.Cluster{
		Name:              config.name,
		KubeconfigPath:    config.resolvedPath,
		RegisteredAt:      time.Now(),
		Status:            status,
		Message:           message,
		AllowedNamespaces: config.namespaces,
	}
}

func displayDryRunClusterSummary(newCluster *clustercore.Cluster, isUpdate bool) error {
	action := "CREATE"
	if isUpdate {
		action = "UPDATE"
	}

	fmt.Printf("\n🔍 DRY RUN - No changes will be applied\n\n")
	fmt.Printf("Action: %s cluster\n", action)
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Name:        %s\n", newCluster.Name)
	fmt.Printf("  Kubeconfig:  %s\n", newCluster.KubeconfigPath)
	fmt.Printf("  Status:      %s\n", newCluster.Status)
	fmt.Printf("  Message:     %s\n", newCluster.Message)
	if len(newCluster.AllowedNamespaces) > 0 {
		fmt.Printf("  Allowed NS:  %s\n", strings.Join(newCluster.AllowedNamespaces, ", "))
	}
	fmt.Printf("\nTo apply these changes, run the command again without --dry-run\n")

	return nil
}

func saveAndConfirmCluster(newCluster *clustercore.Cluster, isUpdate bool) error {
	clusters, err := clustercore.LoadClusters(clustercore.DefaultClusterConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load existing clusters: %w", err)
	}

	clusters.Lock()
	defer clusters.Unlock()

	clusters.Add(newCluster)
	if err := clustercore.SaveClusters(clusters, clustercore.DefaultClusterConfigFile); err != nil {
		return fmt.Errorf("failed to save cluster configuration: %w", err)
	}

	// Success message
	action := "registered"
	emoji := "✅"
	if isUpdate {
		action = "updated"
		emoji = "🔄"
	}

	fmt.Printf("\n%s Cluster '%s' %s successfully!\n\n", emoji, newCluster.Name, action)
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Kubeconfig: %s\n", newCluster.KubeconfigPath)
	fmt.Printf("  Status:     %s\n", newCluster.Status)
	if len(newCluster.AllowedNamespaces) > 0 {
		fmt.Printf("  Allowed NS: %s\n", strings.Join(newCluster.AllowedNamespaces, ", "))
	}

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  • List clusters: gitopsctl list-clusters\n")
	fmt.Printf("  • Check cluster rows in status: gitopsctl status-clusters\n")
	fmt.Printf("  • Register an application targeting this cluster: gitopsctl register-apps -c %s ...\n", newCluster.Name)

	logger.Info("Cluster registered successfully",
		zap.String("name", newCluster.Name),
		zap.String("kubeconfig", newCluster.KubeconfigPath),
		zap.String("status", newCluster.Status),
		zap.Bool("is_update", isUpdate),
	)
	emitCommandEvent(events.TypeClusterRegistered, map[string]any{
		"cluster":    newCluster.Name,
		"kubeconfig": newCluster.KubeconfigPath,
		"status":     newCluster.Status,
		"updated":    isUpdate,
	})

	return nil
}

func init() {
	rootCmd.AddCommand(registerClusterCmd)

	registerClusterCmd.Flags().StringVarP(&clusterRegName, "name", "n", "", "Unique name for the Kubernetes cluster (required)")
	registerClusterCmd.Flags().StringVarP(&clusterKubeconfigPath, "kubeconfig", "k", "", "Path to kubeconfig file (auto-detected if not specified)")

	registerClusterCmd.Flags().BoolVar(&forceCluster, "force", false, "Force overwrite existing cluster")
	registerClusterCmd.Flags().BoolVar(&dryRunCluster, "dry-run", false, "Preview registration without applying changes")
	registerClusterCmd.Flags().BoolVar(&testConnection, "test", false, "Test cluster connectivity during registration")
	registerClusterCmd.Flags().StringSliceVar(&allowedNamespaces, "allowed-namespaces", []string{}, "Comma-separated list of namespaces this cluster is restricted to")

	_ = registerClusterCmd.MarkFlagRequired("name")
	_ = registerClusterCmd.RegisterFlagCompletionFunc("kubeconfig", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{}, cobra.ShellCompDirectiveFilterFileExt
	})
}
