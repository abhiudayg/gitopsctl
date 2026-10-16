package controller

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	gitcore "aeswibon.com/github/gitopsctl/internal/core/git"
	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

// mockApplier is a k8sApplier that delegates to a function — no real cluster needed.
type mockApplier struct {
	applyFn  func(ctx context.Context, manifestsDir, appName, clusterName string) ([]k8s.ResourceMetadata, []error)
	healthFn func(ctx context.Context, r k8s.ResourceMetadata) (string, string, error)
	pruneFn  func(ctx context.Context, appName string, previous, current []k8s.ResourceMetadata, maxDeletions int) ([]k8s.ResourceMetadata, error)
}

func (m *mockApplier) ApplyManifests(ctx context.Context, manifestsDir, appName, clusterName string) ([]k8s.ResourceMetadata, []error) {
	if m.applyFn != nil {
		return m.applyFn(ctx, manifestsDir, appName, clusterName)
	}
	return nil, nil
}

func (m *mockApplier) Prune(ctx context.Context, appName string, previous, current []k8s.ResourceMetadata, maxDeletions int) ([]k8s.ResourceMetadata, error) {
	if m.pruneFn != nil {
		return m.pruneFn(ctx, appName, previous, current, maxDeletions)
	}
	return nil, nil
}

func (m *mockApplier) GetResourceHealth(ctx context.Context, r k8s.ResourceMetadata) (string, string, error) {
	if m.healthFn != nil {
		return m.healthFn(ctx, r)
	}
	return "Healthy", "All good", nil
}

// filesystemApplier applies no k8s operations but does a real WalkDir so
// manifest-path tests exercise the actual filesystem error path.
func filesystemApplier() k8sApplier {
	return &mockApplier{applyFn: func(_ context.Context, manifestsDir, _, _ string) ([]k8s.ResourceMetadata, []error) {
		if _, err := os.Stat(manifestsDir); err != nil {
			return nil, []error{err}
		}
		return nil, nil
	}}
}

func TestPerformSync_GitFailureSetsError(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	tmpDir, _ := os.MkdirTemp("", "perform-sync")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	appCfg := filepath.Join(tmpDir, "apps.json")

	a := &app.Application{
		Name:            "a1",
		RepoURL:         "not-a-valid-git-url",
		Branch:          "main",
		Path:            "manifests",
		ClusterName:     "c1",
		PollingInterval: time.Minute,
		Status:          "Pending",
	}
	apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})

	ctrl.performSync(context.Background(), logger, a, tmpDir, nil, appCfg, "manual")

	if a.Status != "Error" {
		t.Fatalf("expected Error status after git failure, got %s", a.Status)
	}
	if a.ConsecutiveFailures == 0 {
		t.Fatal("expected consecutive failures to increment")
	}
}

func createLocalRepo(t *testing.T) string {
	dir, err := os.MkdirTemp("", "controller-repo")
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	run("branch", "-M", "master")
	return dir
}

func TestPerformSync_ManualPolicySetsOutOfSync(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	ctrl := NewController(logger, apps, cluster.NewClusters())

	repoPath := createLocalRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	workDir, _ := os.MkdirTemp("", "perform-sync-manual")
	defer func() { _ = os.RemoveAll(workDir) }()

	appCfg := filepath.Join(workDir, "apps.json")

	a := &app.Application{
		Name:            "a1",
		RepoURL:         repoPath,
		Branch:          "master",
		Path:            ".",
		ClusterName:     "c1",
		PollingInterval: time.Minute,
		SyncPolicy:      "manual",
		ApprovedGitHash: "different",
		Status:          "Pending",
	}
	apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})

	// Manual policy: returns OutOfSync before reaching apply — no k8s client needed.
	ctrl.performSync(context.Background(), logger, a, workDir, nil, appCfg, "manual")
	if a.Status != "OutOfSync" {
		t.Fatalf("expected OutOfSync, got %s", a.Status)
	}
}

