package k8s

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/policy"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var configMaps = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

func sessionPolicy(t *testing.T) *policy.Policy {
	t.Helper()
	p, err := policy.Parse([]byte("allowed_kinds: [ConfigMap]\nallowed_namespaces: ['agent-*']\n"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeManifests(t *testing.T, manifests string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.yaml"), []byte(manifests), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func configMapIn(t *testing.T, cs *ClientSet, namespace, name string) bool {
	t.Helper()
	_, err := cs.dynamicClient.Resource(configMaps).Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	return err == nil
}

func cm(name, namespace string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\n  namespace: " + namespace + "\n"
}

func TestApplyManifests_PolicyBlocksWholeSetOnViolation(t *testing.T) {
	cs := fakeClientSet().WithPolicy(sessionPolicy(t), policy.Scope{Namespaces: []string{"agent-1"}})
	dir := writeManifests(t, cm("ok", "agent-1")+"---\n"+cm("escape", "kube-system"))

	applied, errs := cs.ApplyManifests(context.Background(), dir, "app", "cluster")

	if len(applied) != 0 {
		t.Fatalf("expected nothing applied, got %v", applied)
	}
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "policy namespace") {
		t.Fatalf("expected a namespace violation, got %v", errs)
	}
	if configMapIn(t, cs, "agent-1", "ok") {
		t.Fatal("the compliant object must not be applied when another is denied")
	}
}

func TestApplyManifests_PolicyAllowsCompliantAndDefaultsToSessionNamespace(t *testing.T) {
	cs := fakeClientSet().WithPolicy(sessionPolicy(t), policy.Scope{Namespaces: []string{"agent-1"}})
	dir := writeManifests(t, "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unscoped\n")

	applied, errs := cs.ApplyManifests(context.Background(), dir, "app", "cluster")

	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(applied) != 1 || applied[0].Namespace != "agent-1" {
		t.Fatalf("expected the object in the session namespace, got %v", applied)
	}
	if !configMapIn(t, cs, "agent-1", "unscoped") {
		t.Fatal("expected the configmap to exist")
	}
}

func TestApplyManifests_PolicyBlocksClusterScopedAndUnknownKinds(t *testing.T) {
	cs := fakeClientSet().WithPolicy(sessionPolicy(t), policy.Scope{})
	cases := map[string]string{
		"namespace kind": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: agent-evil\n",
		"unknown kind":   "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w\n  namespace: agent-1\n",
		"malformed yaml": "kind: [",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			applied, errs := cs.ApplyManifests(context.Background(), writeManifests(t, doc), "app", "cluster")
			if len(applied) != 0 || len(errs) == 0 {
				t.Fatalf("expected a blocked apply, got applied=%v errs=%v", applied, errs)
			}
		})
	}
}

func TestApplyManifests_WithoutPolicyIsUnchanged(t *testing.T) {
	cs := fakeClientSet()
	applied, errs := cs.ApplyManifests(context.Background(), writeManifests(t, cm("a", "kube-system")), "app", "cluster")
	if len(errs) != 0 || len(applied) != 1 {
		t.Fatalf("unrestricted client must still apply: applied=%v errs=%v", applied, errs)
	}
}

func TestPlan_PolicyViolationReturnsNoItems(t *testing.T) {
	cs := fakeClientSet().WithPolicy(sessionPolicy(t), policy.Scope{})
	items, errs := cs.Plan(context.Background(), writeManifests(t, cm("a", "kube-system")), "app", "cluster", nil)
	if len(items) != 0 || len(errs) == 0 {
		t.Fatalf("expected violations and no plan items, got items=%v errs=%v", items, errs)
	}
}

func TestWithPolicy_DoesNotMutateOriginal(t *testing.T) {
	base := fakeClientSet()
	_ = base.WithPolicy(sessionPolicy(t), policy.Scope{Namespaces: []string{"agent-1"}})
	if base.policy != nil || base.defaultNamespace() != "default" {
		t.Fatal("WithPolicy must return a copy")
	}
}
