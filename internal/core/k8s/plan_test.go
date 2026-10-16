package k8s

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func writeManifest(t *testing.T, dir, name, value string) {
	t.Helper()
	content := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\ndata:\n  key: " + value + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func actions(items []PlanItem) map[string]PlanAction {
	out := map[string]PlanAction{}
	for _, it := range items {
		out[it.Resource.Name] = it.Action
	}
	return out
}

func TestPlan(t *testing.T) {
	ctx := context.Background()
	cs := fakeClientSet()
	dir := t.TempDir()
	writeManifest(t, dir, "fresh", "v1")

	items, errs := cs.Plan(ctx, dir, "web", "prod", nil)
	if len(errs) != 0 || actions(items)["fresh"] != PlanCreate {
		t.Fatalf("expected create, got %v (errs %v)", items, errs)
	}
	if configMapExists(t, cs, "fresh") {
		t.Fatal("plan must not create objects in the cluster")
	}

	if _, errs := cs.ApplyManifests(ctx, dir, "web", "prod"); len(errs) != 0 {
		t.Fatal(errs)
	}
	items, errs = cs.Plan(ctx, dir, "web", "prod", nil)
	if len(errs) != 0 || actions(items)["fresh"] != PlanUnchanged {
		t.Fatalf("expected unchanged after apply, got %v (errs %v)", items, errs)
	}

	writeManifest(t, dir, "fresh", "v2")
	items, _ = cs.Plan(ctx, dir, "web", "prod", nil)
	if actions(items)["fresh"] != PlanUpdate {
		t.Fatalf("expected update after manifest change, got %v", items)
	}
	live, _ := cs.dynamicClient.Resource(configMapGVR).Namespace("default").Get(ctx, "fresh", metav1.GetOptions{})
	if live.Object["data"].(map[string]any)["key"] != "v1" {
		t.Fatalf("plan must leave live object untouched, got %v", live.Object["data"])
	}

	previous := []ResourceMetadata{cmRef("fresh"), cmRef("removed")}
	items, _ = cs.Plan(ctx, dir, "web", "prod", previous)
	got := actions(items)
	if got["fresh"] != PlanUpdate || got["removed"] != PlanPrune || len(items) != 2 {
		t.Fatalf("unexpected plan with prune candidate: %v", items)
	}
}

func TestPlan_ReportsDisallowedNamespaceAndRenderErrors(t *testing.T) {
	cs := fakeClientSet()
	cs.allowedNamespaces = []string{"other"}
	dir := t.TempDir()
	writeManifest(t, dir, "blocked", "v")

	items, errs := cs.Plan(context.Background(), dir, "web", "prod", nil)
	if len(errs) != 1 || len(items) != 0 {
		t.Fatalf("expected one error and no items, got items=%v errs=%v", items, errs)
	}

	if _, errs := cs.Plan(context.Background(), filepath.Join(dir, "missing"), "web", "prod", nil); len(errs) == 0 {
		t.Fatal("expected render error for missing directory")
	}
}

func TestIsSubset(t *testing.T) {
	tests := []struct {
		name          string
		desired, live any
		want          bool
	}{
		{"server defaults are ignored", map[string]any{"a": "1"}, map[string]any{"a": "1", "b": "2"}, true},
		{"missing key", map[string]any{"a": "1"}, map[string]any{"b": "2"}, false},
		{"different value", map[string]any{"a": "1"}, map[string]any{"a": "2"}, false},
		{"nested", map[string]any{"s": map[string]any{"x": int64(1)}}, map[string]any{"s": map[string]any{"x": float64(1), "y": 2}}, true},
		{"slice equal", []any{"a", "b"}, []any{"a", "b"}, true},
		{"slice length differs", []any{"a"}, []any{"a", "b"}, false},
		{"slice element differs", []any{"a"}, []any{"b"}, false},
		{"type mismatch", map[string]any{"a": "1"}, "1", false},
		{"slice vs map", []any{"a"}, map[string]any{}, false},
		{"null creationTimestamp ignored", map[string]any{"creationTimestamp": nil}, map[string]any{"creationTimestamp": "2024-01-01T00:00:00Z"}, true},
	}
	for _, tc := range tests {
		if got := isSubset(tc.desired, tc.live); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
