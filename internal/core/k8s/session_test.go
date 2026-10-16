package k8s

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func sessionClientSet() *ClientSet {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		namespaceGVR:     "NamespaceList",
		resourceQuotaGVR: "ResourceQuotaList",
	})
	return &ClientSet{logger: zap.NewNop(), dynamicClient: client}
}

func rawNamespace(name string, labels map[string]string) *unstructured.Unstructured {
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": name},
	}}
	ns.SetLabels(labels)
	return ns
}

func TestEnsureSessionNamespace_CreatesLabelledNamespaceAndQuota(t *testing.T) {
	cs := sessionClientSet()
	err := cs.EnsureSessionNamespace(context.Background(), SessionNamespaceSpec{Name: "agent-a-1", Session: "s1", Agent: "a", Quota: map[string]string{"pods": "5"}})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := cs.dynamicClient.Resource(namespaceGVR).Get(context.Background(), "agent-a-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	labels := ns.GetLabels()
	if labels[LabelManagedBy] != ManagerName || labels[LabelSession] != "s1" || labels[LabelAgent] != "a" {
		t.Fatalf("unexpected labels %v", labels)
	}
	if _, err := cs.dynamicClient.Resource(resourceQuotaGVR).Namespace("agent-a-1").Get(context.Background(), "gitopsctl-session", metav1.GetOptions{}); err != nil {
		t.Fatalf("quota missing: %v", err)
	}
}

func TestEnsureSessionNamespace_IsIdempotentForOwnSession(t *testing.T) {
	cs := sessionClientSet()
	spec := SessionNamespaceSpec{Name: "agent-a-1", Session: "s1", Quota: map[string]string{"pods": "5"}}
	for i := 0; i < 2; i++ {
		if err := cs.EnsureSessionNamespace(context.Background(), spec); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
}

func TestEnsureSessionNamespace_RefusesForeignNamespace(t *testing.T) {
	cs := sessionClientSet()
	if _, err := cs.dynamicClient.Resource(namespaceGVR).Create(context.Background(), rawNamespace("agent-a-1", map[string]string{"team": "payments"}), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	err := cs.EnsureSessionNamespace(context.Background(), SessionNamespaceSpec{Name: "agent-a-1", Session: "s1"})
	if !errors.Is(err, ErrNamespaceNotOwned) {
		t.Fatalf("expected ErrNamespaceNotOwned, got %v", err)
	}
}

func TestDeleteSessionNamespace(t *testing.T) {
	ctx := context.Background()
	cs := sessionClientSet()
	owned := map[string]string{LabelManagedBy: ManagerName, LabelSession: "s1"}
	for name, labels := range map[string]map[string]string{
		"owned":         owned,
		"other-session": {LabelManagedBy: ManagerName, LabelSession: "s2"},
		"unlabelled":    nil,
	} {
		if _, err := cs.dynamicClient.Resource(namespaceGVR).Create(ctx, rawNamespace(name, labels), metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	if err := cs.DeleteSessionNamespace(ctx, "owned", "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.dynamicClient.Resource(namespaceGVR).Get(ctx, "owned", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned namespace should be gone, got %v", err)
	}
	for _, name := range []string{"other-session", "unlabelled"} {
		if err := cs.DeleteSessionNamespace(ctx, name, "s1"); !errors.Is(err, ErrNamespaceNotOwned) {
			t.Fatalf("%s: expected ErrNamespaceNotOwned, got %v", name, err)
		}
		if _, err := cs.dynamicClient.Resource(namespaceGVR).Get(ctx, name, metav1.GetOptions{}); err != nil {
			t.Fatalf("%s must survive: %v", name, err)
		}
	}
	if err := cs.DeleteSessionNamespace(ctx, "never-existed", "s1"); err != nil {
		t.Fatalf("missing namespace should be success, got %v", err)
	}
}

func TestSessionNamespaces_ListsOnlyManagedSessions(t *testing.T) {
	ctx := context.Background()
	cs := sessionClientSet()
	for name, labels := range map[string]map[string]string{
		"b": {LabelManagedBy: ManagerName, LabelSession: "s2"},
		"a": {LabelManagedBy: ManagerName, LabelSession: "s1"},
		"x": {LabelManagedBy: ManagerName},
		"y": {LabelSession: "s9"},
		"z": nil,
	} {
		if _, err := cs.dynamicClient.Resource(namespaceGVR).Create(ctx, rawNamespace(name, labels), metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := cs.SessionNamespaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (SessionNamespace{"a", "s1"}) || got[1] != (SessionNamespace{"b", "s2"}) {
		t.Fatalf("unexpected namespaces %v", got)
	}
}
