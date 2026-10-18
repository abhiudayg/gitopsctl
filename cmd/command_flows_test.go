package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/utils"
	"go.uber.org/zap"
)

func TestRootLoggerInitialization(t *testing.T) {
	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE() error = %v", err)
	}
	if Logger() == nil {
		t.Fatal("expected logger to be initialized")
	}
}

func TestRunStatusAppsCommand_EmptyState(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "status-apps")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()
	statusAppOpts = utils.ListOptions{OutputFormat: "table", SortBy: "name"}

	if err := runStatusAppsCommand(nil, nil); err != nil {
		t.Fatalf("runStatusAppsCommand() error = %v", err)
	}
}

func TestRunRegisterClusterCommand_DryRun(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "register-cluster-run")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	kcfg := filepath.Join(tmpDir, "kubeconfig")
	validConfig := `
apiVersion: v1
clusters:
- cluster:
    server: https://localhost:6443
  name: test-cluster
contexts: []
current-context: ""
kind: Config
preferences: {}
users: []
`
	if err := os.WriteFile(kcfg, []byte(validConfig), 0644); err != nil {
		t.Fatalf("failed writing kubeconfig: %v", err)
	}

	clusterRegName = "c1"
	clusterKubeconfigPath = kcfg
	dryRunCluster = true
	testConnection = false
	forceCluster = false

	if err := runRegisterClusterCommand(nil, nil); err != nil {
		t.Fatalf("runRegisterClusterCommand() error = %v", err)
	}
}

func TestRunUnregisterCommand_DryRunExistingApp(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "unregister-run")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	apps := appcore.NewApplications()
	apps.Add(&appcore.Application{Name: "a1", RepoURL: "https://github.com/org/repo.git", Branch: "main", Path: "m", ClusterName: "c1", Interval: "1m", PollingInterval: time.Minute})
	if err := appcore.SaveApplications(apps, appcore.DefaultAppConfigFile); err != nil {
		t.Fatalf("save apps failed: %v", err)
	}

	unregisterAppName = "a1"
	dryRunUnregisterApp = true
	forceUnregisterApp = false
	if err := runUnregisterCommand(nil, nil); err != nil {
		t.Fatalf("runUnregisterCommand() error = %v", err)
	}
}
