package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadApplications_RejectsCorruptState(t *testing.T) {
	tests := map[string]string{
		"truncated json":   `[{"name":"web","interval":"5m"`,
		"wrong top level":  `{"name":"web"}`,
		"invalid interval": `[{"name":"web","interval":"soon"}]`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "apps.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if apps, err := LoadApplications(path); err == nil {
				t.Fatalf("expected error, got %v", apps)
			}
		})
	}
}

func TestSaveApplications_IsAtomicAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.json")

	apps := NewApplications()
	apps.Add(&Application{Name: "web", Interval: "30s", Prune: true,
		AppliedResources: []ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "cm"}}})
	if err := SaveApplications(apps, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadApplications(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.Get("web")
	if !ok || !got.Prune || len(got.AppliedResources) != 1 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
