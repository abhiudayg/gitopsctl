package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	"go.uber.org/zap"
)

func TestRegisterAppHelpers_LoadAndCheckAndDryRun(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "register-app-flow")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	apps := appcore.NewApplications()
	apps.Add(&appcore.Application{Name: "a1", Interval: "1m", PollingInterval: time.Minute})
	if err := appcore.SaveApplications(apps, appcore.DefaultAppConfigFile); err != nil {
		t.Fatalf("save apps failed: %v", err)
	}

	loaded, exists, err := loadAndCheckApplications("a1")
	if err != nil || !exists || loaded == nil {
		t.Fatalf("loadAndCheckApplications expected existing app, got exists=%v err=%v", exists, err)
	}

	if err := displayDryRunSummary(&appcore.Application{Name: "a1", RepoURL: "r", Branch: "b", Path: "p", ClusterName: "c1", Interval: "1m", SyncPolicy: "auto", Status: "Pending"}, false); err != nil {
		t.Fatalf("displayDryRunSummary() error = %v", err)
	}
}

func TestRunRegisterCommand_MissingFields(t *testing.T) {
	appName, repoURL, pathInRepo, clusterName = "", "", "", ""
	if err := runRegisterCommand(nil, nil); err == nil || !strings.Contains(err.Error(), "missing required fields") {
		t.Fatalf("expected missing required fields error, got %v", err)
	}
}
