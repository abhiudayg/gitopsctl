package controller

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

type syncFixture struct {
	ctrl    *Controller
	app     *app.Application
	workDir string
	cfg     string
	applies *atomic.Int32
	applier *mockApplier
}

func newSyncFixture(t *testing.T, repo string) *syncFixture {
	t.Helper()
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})
	a := &app.Application{Name: "a1", RepoURL: repo, Branch: "master", Path: ".", ClusterName: "c1", Status: "Pending"}
	ctrl.apps.Add(a)

	applies := &atomic.Int32{}
	return &syncFixture{
		ctrl: ctrl, app: a, workDir: t.TempDir(), cfg: filepath.Join(t.TempDir(), "apps.json"), applies: applies,
		applier: &mockApplier{applyFn: func(context.Context, string, string, string) ([]k8s.ResourceMetadata, []error) {
			applies.Add(1)
			return []k8s.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "sample"}}, nil
		}},
	}
}

func (f *syncFixture) sync(ctx context.Context, trigger string) {
	f.ctrl.performSync(ctx, zap.NewNop(), f.app, f.workDir, f.applier, f.cfg, trigger)
}

func commitToRepo(t *testing.T, repo, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "update " + file}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestPerformSync_PollWithUnchangedCommitDoesNotReapply(t *testing.T) {
	repo := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	f := newSyncFixture(t, repo)

	f.sync(context.Background(), "initial")
	f.sync(context.Background(), "poll")
	f.sync(context.Background(), "poll")

	if got := f.applies.Load(); got != 1 {
		t.Fatalf("expected exactly one apply for an unchanged commit, got %d", got)
	}
	if f.app.Status != "Synced" {
		t.Fatalf("status = %s", f.app.Status)
	}
}

func TestPerformSync_NewCommitBetweenPollsIsPickedUp(t *testing.T) {
	repo := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	f := newSyncFixture(t, repo)

	f.sync(context.Background(), "initial")
	first := f.app.LastSyncedGitHash
	commitToRepo(t, repo, "extra.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: extra\n")
	f.sync(context.Background(), "poll")

	if f.applies.Load() != 2 || f.app.LastSyncedGitHash == first || f.app.LatestGitHash != f.app.LastSyncedGitHash {
		t.Fatalf("new commit not applied: applies=%d first=%s last=%s latest=%s", f.applies.Load(), first, f.app.LastSyncedGitHash, f.app.LatestGitHash)
	}
}

func TestPerformSync_RecoversAfterApplyFailure(t *testing.T) {
	repo := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	f := newSyncFixture(t, repo)

	fail := true
	good := f.applier.applyFn
	f.applier.applyFn = func(ctx context.Context, dir, appName, cluster string) ([]k8s.ResourceMetadata, []error) {
		if fail {
			return nil, []error{errors.New("apiserver unavailable")}
		}
		return good(ctx, dir, appName, cluster)
	}

	f.sync(context.Background(), "initial")
	if f.app.Status != "Error" || f.app.ConsecutiveFailures != 1 || f.app.LastSyncedGitHash != "" {
		t.Fatalf("failure not recorded: %s failures=%d hash=%q", f.app.Status, f.app.ConsecutiveFailures, f.app.LastSyncedGitHash)
	}

	fail = false
	f.sync(context.Background(), "poll")
	if f.app.Status != "Synced" || f.app.ConsecutiveFailures != 0 || f.app.LastSyncedGitHash == "" {
		t.Fatalf("did not recover: %s failures=%d hash=%q", f.app.Status, f.app.ConsecutiveFailures, f.app.LastSyncedGitHash)
	}
}

func TestPerformSync_InterruptedApplyIsRetriedOnNextPoll(t *testing.T) {
	repo := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	f := newSyncFixture(t, repo)

	cancelled, cancel := context.WithCancel(context.Background())
	f.applier.applyFn = func(ctx context.Context, _, _, _ string) ([]k8s.ResourceMetadata, []error) {
		cancel()
		return nil, []error{ctx.Err()}
	}
	f.sync(cancelled, "initial")
	if f.app.Status != "Error" || f.app.LastSyncedGitHash != "" {
		t.Fatalf("interrupted apply must not be recorded as synced: %s %q", f.app.Status, f.app.LastSyncedGitHash)
	}

	f.applier.applyFn = func(context.Context, string, string, string) ([]k8s.ResourceMetadata, []error) { return nil, nil }
	f.sync(context.Background(), "poll")
	if f.app.Status != "Synced" {
		t.Fatalf("expected retry to sync, got %s", f.app.Status)
	}
}

func TestPerformSync_RecoversAfterGitRepositoryBecomesReachable(t *testing.T) {
	repo := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	f := newSyncFixture(t, filepath.Join(t.TempDir(), "not-yet"))

	f.sync(context.Background(), "initial")
	if f.app.Status != "Error" || f.app.ConsecutiveFailures != 1 || f.applies.Load() != 0 {
		t.Fatalf("git failure not recorded: %s failures=%d applies=%d", f.app.Status, f.app.ConsecutiveFailures, f.applies.Load())
	}

	f.app.RepoURL = repo
	f.sync(context.Background(), "poll")
	if f.app.Status != "Synced" || f.app.ConsecutiveFailures != 0 || f.applies.Load() != 1 {
		t.Fatalf("did not recover: %s failures=%d applies=%d", f.app.Status, f.app.ConsecutiveFailures, f.applies.Load())
	}
}

func TestHandleAppCommand_SyncRequestsCoalesceWithoutBlocking(t *testing.T) {
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	rt := &appRuntime{cancel: func() {}, syncChan: make(chan struct{}, 1)}
	ctrl.runningApps["a1"] = rt

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			ctrl.handleAppCommand(AppCommand{Type: AppCommandSync, AppName: "a1"}, "unused")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sync requests blocked")
	}
	if len(rt.syncChan) != 1 {
		t.Fatalf("expected requests to coalesce into one pending sync, got %d", len(rt.syncChan))
	}
}
