package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	apiapp "aeswibon.com/github/gitopsctl/internal/api/app"
	"github.com/spf13/cobra"
)

var diffAppName string

var planSymbols = map[string]string{
	"create":    "+",
	"update":    "~",
	"prune":     "-",
	"unchanged": "=",
}

var diffAppCmd = &cobra.Command{
	Use:     "diff-app",
	GroupID: "appGroup",
	Short:   "Show what a sync would change for an application, without applying (via controller API)",
	Long: `Asks a running gitopsctl controller to render the application's manifests from Git and compare them
with the live cluster. Nothing is applied. Resources removed from Git are listed as prune candidates
only when the application was registered with --prune.`,
	Example: `
  gitopsctl diff-app -n myapp
  gitopsctl --api-url http://127.0.0.1:9090 --api-token "$TOKEN" diff-app -n myapp`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := postExpecting(fmt.Sprintf("/api/v1/applications/%s/plan", url.PathEscape(diffAppName)), nil, http.StatusOK)
		if err != nil {
			return err
		}
		var plan apiapp.PlanResponse
		if err := json.Unmarshal([]byte(body), &plan); err != nil {
			return fmt.Errorf("invalid plan response: %w", err)
		}
		printPlan(plan)
		return nil
	},
}

func printPlan(plan apiapp.PlanResponse) {
	fmt.Printf("Plan for %s at %s\n", plan.App, plan.Commit)
	counts := map[string]int{}
	for _, item := range plan.Items {
		counts[item.Action]++
		fmt.Printf("  %s %-9s %s %s/%s\n", planSymbols[item.Action], item.Action, item.Kind, item.Namespace, item.Name)
	}
	fmt.Printf("%d to create, %d to update, %d to prune, %d unchanged\n",
		counts["create"], counts["update"], counts["prune"], counts["unchanged"])
}

func init() {
	diffAppCmd.Flags().StringVarP(&diffAppName, "name", "n", "", "Application name (required)")
	_ = diffAppCmd.MarkFlagRequired("name")
	rootCmd.AddCommand(diffAppCmd)
}
