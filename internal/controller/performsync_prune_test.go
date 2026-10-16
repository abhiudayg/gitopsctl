package controller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

type pruneCall struct {
	app               string
	previous, current []k8s.ResourceMetadata
	max               int
}

func runPruneSync(t *testing.T, prune bool, previous []app.ResourceMetadata, applied []k8s.ResourceMetadata, pruneFn func(pruneCall) ([]k8s.ResourceMetadata, error)) (*app.Application, []pruneCall) {
	t.Helper()
	logger := zap.NewNop()
	ctrl := NewController(logger, app.NewApplications(), cluster.NewClusters())

	repoPath := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repoPath) })
	workDir := t.TempDir()
	appCfg := filepath.Join(workDir, "apps.json")

	a := &app.Application{
		Name:              "a1",
		RepoURL:           repoPath,
		Branch:            "master",
		Path:              ".",
		ClusterName:       "c1",
		Status:            "Pending",
		Prune:             prune,
		AppliedResources:  previous,
		LastSyncedGitHash: "old-hash",
	}
	ctrl.apps.Add(a)
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})

	var calls []pruneCall
	mock := &mockApplier{
		applyFn: func(context.Context, string, string, string) ([]k8s.ResourceMetadata, []error) {
			return applied, nil
		},
		pruneFn: func(_ context.Context, appName string, prev, cur []k8s.ResourceMetadata, max int) ([]k8s.ResourceMetadata, error) {
			call := pruneCall{app: appName, previous: prev, current: cur, max: max}
			calls = append(calls, call)
			return pruneFn(call)
		},
	}

	ctrl.performSync(context.Background(), logger, a, workDir, mock, appCfg, "manual")
	return a, calls
}

func TestPerformSync_PruneDisabledNeverPrunesAndTracksApplied(t *testing.T) {
	previous := []app.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "old"}}
	applied := []k8s.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "new"}}

	a, calls := runPruneSync(t, false, previous, applied, nil)

	if len(calls) != 0 {
		t.Fatalf("prune must not run when disabled, got %v", calls)
	}
	if a.Status != "Synced" || len(a.AppliedResources) != 1 || a.AppliedResources[0].Name != "new" {
		t.Fatalf("unexpected state: %s %+v", a.Status, a.AppliedResources)
	}
}

func TestPerformSync_PruneEnabledPassesPreviousAndCurrent(t *testing.T) {
	previous := []app.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "old"}}
	applied := []k8s.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "new"}}

	a, calls := runPruneSync(t, true, previous, applied, func(pruneCall) ([]k8s.ResourceMetadata, error) { return nil, nil })

	if len(calls) != 1 || calls[0].app != "a1" || calls[0].max != k8s.DefaultMaxPrune ||
		len(calls[0].previous) != 1 || calls[0].previous[0].Name != "old" ||
		len(calls[0].current) != 1 || calls[0].current[0].Name != "new" {
		t.Fatalf("unexpected prune call: %+v", calls)
	}
	if a.Status != "Synced" || len(a.AppliedResources) != 1 || a.AppliedResources[0].Name != "new" {
		t.Fatalf("unexpected state: %s %+v", a.Status, a.AppliedResources)
	}
}

func TestPerformSync_PruneFailureRetainsStaleAndRetriesOnNextPoll(t *testing.T) {
	previous := []app.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "old"}}
	applied := []k8s.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "new"}}
	retained := []k8s.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "old"}}

	a, _ := runPruneSync(t, true, previous, applied, func(pruneCall) ([]k8s.ResourceMetadata, error) {
		return retained, errors.New("boom")
	})

	if a.Status != "Error" || !strings.Contains(a.Message, "Prune error") || a.ConsecutiveFailures != 1 {
		t.Fatalf("expected prune error state, got %s %q failures=%d", a.Status, a.Message, a.ConsecutiveFailures)
	}
	if a.LastSyncedGitHash != "old-hash" {
		t.Fatalf("LastSyncedGitHash must be rolled back so the next poll retries, got %q", a.LastSyncedGitHash)
	}
	if len(a.AppliedResources) != 2 {
		t.Fatalf("stale resource must stay tracked alongside the new one, got %+v", a.AppliedResources)
	}
}
