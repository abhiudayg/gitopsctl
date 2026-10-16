package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestHandleAppCommand_StartMissingClusterUpdatesApp(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, _ := os.MkdirTemp("", "handle-start")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	appCfg := filepath.Join(tmpDir, "apps.json")

	a := &app.Application{
		Name:            "orphan",
		RepoURL:         "https://github.com/example/repo.git",
		ClusterName:     "missing",
		PollingInterval: time.Minute,
		Status:          "Pending",
	}
	apps.Add(a)

	ctrl.handleAppCommand(AppCommand{Type: AppCommandStart, AppName: "orphan"}, appCfg)

	apps.RLock()
	got, _ := apps.Get("orphan")
	apps.RUnlock()
	if got == nil || got.Status != "Error" {
		t.Fatalf("expected Error status for missing cluster, got %+v", got)
	}
}

func TestHandleAppCommand_StopNonRunning_LogsOnly(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)
	appCfg := filepath.Join(t.TempDir(), "apps.json")

	ctrl.handleAppCommand(AppCommand{Type: AppCommandStop, AppName: "none"}, appCfg)
}

func TestHandleAppCommand_SyncNonRunning_LogsOnly(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)
	appCfg := filepath.Join(t.TempDir(), "apps.json")

	ctrl.handleAppCommand(AppCommand{Type: AppCommandSync, AppName: "none"}, appCfg)
}

func TestHandleAppCommand_ApproveInvalidCommit_NoPanic(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	apps.Add(&app.Application{Name: "a1", Interval: "1m"})
	ctrl := NewController(logger, apps, cluster.NewClusters())
	appCfg := filepath.Join(t.TempDir(), "apps.json")

	ctrl.handleAppCommand(AppCommand{
		Type:    AppCommandApprove,
		AppName: "a1",
		Data:    map[string]any{"commitHash": ""},
	}, appCfg)

	ctrl.handleAppCommand(AppCommand{
		Type:    AppCommandApprove,
		AppName: "missing",
		Data:    map[string]any{"commitHash": "abc"},
	}, appCfg)
}

func TestReloadApplicationsAddsUpdatesAndRemoves(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	apps.Add(&app.Application{Name: "keep", RepoURL: "old", Branch: "main", Path: ".", ClusterName: "prod", Interval: "1m"})
	apps.Add(&app.Application{Name: "remove", RepoURL: "old", Branch: "main", Path: ".", ClusterName: "prod", Interval: "1m"})

	ctrl := NewController(logger, apps, cluster.NewClusters())
	appCfg := filepath.Join(t.TempDir(), "apps.json")

	loaded := app.NewApplications()
	loaded.Add(&app.Application{Name: "keep", RepoURL: "new", Branch: "main", Path: ".", ClusterName: "prod", Interval: "1m"})
	loaded.Add(&app.Application{Name: "add", RepoURL: "new", Branch: "main", Path: ".", ClusterName: "prod", Interval: "1m"})
	if err := app.SaveApplications(loaded, appCfg); err != nil {
		t.Fatalf("SaveApplications: %v", err)
	}

	ctrl.reloadApplications(appCfg)

	if _, ok := apps.Get("remove"); ok {
		t.Fatal("expected removed app to be deleted")
	}
	if got, ok := apps.Get("keep"); !ok || got.RepoURL != "new" {
		t.Fatalf("expected updated app, got %#v", got)
	}
	if _, ok := apps.Get("add"); !ok {
		t.Fatal("expected added app")
	}

	seen := map[AppCommandType]int{}
	for i := 0; i < 3; i++ {
		select {
		case cmd := <-ctrl.appCommandChan:
			seen[cmd.Type]++
		default:
			t.Fatalf("expected command %d", i+1)
		}
	}
	if seen[AppCommandStart] != 2 || seen[AppCommandStop] != 1 {
		t.Fatalf("unexpected commands: %#v", seen)
	}
}

func TestReloadApplicationsInvalidFileLeavesAppsUnchanged(t *testing.T) {
	apps := app.NewApplications()
	apps.Add(&app.Application{Name: "keep", Interval: "1m"})
	ctrl := NewController(zap.NewNop(), apps, cluster.NewClusters())

	appCfg := filepath.Join(t.TempDir(), "apps.json")
	if err := os.WriteFile(appCfg, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}

	ctrl.reloadApplications(appCfg)
	if apps.Len() != 1 {
		t.Fatalf("expected apps to remain unchanged, got %d", apps.Len())
	}
}
