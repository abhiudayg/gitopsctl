package cmd

import (
	"fmt"
	"strings"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/common"
	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	// Flags for the register command
	appName       string // Name of the application
	repoURL       string // Git repository URL
	branch        string // Branch in the repository (optional, default is "main")
	pathInRepo    string // Path to Kubernetes manifests in the repository
	clusterName   string // Name of the Kubernetes cluster
	interval      string // Polling interval for Git repository
	dryRunApp     bool   // Preview changes without applying them
	forceApp      bool   // Force overwrite existing application
	syncPolicy    string // Synchronization policy (auto or manual)
	prune         bool
	webhookURL    string // Webhook URL for notifications
	webhookSecret string // Webhook secret for signing
)

// registrationConfig holds validated configuration for app registration
type registrationConfig struct {
	appName         string
	repoURL         string
	branch          string
	pathInRepo      string
	clusterName     string
	interval        string
	pollingInterval time.Duration
	syncPolicy      string
	prune           bool
	webhookURL      string
	webhookSecret   string
}

var registerCmd = &cobra.Command{
	Use:     "register-apps",
	GroupID: "appGroup",
	Short:   "Register a new GitOps application",
	Long: `Registers a new application to be managed by the GitOps controller.

This command defines where the Kubernetes manifests are located in Git
and which Kubernetes cluster they should be applied to.

The controller will periodically poll the Git repository and apply any
changes to the specified Kubernetes cluster.`,
	Example: `  # Register a simple application
  gitopsctl register-apps -n myapp -r https://github.com/user/repo.git -p k8s/prod -c production

  # Register with custom branch and interval
  gitopsctl register-apps -n myapp -r git@github.com:user/repo.git -b develop -p manifests -c staging -i 10m

  # Preview registration without saving (dry run)
  gitopsctl register-apps -n myapp -r https://github.com/user/repo.git -p k8s -c prod --dry-run

  # Force overwrite existing application
  gitopsctl register-apps -n myapp -r https://github.com/user/repo.git -p k8s -c prod --force`,
	Args: cobra.NoArgs,
	RunE: runRegisterCommand,
}

func runRegisterCommand(cobraCmd *cobra.Command, args []string) error {
	config, err := validateAndNormalizeInput()
	if err != nil {
		return err
	}

	if err := verifyClusterExists(config.clusterName); err != nil {
		return err
	}

	apps, appExists, err := loadAndCheckApplications(config.appName)
	if err != nil {
		return err
	}

	if err := handleExistingApp(appExists, config.appName); err != nil {
		return err
	}

	newApp := createApplication(config)

	if dryRunApp {
		return displayDryRunSummary(newApp, appExists)
	}

	return saveAndConfirmApplication(apps, newApp, appExists)
}

func validateAndNormalizeInput() (*registrationConfig, error) {
	config := &registrationConfig{}

	requiredFields := map[string]string{
		"application name": appName,
		"repository URL":   repoURL,
		"path":             pathInRepo,
		"cluster name":     clusterName,
	}

	var missingFields []string
	for field, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			missingFields = append(missingFields, field)
		}
	}

	if len(missingFields) > 0 {
		return nil, fmt.Errorf("missing required fields: %s", strings.Join(missingFields, ", "))
	}

	config.appName = strings.TrimSpace(appName)
	config.repoURL = strings.TrimSpace(repoURL)
	config.branch = strings.TrimSpace(branch)
	if config.branch == "" {
		config.branch = "main"
	}
	config.clusterName = strings.TrimSpace(clusterName)
	config.interval = strings.TrimSpace(interval)
	if config.interval == "" {
		config.interval = "5m"
	}

	if !common.IsValidGitURL(config.repoURL) {
		return nil, fmt.Errorf("invalid repository URL format: %s\nMust be a valid HTTPS or SSH Git URL", config.repoURL)
	}

	config.pathInRepo = strings.Trim(strings.TrimSpace(pathInRepo), "/")
	if config.pathInRepo == "" {
		return nil, fmt.Errorf("path cannot be empty or contain only slashes")
	}

	if err := common.ValidateName(config.appName); err != nil {
		return nil, err
	}

	parsedInterval, err := time.ParseDuration(config.interval)
	if err != nil {
		return nil, fmt.Errorf("invalid interval format: %w\nExamples: 30s, 5m, 1h", err)
	}
	if parsedInterval < 10*time.Second {
		return nil, fmt.Errorf("interval must be at least 10 seconds to avoid excessive polling")
	}
	if parsedInterval > 24*time.Hour {
		return nil, fmt.Errorf("interval cannot exceed 24 hours")
	}
	config.pollingInterval = parsedInterval
	config.syncPolicy = strings.ToLower(strings.TrimSpace(syncPolicy))
	if config.syncPolicy == "" {
		config.syncPolicy = "auto"
	}
	if config.syncPolicy != "auto" && config.syncPolicy != "manual" {
		return nil, fmt.Errorf("invalid sync policy: %s (must be 'auto' or 'manual')", config.syncPolicy)
	}
	config.prune = prune
	config.webhookURL = strings.TrimSpace(webhookURL)
	config.webhookSecret = strings.TrimSpace(webhookSecret)

	return config, nil
}