func TestPerformSync_NoChangesSetsSynced(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	ctrl := NewController(logger, apps, cluster.NewClusters())

	repoPath := createLocalRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	workDir, _ := os.MkdirTemp("", "perform-sync-nochanges")
	defer func() { _ = os.RemoveAll(workDir) }()

	appCfg := filepath.Join(workDir, "apps.json")

	// First clone to workDir and read hash.
	hash, err := gitcore.CloneOrPull(context.Background(), logger, repoPath, "master", workDir)
	if err != nil {
		t.Fatalf("initial clone failed: %v", err)
	}

	a := &app.Application{
		Name:              "a1",
		RepoURL:           repoPath,
		Branch:            "master",
		Path:              ".",
		ClusterName:       "c1",
		PollingInterval:   time.Minute,
		LastSyncedGitHash: hash,
		Status:            "Pending",
	}
	apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})

	// Same hash + manual trigger = Synced without hitting apply.
	applier := &mockApplier{applyFn: func(_ context.Context, _, _, _ string) ([]k8s.ResourceMetadata, []error) { return nil, nil }}
	ctrl.performSync(context.Background(), logger, a, workDir, applier, appCfg, "manual")
	if a.Status != "Synced" {
		t.Fatalf("expected Synced on no-change manual sync, got %s", a.Status)
	}
}

func TestPerformSync_ManifestPathMissingSetsError(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	ctrl := NewController(logger, apps, cluster.NewClusters())

	repoPath := createLocalRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	workDir, _ := os.MkdirTemp("", "perform-sync-missing-manifest")
	defer func() { _ = os.RemoveAll(workDir) }()

	appCfg := filepath.Join(workDir, "apps.json")

	if _, err := gitcore.CloneOrPull(context.Background(), logger, repoPath, "master", workDir); err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	headHash, err := gitcore.GetLatestCommitHash(logger, repoPath)
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}

	a := &app.Application{
		Name:              "a1",
		RepoURL:           repoPath,
		Branch:            "master",
		Path:              "does-not-exist",
		ClusterName:       "c1",
		PollingInterval:   time.Minute,
		LastSyncedGitHash: "",
		SyncPolicy:        "auto",
		Status:            "Pending",
	}
	apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})

	// Use a filesystem-only applier — no real Kubernetes cluster required.
	ctrl.performSync(context.Background(), logger, a, workDir, filesystemApplier(), appCfg, "manual")

	if headHash == "" {
		t.Fatal("expected non-empty repo head hash")
	}
	if a.Status != "Error" {
		t.Fatalf("expected Error for missing manifest path, got %s (msg=%q)", a.Status, a.Message)
	}
	if !strings.Contains(a.Message, "no such file or directory") && !strings.Contains(a.Message, "Apply error") {
		t.Fatalf("unexpected message: %q", a.Message)
	}
}

func TestController_CheckHealth(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	application := &app.Application{
		Name:        "test-app",
		ClusterName: "c1",
		Status:      "Synced",
		AppliedResources: []app.ResourceMetadata{
			{Kind: "Deployment", Name: "d1", Namespace: "default"},
		},
	}
	apps.Add(application)

	tmpDir, _ := os.MkdirTemp("", "health-test")
	defer func() { _ = os.RemoveAll(tmpDir) }()
	appCfg := filepath.Join(tmpDir, "apps.json")
	if err := os.WriteFile(appCfg, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}

	// Case 1: Resource is Degraded
	mock := &mockApplier{
		healthFn: func(ctx context.Context, r k8s.ResourceMetadata) (string, string, error) {
			return "Degraded", "CrashLoopBackOff", nil
		},
	}

	ctrl.checkHealth(context.Background(), logger, application, mock, appCfg)
	if application.Status != "Degraded" {
		t.Errorf("expected Degraded status, got %s", application.Status)
	}
	if !strings.Contains(application.Message, "CrashLoopBackOff") {
		t.Errorf("expected message to contain CrashLoopBackOff, got %s", application.Message)
	}

	// Case 2: Resource is Progressing
	mock.healthFn = func(ctx context.Context, r k8s.ResourceMetadata) (string, string, error) {
		return "Progressing", "Wait for replicas", nil
	}
	ctrl.checkHealth(context.Background(), logger, application, mock, appCfg)
	if application.Status != "Progressing" {
		t.Errorf("expected Progressing status, got %s", application.Status)
	}

	// Case 3: Resource becomes Healthy
	mock.healthFn = func(ctx context.Context, r k8s.ResourceMetadata) (string, string, error) {
		return "Healthy", "Ready", nil
	}
	ctrl.checkHealth(context.Background(), logger, application, mock, appCfg)
	if application.Status != "Healthy" {
		t.Errorf("expected Healthy status, got %s", application.Status)
	}
}

