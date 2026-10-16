package k8s

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var configMapGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

func cmRef(name string) ResourceMetadata {
	return ResourceMetadata{Version: "v1", Kind: "ConfigMap", Namespace: "default", Name: name}
}

func seedConfigMap(t *testing.T, cs *ClientSet, name string, labels map[string]string) {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": name, "namespace": "default"},
	}}
	obj.SetLabels(labels)
	if _, err := cs.dynamicClient.Resource(configMapGVR).Namespace("default").Create(context.Background(), obj, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func configMapExists(t *testing.T, cs *ClientSet, name string) bool {
	t.Helper()
	_, err := cs.dynamicClient.Resource(configMapGVR).Namespace("default").Get(context.Background(), name, metav1.GetOptions{})
	return err == nil
}

func ownedBy(app string) map[string]string {
	return map[string]string{LabelManagedBy: ManagerName, LabelApp: app}
}

func TestApplyYAMLData_AddsOwnershipLabelsAndKeepsExisting(t *testing.T) {
	cs := fakeClientSet()
	data := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: labelled
  labels:
    team: platform
data:
  k: v
`)
	if _, errs := cs.applyYAMLData(context.Background(), data, "inline", "web", "prod"); len(errs) != 0 {
		t.Fatalf("apply failed: %v", errs)
	}
	got, err := cs.dynamicClient.Resource(configMapGVR).Namespace("default").Get(context.Background(), "labelled", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"team": "platform", LabelManagedBy: ManagerName, LabelApp: "web", LabelCluster: "prod"}
	for k, v := range want {
		if got.GetLabels()[k] != v {
			t.Errorf("label %s = %q, want %q (labels: %v)", k, got.GetLabels()[k], v, got.GetLabels())
		}
	}
}

func TestApplyYAMLData_GetErrorOtherThanNotFoundIsReportedNotCreated(t *testing.T) {
	cs := fakeClientSet()
	fakeDyn := cs.dynamicClient.(*dynamicfake.FakeDynamicClient)
	fakeDyn.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	created := false
	fakeDyn.PrependReactor("create", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		created = true
		return false, nil, nil
	})

	data := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")
	applied, errs := cs.applyYAMLData(context.Background(), data, "inline", "web", "prod")
	if len(errs) != 1 || len(applied) != 0 || created {
		t.Fatalf("expected one error and no create, got applied=%v errs=%v created=%v", applied, errs, created)
	}
}

func TestPrune(t *testing.T) {
	t.Run("deletes only stale owned resources", func(t *testing.T) {
		cs := fakeClientSet()
		seedConfigMap(t, cs, "kept", ownedBy("web"))
		seedConfigMap(t, cs, "stale", ownedBy("web"))
		seedConfigMap(t, cs, "foreign", ownedBy("other"))
		seedConfigMap(t, cs, "unlabelled", nil)

		previous := []ResourceMetadata{cmRef("kept"), cmRef("stale"), cmRef("foreign"), cmRef("unlabelled")}
		retained, err := cs.Prune(context.Background(), "web", previous, []ResourceMetadata{cmRef("kept")}, DefaultMaxPrune)
		if err != nil || len(retained) != 0 {
			t.Fatalf("unexpected result: retained=%v err=%v", retained, err)
		}
		for name, wantExists := range map[string]bool{"kept": true, "stale": false, "foreign": true, "unlabelled": true} {
			if got := configMapExists(t, cs, name); got != wantExists {
				t.Errorf("configmap %s exists=%v, want %v", name, got, wantExists)
			}
		}
	})

	t.Run("already deleted resources are settled", func(t *testing.T) {
		cs := fakeClientSet()
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{cmRef("gone")}, nil, DefaultMaxPrune)
		if err != nil || len(retained) != 0 {
			t.Fatalf("retained=%v err=%v", retained, err)
		}
	})

	t.Run("nothing stale is a no-op", func(t *testing.T) {
		cs := fakeClientSet()
		seedConfigMap(t, cs, "a", ownedBy("web"))
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{cmRef("a")}, []ResourceMetadata{cmRef("a")}, 0)
		if err != nil || retained != nil || !configMapExists(t, cs, "a") {
			t.Fatalf("retained=%v err=%v", retained, err)
		}
	})

	t.Run("max deletions guard deletes nothing", func(t *testing.T) {
		cs := fakeClientSet()
		seedConfigMap(t, cs, "a", ownedBy("web"))
		seedConfigMap(t, cs, "b", ownedBy("web"))
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{cmRef("a"), cmRef("b")}, nil, 1)
		if err == nil || len(retained) != 2 || !configMapExists(t, cs, "a") || !configMapExists(t, cs, "b") {
			t.Fatalf("expected refusal, retained=%v err=%v", retained, err)
		}
	})

	t.Run("protected kinds are never deleted", func(t *testing.T) {
		cs := fakeClientSet()
		ns := ResourceMetadata{Version: "v1", Kind: "Namespace", Name: "team-a"}
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{ns}, nil, DefaultMaxPrune)
		if err != nil || len(retained) != 0 {
			t.Fatalf("retained=%v err=%v", retained, err)
		}
	})

	t.Run("disallowed namespace is retained with an error", func(t *testing.T) {
		cs := fakeClientSet()
		cs.allowedNamespaces = []string{"other"}
		seedConfigMap(t, cs, "a", ownedBy("web"))
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{cmRef("a")}, nil, DefaultMaxPrune)
		if err == nil || len(retained) != 1 || !configMapExists(t, cs, "a") {
			t.Fatalf("retained=%v err=%v", retained, err)
		}
	})

	t.Run("unmappable kind is retained with an error", func(t *testing.T) {
		cs := fakeClientSet()
		unknown := ResourceMetadata{Group: "example.com", Version: "v1", Kind: "Widget", Namespace: "default", Name: "w"}
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{unknown}, nil, DefaultMaxPrune)
		if err == nil || len(retained) != 1 {
			t.Fatalf("retained=%v err=%v", retained, err)
		}
	})

	t.Run("delete failure is retained with an error", func(t *testing.T) {
		cs := fakeClientSet()
		seedConfigMap(t, cs, "a", ownedBy("web"))
		cs.dynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("delete", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("boom")
		})
		retained, err := cs.Prune(context.Background(), "web", []ResourceMetadata{cmRef("a")}, nil, DefaultMaxPrune)
		if err == nil || len(retained) != 1 {
			t.Fatalf("retained=%v err=%v", retained, err)
		}
	})
}
