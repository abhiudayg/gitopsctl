package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestRegisterAppHelpers_CreateAndHandleExisting(t *testing.T) {
	cfg := &registrationConfig{
		appName:         "a1",
		repoURL:         "https://github.com/org/repo.git",
		branch:          "main",
		pathInRepo:      "manifests",
		clusterName:     "c1",
		interval:        "1m",
		pollingInterval: time.Minute,
		syncPolicy:      "auto",
	}
	newApp := createApplication(cfg)
	if newApp.Name != "a1" || newApp.Status != "Pending" {
		t.Fatalf("unexpected created app: %+v", newApp)
	}

	forceApp = false
	if err := handleExistingApp(false, "a1"); err != nil {
		t.Fatalf("unexpected error for non-existing app: %v", err)
	}
	if err := handleExistingApp(true, "a1"); err == nil {
		t.Fatal("expected error when app exists and force=false")
	}
	forceApp = true
	if err := handleExistingApp(true, "a1"); err != nil {
		t.Fatalf("unexpected error with force=true: %v", err)
	}
}

func TestRegisterAppHelpers_VerifyClusterAndSave(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "register-app")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	logger = zap.NewNop()

	clusters := cluster.NewClusters()
	clusters.Add(&cluster.Cluster{Name: "c1", KubeconfigPath: "/tmp/kube"})
	if err := cluster.SaveClusters(clusters, cluster.DefaultClusterConfigFile); err != nil {
		t.Fatalf("save clusters failed: %v", err)
	}
	if err := verifyClusterExists("c1"); err != nil {
		t.Fatalf("verifyClusterExists() error = %v", err)
	}

	apps := app.NewApplications()
	newApp := &app.Application{Name: "a1", RepoURL: "https://github.com/org/repo.git", Branch: "main", Path: "m", ClusterName: "c1", Interval: "1m", Status: "Pending"}
	if err := saveAndConfirmApplication(apps, newApp, false); err != nil {
		t.Fatalf("saveAndConfirmApplication() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join("configs", "applications.json")); err != nil {
		t.Fatalf("expected applications config to be written: %v", err)
	}
}