func TestPerformSync_ClusterNotFound(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	repoPath := createLocalRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	workDir, _ := os.MkdirTemp("", "sync-no-cluster")
	defer func() { _ = os.RemoveAll(workDir) }()
	appCfg := filepath.Join(workDir, "apps.json")

	a := &app.Application{
		Name:        "a1",
		RepoURL:     repoPath,
		Branch:      "master",
		Path:        ".",
		ClusterName: "non-existent",
		Status:      "Pending",
	}
	apps.Add(a)

	ctrl.performSync(context.Background(), logger, a, workDir, nil, appCfg, "manual")
	if a.Status != "Error" || !strings.Contains(a.Message, "not found") {
		t.Fatalf("expected Cluster not found error, got %s: %s", a.Status, a.Message)
	}
}

func TestPerformSync_ManualSyncApproved(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	repoPath := createLocalRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	workDir, _ := os.MkdirTemp("", "sync-approved")
	defer func() { _ = os.RemoveAll(workDir) }()
	appCfg := filepath.Join(workDir, "apps.json")

	hash, _ := gitcore.GetLatestCommitHash(logger, repoPath)

	a := &app.Application{
		Name:            "a1",
		RepoURL:         repoPath,
		Branch:          "master",
		Path:            ".",
		ClusterName:     "c1",
		SyncPolicy:      "manual",
		ApprovedGitHash: hash,
		Status:          "Pending",
	}
	apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1", KubeconfigPath: "invalid"})

	mock := &mockApplier{
		applyFn: func(ctx context.Context, manifestsDir, appName, clusterName string) ([]k8s.ResourceMetadata, []error) {
			return []k8s.ResourceMetadata{{Kind: "ConfigMap", Name: "sample"}}, nil
		},
	}

	ctrl.performSync(context.Background(), logger, a, workDir, mock, appCfg, "manual")
	if a.Status != "Synced" {
		t.Fatalf("expected Synced for approved manual sync, got %s", a.Status)
	}
}

func TestPerformSync_ApplyFailure(t *testing.T) {
	logger := zap.NewNop()
	apps := app.NewApplications()
	clusters := cluster.NewClusters()
	ctrl := NewController(logger, apps, clusters)

	repoPath := createLocalRepo(t)
	defer func() { _ = os.RemoveAll(repoPath) }()

	workDir, _ := os.MkdirTemp("", "sync-apply-fail")
	defer func() { _ = os.RemoveAll(workDir) }()
	appCfg := filepath.Join(workDir, "apps.json")

	a := &app.Application{
		Name:        "a1",
		RepoURL:     repoPath,
		Branch:      "master",
		Path:        ".",
		ClusterName: "c1",
		Status:      "Pending",
	}
	apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})

	mock := &mockApplier{
		applyFn: func(ctx context.Context, manifestsDir, appName, clusterName string) ([]k8s.ResourceMetadata, []error) {
			return nil, []error{fmt.Errorf("apply failed")}
		},
	}

	ctrl.performSync(context.Background(), logger, a, workDir, mock, appCfg, "manual")
	if a.Status != "Error" || !strings.Contains(a.Message, "apply failed") {
		t.Fatalf("expected Apply error, got %s: %s", a.Status, a.Message)
	}
}
