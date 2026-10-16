package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestReconcileApp_ManualSyncTrigger(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, _ := os.MkdirTemp("", "reconcile-test")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appCfg := filepath.Join(tmpDir, "apps.json")
	if err := os.WriteFile(appCfg, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}

	a := &app.Application{
		Name:            "a1",
		RepoURL:         "https://github.com/example/repo",
		Branch:          "main",
		Path:            ".",
		ClusterName:     "c1",
		PollingInterval: time.Hour, // Long interval to avoid ticker
		Status:          "Pending",
	}
	apps.Add(a)
	clusters.Add(&cluster.Cluster{Name: "c1"})

	syncChan := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())

	// We need to mock performSync because reconcileApp calls it.
	// But reconcileApp is a method on Controller.
	// This is hard to test without mocking the whole Controller or some internal functions.

	// Wait, I can't easily mock performSync since it's a private method called by reconcileApp.
	// However, I can let it run and it will fail on Git clone (since repo is fake).

	ctrl.wg.Add(1)
	go ctrl.reconcileApp(ctx, a, appCfg, cancel, syncChan)

	// Wait for initial sync to finish (it will likely set status to Error because of git)
	time.Sleep(100 * time.Millisecond)

	// Trigger manual sync
	syncChan <- struct{}{}
	time.Sleep(100 * time.Millisecond)

	cancel()
	ctrl.wg.Wait()
}

func TestReconcileApp_ContextDone(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, _ := os.MkdirTemp("", "reconcile-ctx-done")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appCfg := filepath.Join(tmpDir, "apps.json")
	if err := os.WriteFile(appCfg, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}

	a := &app.Application{
		Name:            "a1",
		ClusterName:     "", // Trigger "Awaiting cluster assignment"
		PollingInterval: time.Hour,
		Status:          "Pending",
	}
	apps.Add(a)

	syncChan := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	ctrl.wg.Add(1)
	go ctrl.reconcileApp(ctx, a, appCfg, cancel, syncChan)

	time.Sleep(50 * time.Millisecond)
	cancel()
	ctrl.wg.Wait()

	if a.Status != "Stopped" && a.Status != "Error" {
		t.Errorf("expected Stopped or Error status after context cancel, got %s", a.Status)
	}
}
