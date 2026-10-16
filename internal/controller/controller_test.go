package controller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/events"
	"go.uber.org/zap"
)

func TestNewController(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()

	ctrl := NewController(logger, apps, clusters)

	if ctrl == nil {
		t.Fatal("Expected controller, got nil")
	}

	// Test Start and Stop
	go func() {

		_ = ctrl.Start("/tmp/gitopsctl-test-apps.yaml")
	}()

	time.Sleep(50 * time.Millisecond)
	ctrl.Stop()
}

func TestController_AppCommands(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()

	tmpDir, err := os.MkdirTemp("", "controller-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appConfig := filepath.Join(tmpDir, "apps.yaml")
	if err := os.WriteFile(appConfig, []byte("[]"), 0644); err != nil {
		t.Fatalf("Failed to write app config: %v", err)
	}

	ctrl := NewController(logger, apps, clusters)

	// Start the command dispatcher in a goroutine
	ctrl.wg.Add(1)
	go ctrl.commandDispatcher(appConfig)

	testApp := &app.Application{
		Name:    "test-app",
		RepoURL: "https://github.com/user/repo.git",
		Status:  "Pending",
	}
	apps.Add(testApp)

	// Test ApproveSync
	ctrl.ApproveSync("test-app", "some-hash")

	// Wait a bit for async command processing
	time.Sleep(50 * time.Millisecond)

	apps.RLock()
	hash := testApp.ApprovedGitHash
	apps.RUnlock()

	if hash != "some-hash" {
		t.Errorf("Expected ApprovedGitHash 'some-hash', got %s", hash)
	}

	// Cleanup
	ctrl.Stop()
}

func TestController_PerformClusterHealthCheck(t *testing.T) {

	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	cl := &cluster.Cluster{
		Name:           "test-cluster",
		KubeconfigPath: "/non/existent/path",
	}
	clusters.Add(cl)

	ctrl.performClusterHealthCheck(context.Background(), cl)

	if cl.Status != "Error" {
		t.Errorf("Expected status Error for invalid kubeconfig, got %s", cl.Status)
	}
}

func TestPerformClusterHealthCheck_ConnectivityUnreachable(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir, err := os.MkdirTemp("", "ctrl-health-reachable")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("configs", 0o755); err != nil {
		t.Fatal(err)
	}

	kcfg := filepath.Join(tmpDir, "kube.yaml")
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:1
    insecure-skip-tls-verify: true
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
users:
- name: default
  user: {}
`
	if err := os.WriteFile(kcfg, []byte(kubeconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	cl := &cluster.Cluster{Name: "c-unreachable", KubeconfigPath: kcfg}
	ctrl.clusters.Add(cl)

	ctrl.performClusterHealthCheck(context.Background(), cl)

	if cl.Status != "Unreachable" {
		t.Fatalf("expected Unreachable, got %q (%s)", cl.Status, cl.Message)
	}
}

func TestClusterHealthChecker_ManualCheckMissingCluster(t *testing.T) {
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())

	ctrl.wg.Add(1)
	go ctrl.clusterHealthChecker()

	ctrl.clusterCommandChan <- ClusterCommand{Type: ClusterCommandCheck, ClusterName: "does-not-exist"}
	time.Sleep(50 * time.Millisecond)

	ctrl.cancel()
	close(ctrl.clusterCommandChan)
	ctrl.wg.Wait()
}

func TestReconcileApp_ClusterMissing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reconcile-missing-cluster")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	appCfg := filepath.Join(tmpDir, "apps.json")
	if err := os.WriteFile(appCfg, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())

	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncChan := make(chan struct{}, 1)

	a := &app.Application{
		Name:            "orphan",
		ClusterName:     "missing-cluster",
		RepoURL:         "https://example.com/repo.git",
		Branch:          "main",
		Path:            "manifests",
		Status:          "Pending",
		PollingInterval: time.Minute,
	}

	ctrl.apps.Add(a)
	local := *a

	ctrl.wg.Add(1)
	go ctrl.reconcileApp(appCtx, &local, appCfg, cancel, syncChan)

	storedState := func() (string, string) {
		ctrl.apps.RLock()
		defer ctrl.apps.RUnlock()
		return a.Status, a.Message
	}

	// Wait for the status to change or timeout
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status, _ := storedState(); status == "Error" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel() // Stop the loop
	ctrl.wg.Wait()

	if status, msg := storedState(); status != "Error" || !strings.Contains(msg, "does not exist") {
		t.Fatalf("unexpected app state: status=%q msg=%q", status, msg)
	}
}

type recordingEmitter struct {
	mu   sync.Mutex
	last map[string]any
}

func (r *recordingEmitter) Emit(_ context.Context, _ events.Type, data map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = data
}

func TestController_EmitNilDataPassesEmptyMap(t *testing.T) {
	rec := &recordingEmitter{}
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters(), WithEmitter(rec))
	ctrl.Emit(events.TypeAppRegistered, nil)
	if rec.last == nil {
		t.Fatal("expected normalized empty map, got nil")
	}
	if len(rec.last) != 0 {
		t.Fatalf("expected empty map, got %#v", rec.last)
	}
}

func TestController_HandleAppCommand_Register(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, err := os.MkdirTemp("", "controller-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appConfig := filepath.Join(tmpDir, "apps.yaml")

	cmd := AppCommand{
		Type:    AppCommandStart,
		AppName: "new-app",
	}

	// This should log a warning but not crash if the app doesn't exist
	ctrl.handleAppCommand(cmd, appConfig)
}

func TestController_Notify(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	testApp := &app.Application{
		Name:       "test-app",
		WebhookURL: "http://example.com/webhook",
	}

	// This just triggers a goroutine, hard to test without a server but we cover the lines
	ctrl.notify(testApp, "", "")
}

func TestController_Emit(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	// Test public Emit
	ctrl.Emit(events.TypeAppRegistered, map[string]any{"app": "test"})
}

func TestWithEmitterOption(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters, WithEmitter(events.Noop{}))
	if ctrl == nil {
		t.Fatal("expected controller instance")
	}
}

func TestController_InternalState(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	// These are mostly to cover the setup/cleanup paths
	ctrl.stopAllAppGoroutines()
}

func TestController_CommandEnqueueHelpers(t *testing.T) {
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())

	ctrl.StartApp("a1")
	if cmd := <-ctrl.appCommandChan; cmd.Type != AppCommandStart || cmd.AppName != "a1" {
		t.Fatalf("unexpected start command: %+v", cmd)
	}

	ctrl.StopApp("a1")
	if cmd := <-ctrl.appCommandChan; cmd.Type != AppCommandStop || cmd.AppName != "a1" {
		t.Fatalf("unexpected stop command: %+v", cmd)
	}

	ctrl.TriggerSync("a1")
	if cmd := <-ctrl.appCommandChan; cmd.Type != AppCommandSync || cmd.AppName != "a1" {
		t.Fatalf("unexpected sync command: %+v", cmd)
	}

	ctrl.TriggerClusterHealthCheck("c1")
	if cmd := <-ctrl.clusterCommandChan; cmd.Type != ClusterCommandCheck || cmd.ClusterName != "c1" {
		t.Fatalf("unexpected cluster command: %+v", cmd)
	}
}

func TestController_HandleAppCommandApprove_TriggersSync(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, err := os.MkdirTemp("", "controller-approve")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appConfig := filepath.Join(tmpDir, "apps.json")

	testApp := &app.Application{Name: "a1", SyncPolicy: "manual"}
	apps.Add(testApp)

	ctrl.handleAppCommand(AppCommand{
		Type:    AppCommandApprove,
		AppName: "a1",
		Data:    map[string]any{"commitHash": "abc123"},
	}, appConfig)

	if testApp.ApprovedGitHash != "abc123" {
		t.Fatalf("expected ApprovedGitHash to be updated, got %q", testApp.ApprovedGitHash)
	}

	// Approve path should enqueue an immediate sync request.
	gotSync := false
	for len(ctrl.appCommandChan) > 0 {
		cmd := <-ctrl.appCommandChan
		if cmd.Type == AppCommandSync && cmd.AppName == "a1" {
			gotSync = true
		}
	}
	if !gotSync {
		t.Fatal("expected approve command to enqueue sync command")
	}
}

func TestController_SaveAppStatus_UpdatesSharedState(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, err := os.MkdirTemp("", "controller-status")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appConfig := filepath.Join(tmpDir, "apps.json")

	original := &app.Application{Name: "a1", Status: "Pending"}
	apps.Add(original)

	appCopy := &app.Application{Name: "a1", Status: "Synced", Message: "ok", LastSyncedGitHash: "hash1"}
	ctrl.saveAppStatus(appCopy, appConfig, true)

	updated, ok := apps.Get("a1")
	if !ok {
		t.Fatal("expected app to exist after save")
	}
	if updated.Status != "Synced" || updated.LastSyncedGitHash != "hash1" {
		t.Fatalf("unexpected app status after save: %+v", updated)
	}
}
