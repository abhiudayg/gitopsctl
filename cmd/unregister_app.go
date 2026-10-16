package cmd

import (
	"fmt"
	"strings"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	unregisterAppName   string // Name of the application to unregister
	forceUnregisterApp  bool   // Force unregister without confirmation
	dryRunUnregisterApp bool   // Preview unregister without applying changes
)

var unregisterAppCmd = &cobra.Command{
	Use:     "unregister",
	GroupID: "appGroup",
	Short:   "Unregister a GitOps application from controller management",
	Long: `Removes a registered application from GitOps controller's management.

This command removes the application configuration from the controller, but does NOT:
- Delete any resources from the Kubernetes cluster
- Remove the Git repository or its contents
- Affect other applications

The application will stop being synchronized, but existing resources will remain
in the cluster until manually removed.

Use --dry-run to preview what will be unregistered.
Use --force to skip confirmation prompts.`,
	Example: `  # Unregister an application with confirmation
  gitopsctl unregister -n myapp

  # Preview unregistration without applying changes
  gitopsctl unregister -n myapp --dry-run

  # Force unregister without confirmation
  gitopsctl unregister -n myapp --force`,
	Args: cobra.NoArgs,
	RunE: runUnregisterCommand,
}

func runUnregisterCommand(cmd *cobra.Command, args []string) error {
	if err := validateUnregisterInput(); err != nil {
		return err
	}

	apps, targetApp, err := loadAndFindApplication(unregisterAppName)
	if err != nil {
		return err
	}

	if targetApp == nil {
		return handleAppNotFound(unregisterAppName)
	}

	if dryRunUnregisterApp {
		return displayUnregisterDryRun(targetApp)
	}

	if !forceUnregisterApp {
		if !confirmUnregister(targetApp) {
			fmt.Println("Operation cancelled.")
			return nil
		}
	}

	return performUnregistration(apps, targetApp)
}

func validateUnregisterInput() error {
	if strings.TrimSpace(unregisterAppName) == "" {
		return fmt.Errorf("application name is required (use --name or -n flag)")
	}

	unregisterAppName = strings.TrimSpace(unregisterAppName)

	if len(unregisterAppName) > 63 {
		return fmt.Errorf("application name too long (maximum 63 characters)")
	}

	return nil
}

func loadAndFindApplication(appName string) (*app.Applications, *app.Application, error) {
	apps, err := app.LoadApplications(app.DefaultAppConfigFile)
	if err != nil {
		logger.Error("Failed to load applications", zap.Error(err))
		return nil, nil, fmt.Errorf("failed to load applications: %w", err)
	}

	apps.RLock()
	targetApp, exists := apps.Get(appName)
	apps.RUnlock()

	if !exists {
		return apps, nil, nil
	}

	return apps, targetApp, nil
}

func handleAppNotFound(appName string) error {
	logger.Warn("Application not found in registry",
		zap.String("name", appName))

	fmt.Printf("Application '%s' is not registered. Nothing to unregister.\n", appName)
	fmt.Printf("\nTip: Use 'gitopsctl list-apps' to see all registered applications.\n")

	return nil
}

func displayUnregisterDryRun(targetApp *app.Application) error {
	fmt.Printf("\n🔍 DRY RUN - No changes will be applied\n\n")
	fmt.Printf("Action: UNREGISTER application\n")
	fmt.Printf("Application to unregister:\n")
	fmt.Printf("  Name:           %s\n", targetApp.Name)
	fmt.Printf("  Repository:     %s\n", targetApp.RepoURL)
	fmt.Printf("  Branch:         %s\n", targetApp.Branch)
	fmt.Printf("  Path:           %s\n", targetApp.Path)
	fmt.Printf("  Target Cluster: %s\n", targetApp.ClusterName)
	fmt.Printf("  Poll Interval:  %s\n", targetApp.Interval)
	fmt.Printf("  Status:         %s\n", targetApp.Status)

	if targetApp.Message != "" {
		fmt.Printf("  Last Message:   %s\n", targetApp.Message)
	}

	fmt.Printf("\nWarning: This will stop GitOps synchronization for this application.\n")
	fmt.Printf("Existing Kubernetes resources will remain in the cluster.\n")
	fmt.Printf("\nTo apply these changes, run the command again without --dry-run\n")

	return nil
}

func confirmUnregister(targetApp *app.Application) bool {
	fmt.Printf("Application to unregister:\n")
	fmt.Printf("  Name:           %s\n", targetApp.Name)
	fmt.Printf("  Repository:     %s@%s\n", targetApp.RepoURL, targetApp.Branch)
	fmt.Printf("  Target Cluster: %s\n", targetApp.ClusterName)
	fmt.Printf("  Status:         %s\n", targetApp.Status)

	fmt.Printf("\n⚠️  Warning: This will stop GitOps synchronization for this application.\n")
	fmt.Printf("Existing Kubernetes resources will remain in the cluster and must be manually removed if needed.\n\n")

	return confirmAction("Are you sure you want to unregister this application?")
}

func performUnregistration(apps *app.Applications, targetApp *app.Application) error {
	apps.Lock()
	defer apps.Unlock()

	apps.Delete(targetApp.Name)

	if err := app.SaveApplications(apps, app.DefaultAppConfigFile); err != nil {
		logger.Error("Failed to save applications after unregister",
			zap.String("app", targetApp.Name),
			zap.Error(err))
		return fmt.Errorf("failed to save applications after unregister: %w", err)
	}

	logger.Info("Application unregistered successfully",
		zap.String("name", targetApp.Name),
		zap.String("repo", targetApp.RepoURL),
		zap.String("cluster", targetApp.ClusterName))

	fmt.Printf("\n✅ Application '%s' has been unregistered successfully!\n\n", targetApp.Name)
	fmt.Printf("Summary:\n")
	fmt.Printf("  • GitOps synchronization stopped\n")
	fmt.Printf("  • Application removed from controller\n")
	fmt.Printf("  • Kubernetes resources remain in cluster '%s'\n", targetApp.ClusterName)

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  • To manually clean up resources: kubectl delete -f <manifests> --namespace <namespace>\n")
	fmt.Printf("  • To re-register: gitopsctl register-apps -n %s -r %s -p %s -c %s\n",
		targetApp.Name, targetApp.RepoURL, targetApp.Path, targetApp.ClusterName)
	fmt.Printf("  • To list remaining apps: gitopsctl list-apps\n")
	emitCommandEvent(events.TypeAppUnregistered, map[string]any{
		"app":     targetApp.Name,
		"repoURL": targetApp.RepoURL,
		"path":    targetApp.Path,
		"cluster": targetApp.ClusterName,
	})

	return nil
}

func confirmAction(message string) bool {
	fmt.Printf("%s [y/N]: ", message)
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func init() {
	rootCmd.AddCommand(unregisterAppCmd)

	unregisterAppCmd.Flags().StringVarP(&unregisterAppName, "name", "n", "",
		"Name of the application to unregister (required)")
	unregisterAppCmd.Flags().BoolVar(&forceUnregisterApp, "force", false,
		"Skip confirmation prompts")
	unregisterAppCmd.Flags().BoolVar(&dryRunUnregisterApp, "dry-run", false,
		"Preview the unregistration without applying changes")

	_ = unregisterAppCmd.MarkFlagRequired("name")
}