func verifyClusterExists(clusterName string) error {
	clusters, err := cluster.LoadClusters(cluster.DefaultClusterConfigFile)
	if err != nil {
		logger.Error("Failed to load cluster configurations", zap.Error(err))
		return fmt.Errorf("failed to load cluster configurations: %w", err)
	}

	clusters.RLock()
	defer clusters.RUnlock()

	if _, exists := clusters.Get(clusterName); !exists {
		return fmt.Errorf("cluster '%s' not found\nUse 'gitopsctl list-clusters' to see registered clusters or 'gitopsctl register-cluster' to add one", clusterName)
	}

	return nil
}

func loadAndCheckApplications(appName string) (*app.Applications, bool, error) {
	apps, err := app.LoadApplications(app.DefaultAppConfigFile)
	if err != nil {
		logger.Error("Failed to load applications", zap.Error(err))
		return nil, false, fmt.Errorf("failed to load applications: %w", err)
	}

	apps.RLock()
	_, exists := apps.Get(appName)
	apps.RUnlock()

	return apps, exists, nil
}

func handleExistingApp(exists bool, appName string) error {
	if !exists {
		return nil
	}

	if forceApp {
		logger.Info("Forcing update of existing application", zap.String("name", appName))
		fmt.Printf("⚠️  Overwriting existing application '%s' (--force flag used)\n", appName)
		return nil
	}

	return fmt.Errorf("application '%s' already exists\nUse --force to overwrite or choose a different name", appName)
}

func createApplication(config *registrationConfig) *app.Application {
	return &app.Application{
		Name:                config.appName,
		RepoURL:             config.repoURL,
		Branch:              config.branch,
		Path:                config.pathInRepo,
		ClusterName:         config.clusterName,
		Interval:            config.interval,
		PollingInterval:     config.pollingInterval,
		Status:              "Pending",
		Message:             "Application registered, awaiting first sync",
		ConsecutiveFailures: 0,
		SyncPolicy:          config.syncPolicy,
		Prune:               config.prune,
		WebhookURL:          config.webhookURL,
		WebhookSecret:       config.webhookSecret,
	}
}

func displayDryRunSummary(newApp *app.Application, isUpdate bool) error {
	action := "CREATE"
	if isUpdate {
		action = "UPDATE"
	}

	fmt.Printf("\n🔍 DRY RUN - No changes will be applied\n\n")
	fmt.Printf("Action: %s application\n", action)
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Name:           %s\n", newApp.Name)
	fmt.Printf("  Repository:     %s\n", newApp.RepoURL)
	fmt.Printf("  Branch:         %s\n", newApp.Branch)
	fmt.Printf("  Path:           %s\n", newApp.Path)
	fmt.Printf("  Cluster:        %s\n", newApp.ClusterName)
	fmt.Printf("  Poll Interval:  %s\n", newApp.Interval)
	fmt.Printf("  Sync Policy:    %s\n", newApp.SyncPolicy)
	fmt.Printf("  Prune:          %t\n", newApp.Prune)
	fmt.Printf("  Status:         %s\n", newApp.Status)

	if isUpdate {
		fmt.Printf("\n⚠️  This will overwrite the existing application configuration.\n")
	}

	fmt.Printf("\nTo apply these changes, run the command again without --dry-run\n")

	return nil
}

