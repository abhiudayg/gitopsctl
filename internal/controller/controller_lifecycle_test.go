package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestController_StopIsIdempotent(t *testing.T) {
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	ctrl.wg.Add(1)
	go ctrl.commandDispatcher(filepath.Join(t.TempDir(), "apps.json"))

	ctrl.Stop()
	ctrl.Stop()
}

func TestController_SendersDoNotBlockOrPanicAfterStop(t *testing.T) {
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	ctrl.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2*cap(ctrl.appCommandChan); i++ {
			ctrl.StartApp("a")
			ctrl.StopApp("a")
			ctrl.TriggerSync("a")
			ctrl.ApproveSync("a", "hash")
			ctrl.TriggerClusterHealthCheck("c")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("senders blocked after Stop")
	}
}

func TestController_ConcurrentStopAndSenders(t *testing.T) {
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	ctrl.wg.Add(1)
	go ctrl.commandDispatcher(filepath.Join(t.TempDir(), "apps.json"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			ctrl.TriggerSync("missing")
		}
	}()
	ctrl.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sender goroutine did not finish")
	}
}

func TestSaveAppStatus_PropagatesSyncResultsWithoutClobberingApproval(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "apps.json")
	if err := os.WriteFile(cfg, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters())
	shared := &app.Application{Name: "web", ApprovedGitHash: "approved"}
	ctrl.apps.Add(shared)

	local := &app.Application{
		Name:             "web",
		Status:           "Synced",
		LatestGitHash:    "abc",
		AppliedResources: []app.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "cm"}},
	}
	ctrl.saveAppStatus(local, cfg, true)

	ctrl.apps.RLock()
	defer ctrl.apps.RUnlock()
	if shared.Status != "Synced" || shared.LatestGitHash != "abc" || len(shared.AppliedResources) != 1 {
		t.Fatalf("sync results not propagated: %+v", shared)
	}
	if shared.ApprovedGitHash != "approved" {
		t.Fatalf("ApprovedGitHash clobbered: %q", shared.ApprovedGitHash)
	}
}
