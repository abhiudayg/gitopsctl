package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestRunRegisterCommand_DryRunWithClusterOnDisk(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "register-run-dry")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	logger = zap.NewNop()

	cs := cluster.NewClusters()
	cs.Add(&cluster.Cluster{Name: "c1", KubeconfigPath: "/tmp/k"})
	if err := cluster.SaveClusters(cs, cluster.DefaultClusterConfigFile); err != nil {
		t.Fatal(err)
	}

	appName = "myapp"
	repoURL = "https://github.com/example/repo.git"
	pathInRepo = "deploy"
	clusterName = "c1"
	branch = ""
	interval = ""
	dryRunApp = true
	forceApp = false
	syncPolicy = ""

	defer func() {
		dryRunApp = false
		appName, repoURL, pathInRepo, clusterName = "", "", "", ""
	}()

	if err := runRegisterCommand(nil, nil); err != nil {
		t.Fatalf("runRegisterCommand dry-run: %v", err)
	}
}

func TestRunRegisterCommand_ForceOverwritePath(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "register-run-force")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	logger = zap.NewNop()

	cs := cluster.NewClusters()
	cs.Add(&cluster.Cluster{Name: "c1", KubeconfigPath: "/tmp/k"})
	if err := cluster.SaveClusters(cs, cluster.DefaultClusterConfigFile); err != nil {
		t.Fatal(err)
	}

	// existing app on disk
	if err := os.MkdirAll(filepath.Dir(cluster.DefaultClusterConfigFile), 0755); err != nil {
		t.Fatal(err)
	}
	appsJSON := `[{"name":"myapp","repoURL":"https://github.com/old/repo.git","branch":"main","path":"x","clusterName":"c1","interval":"1m"}]`
	if err := os.WriteFile(filepath.Join("configs", "applications.json"), []byte(appsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	appName = "myapp"
	repoURL = "https://github.com/example/repo.git"
	pathInRepo = "deploy"
	clusterName = "c1"
	dryRunApp = true
	forceApp = true

	defer func() {
		dryRunApp = false
		forceApp = false
		appName, repoURL, pathInRepo, clusterName = "", "", "", ""
	}()

	if err := runRegisterCommand(nil, nil); err != nil {
		t.Fatalf("runRegisterCommand force dry-run: %v", err)
	}
}

func TestLoadAppsForList_ReturnsRenderable(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "list-apps-load")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	logger = zap.NewNop()

	if err := os.MkdirAll("configs", 0755); err != nil {
		t.Fatal(err)
	}
	appsJSON := `[{"name":"a1","repoURL":"https://github.com/x/y.git","branch":"main","path":"p","clusterName":"c1","interval":"1m"}]`
	if err := os.WriteFile("configs/applications.json", []byte(appsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	items, err := loadAppsForList()
	if err != nil {
		t.Fatalf("loadAppsForList: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 app, got %d", len(items))
	}
}

func TestLoadClustersForList_ReturnsRenderable(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "list-clusters-load")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	logger = zap.NewNop()

	if err := os.MkdirAll("configs", 0755); err != nil {
		t.Fatal(err)
	}
	clJSON := `[{"name":"c1","kubeconfigPath":"/tmp/k","registeredAt":"2020-01-01T00:00:00Z"}]`
	if err := os.WriteFile("configs/clusters.json", []byte(clJSON), 0644); err != nil {
		t.Fatal(err)
	}

	items, err := loadClustersForList()
	if err != nil {
		t.Fatalf("loadClustersForList: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(items))
	}
}

func TestUnregisterCluster_WithConfirmationYes(t *testing.T) {
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "unreg-cluster-confirm")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	logger = zap.NewNop()

	cs := cluster.NewClusters()
	cs.Add(&cluster.Cluster{Name: "c1", KubeconfigPath: "/tmp/k"})
	if err := cluster.SaveClusters(cs, cluster.DefaultClusterConfigFile); err != nil {
		t.Fatal(err)
	}

	f, err := os.CreateTemp("", "stdin-unreg-cluster")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = f

	clusterUnregName = "c1"
	forceUnregisterCluster = false
	defer func() {
		clusterUnregName = ""
		forceUnregisterCluster = false
	}()

	if err := unregisterCluster(nil, nil); err != nil {
		t.Fatalf("unregisterCluster: %v", err)
	}
}
