package cmd

import (
	"os"
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	"go.uber.org/zap"
)

func TestUnregisterHelpers_LoadFindDryRunAndPerform(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "unregister-app")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	apps := appcore.NewApplications()
	apps.Add(&appcore.Application{Name: "a1", RepoURL: "https://github.com/org/repo.git", Branch: "main", Path: "manifests", ClusterName: "c1", Interval: "1m"})
	if err := appcore.SaveApplications(apps, appcore.DefaultAppConfigFile); err != nil {
		t.Fatalf("save apps failed: %v", err)
	}

	loaded, target, err := loadAndFindApplication("a1")
	if err != nil || target == nil || loaded == nil {
		t.Fatalf("loadAndFindApplication failed: target=%v err=%v", target, err)
	}
	if err := displayUnregisterDryRun(target); err != nil {
		t.Fatalf("displayUnregisterDryRun() error = %v", err)
	}
	if err := performUnregistration(loaded, target); err != nil {
		t.Fatalf("performUnregistration() error = %v", err)
	}
}

func TestRunUnregisterCommand_DryRunMissingApp(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "unregister-app-run")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	unregisterAppName = "missing"
	forceUnregisterApp = true
	dryRunUnregisterApp = false
	if err := runUnregisterCommand(nil, nil); err != nil {
		t.Fatalf("runUnregisterCommand expected nil for missing app path, got %v", err)
	}
}

func TestRunUnregisterCommand_ForceRemovesApp(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "unregister-app-force")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	apps := appcore.NewApplications()
	apps.Add(&appcore.Application{Name: "delme", RepoURL: "https://example.com/r.git", Branch: "main", Path: "p", ClusterName: "c1", Interval: "1m"})
	if err := appcore.SaveApplications(apps, appcore.DefaultAppConfigFile); err != nil {
		t.Fatal(err)
	}

	unregisterAppName = "delme"
	forceUnregisterApp = true
	dryRunUnregisterApp = false
	if err := runUnregisterCommand(nil, nil); err != nil {
		t.Fatalf("runUnregisterCommand: %v", err)
	}

	reloaded, err := appcore.LoadApplications(appcore.DefaultAppConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.RLock()
	_, exists := reloaded.Get("delme")
	reloaded.RUnlock()
	if exists {
		t.Fatal("expected app to be removed")
	}
}
