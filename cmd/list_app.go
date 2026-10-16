package cmd

import (
	"fmt"
	"sort"
	"strings"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/utils"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var listAppOpts utils.ListOptions

var listAppCmd = &cobra.Command{
	Use:     "list-apps",
	GroupID: "appGroup",
	Short:   "List all registered GitOps applications",
	Long: `Displays information about all registered GitOps applications registered with gitopsctl.

This command shows application names, repository URLs, branches, paths, clusters, and sync intervals.
You can filter, sort, and format the output according to your needs.`,
	Example: `
  # List all registered applications in table format
  gitopsctl list-apps

  # List only active applications
  gitopsctl list-apps --status active

  # List applications sorted by name
  gitopsctl list-apps --sort-by name

  # Show detailed information for each application
  gitopsctl list-apps --details

  # Output as JSON for automation
  gitopsctl list-apps --output json

  # Compact view without headers
  gitopsctl list-apps --no-header
	`,
	RunE: runListAppsCommand,
}

func runListAppsCommand(cmd *cobra.Command, args []string) error {
	return utils.RunListCommand(
		logger,
		listAppOpts,
		loadAppsForList,
		filterAppsForList,
		sortAppsForList,
		handleEmptyAppsForList,
	)
}

// loadAppsForList loads applications and converts them to cliutils.Renderable.
func loadAppsForList() ([]utils.Renderable, error) {
	apps, err := app.LoadApplications(app.DefaultAppConfigFile)
	if err != nil {
		logger.Error("Failed to load applications", zap.Error(err))
		return nil, fmt.Errorf("failed to load applications: %w", err)
	}

	if len(apps.List()) == 0 {
		return nil, fmt.Errorf("no applications registered")
	}

	logger.Info("Loaded applications successfully", zap.Int("count", len(apps.List())))
	renderableApps := make([]utils.Renderable, len(apps.List()))
	for i, a := range apps.List() {
		renderableApps[i] = a
	}
	return renderableApps, nil
}

// filterAppsForList filters a slice of Renderable (app.Application) by status.
func filterAppsForList(items []utils.Renderable, statusFilter string) []utils.Renderable {
	if statusFilter == "" || strings.ToLower(statusFilter) == "all" {
		return items
	}

	var filtered []utils.Renderable
	targetStatus := strings.ToLower(statusFilter)

	for _, item := range items {
		if appItem, ok := item.(*app.Application); ok {
			if strings.ToLower(appItem.Status) == targetStatus {
				filtered = append(filtered, appItem)
			}
		}
	}
	return filtered
}

// sortAppsForList sorts a slice of Renderable (app.Application) by a given field.
func sortAppsForList(items []utils.Renderable, sortField string) {
	sort.Slice(items, func(i, j int) bool {
		appI := items[i].(*app.Application)
		appJ := items[j].(*app.Application)

		switch strings.ToLower(sortField) {
		case "status":
			if appI.Status == appJ.Status {
				return appI.Name < appJ.Name
			}
			return appI.Status < appJ.Status
		case "branch":
			if appI.Branch == appJ.Branch {
				return appI.Name < appJ.Name
			}
			return appI.Branch < appJ.Branch
		default: // Default to name
			return appI.Name < appJ.Name
		}
	})
}

// handleEmptyAppsForList displays a message if no applications are found.
func handleEmptyAppsForList(statusFilter string) error {
	if statusFilter == "" || strings.ToLower(statusFilter) == "all" {
		fmt.Println("📋 No apps registered yet")
		fmt.Println("\n💡 Get started:")
		fmt.Println("   gitopsctl register-apps --help")
		fmt.Println("   gitopsctl register-cluster -n mycluster -k ~/.kube/config")
		fmt.Println("   gitopsctl register-apps -n myapp -c mycluster -r <repo-url> -p <path>")
	} else {
		fmt.Printf("📋 No apps found with status '%s'\n", statusFilter)
		fmt.Println("\n💡 Try:")
		fmt.Println("   gitopsctl list-apps --status all")
		fmt.Println("   gitopsctl list-apps")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listAppCmd)
	utils.AddListFlags(listAppCmd, &listAppOpts, "name")
	_ = listAppCmd.RegisterFlagCompletionFunc("sort-by", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"name", "status", "branch"}, cobra.ShellCompDirectiveDefault
	})
}
