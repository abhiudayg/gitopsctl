package cmd

import (
	"os"
	"testing"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestUnregisterCluster_ForcePath(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "unregister-cluster")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	cs := cluster.NewClusters()
	cs.Add(&cluster.Cluster{Name: "c1", KubeconfigPath: "/tmp/kube", RegisteredAt: time.Now()})
	if err := cluster.SaveClusters(cs, cluster.DefaultClusterConfigFile); err != nil {
		t.Fatalf("save clusters failed: %v", err)
	}

	clusterUnregName = "c1"
	forceUnregisterCluster = true
	if err := unregisterCluster(nil, nil); err != nil {
		t.Fatalf("unregisterCluster() error = %v", err)
	}
}
