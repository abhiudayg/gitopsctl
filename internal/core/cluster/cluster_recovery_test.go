package cluster

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClusters_RejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clusters.json")
	if err := os.WriteFile(path, []byte(`[{"name":"prod"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if clusters, err := LoadClusters(path); err == nil {
		t.Fatalf("expected error, got %v", clusters)
	}
}

func TestLoadClusters_MissingFileIsEmpty(t *testing.T) {
	clusters, err := LoadClusters(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(clusters.List()) != 0 {
		t.Fatalf("expected empty clusters, got %v (%v)", clusters, err)
	}
}

func TestSaveClusters_LeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")
	clusters := NewClusters()
	clusters.Add(&Cluster{Name: "prod", KubeconfigPath: "/kube"})
	if err := SaveClusters(clusters, path); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