func saveAndConfirmApplication(apps *app.Applications, newApp *app.Application, isUpdate bool) error {
	apps.Lock()
	defer apps.Unlock()

	apps.Add(newApp)

	if err := app.SaveApplications(apps, app.DefaultAppConfigFile); err != nil {
		logger.Error("Failed to save application configuration",
			zap.String("app", newApp.Name),
			zap.Error(err))
		return fmt.Errorf("failed to save application configuration: %w", err)
	}

	action := "registered"
	emoji := "✅"
	if isUpdate {
		action = "updated"
		emoji = "🔄"
	}

	fmt.Printf("\n%s Application '%s' %s successfully!\n\n", emoji, newApp.Name, action)
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Repository:     %s@%s\n", newApp.RepoURL, newApp.Branch)
	fmt.Printf("  Path:           %s\n", newApp.Path)
	fmt.Printf("  Target Cluster: %s\n", newApp.ClusterName)
	fmt.Printf("  Poll Interval:  %s\n", newApp.Interval)
	fmt.Printf("  Sync Policy:    %s\n", newApp.SyncPolicy)
	fmt.Printf("  Prune:          %t\n", newApp.Prune)
	fmt.Printf("  Status:         %s\n", newApp.Status)

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  • Monitor sync status: gitopsctl status-apps\n")
	fmt.Printf("  • Run controller + API: gitopsctl start\n")
	fmt.Printf("  • Trigger manual sync (controller running): gitopsctl sync-app -n %s\n", newApp.Name)

	logger.Info("Application registered successfully",
		zap.String("name", newApp.Name),
		zap.String("repo", newApp.RepoURL),
		zap.String("branch", newApp.Branch),
		zap.String("path", newApp.Path),
		zap.String("cluster", newApp.ClusterName),
		zap.String("interval", newApp.Interval),
		zap.Bool("is_update", isUpdate),
	)
	emitCommandEvent(events.TypeAppRegistered, map[string]any{
		"app":        newApp.Name,
		"repoURL":    newApp.RepoURL,
		"branch":     newApp.Branch,
		"path":       newApp.Path,
		"cluster":    newApp.ClusterName,
		"interval":   newApp.Interval,
		"syncPolicy": newApp.SyncPolicy,
		"updated":    isUpdate,
	})

	return nil
}

func init() {
	rootCmd.AddCommand(registerCmd)

	registerCmd.Flags().StringVarP(&appName, "name", "n", "",
		"Unique name for the application (required)")
	registerCmd.Flags().StringVarP(&repoURL, "repo", "r", "",
		"Git repository URL (required)")
	registerCmd.Flags().StringVarP(&pathInRepo, "path", "p", "",
		"Path to Kubernetes manifests in the repository (required)")
	registerCmd.Flags().StringVarP(&clusterName, "cluster", "c", "",
		"Name of the target Kubernetes cluster (required)")

	registerCmd.Flags().StringVarP(&branch, "branch", "b", "main",
		"Branch in the repository")
	registerCmd.Flags().StringVarP(&interval, "interval", "i", "5m",
		"Polling interval (min: 10s, max: 24h)")
	registerCmd.Flags().StringVar(&syncPolicy, "sync-policy", "auto",
		"Synchronization policy (auto or manual)")
	registerCmd.Flags().BoolVar(&prune, "prune", false,
		"Delete resources owned by this app that were removed from Git")
	registerCmd.Flags().StringVar(&webhookURL, "webhook-url", "",
		"Webhook URL for sync notifications")
	registerCmd.Flags().StringVar(&webhookSecret, "webhook-secret", "",
		"Optional secret to sign webhook payloads")

	registerCmd.Flags().BoolVar(&dryRunApp, "dry-run", false,
		"Preview the registration without applying changes")
	registerCmd.Flags().BoolVar(&forceApp, "force", false,
		"Force overwrite existing application")

	_ = registerCmd.MarkFlagRequired("name")
	_ = registerCmd.MarkFlagRequired("repo")
	_ = registerCmd.MarkFlagRequired("path")
	_ = registerCmd.MarkFlagRequired("cluster")
}
