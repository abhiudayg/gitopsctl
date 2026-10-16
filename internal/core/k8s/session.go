package k8s

import (
	"context"
	"errors"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	LabelSession = "gitopsctl.io/session"
	LabelAgent   = "gitopsctl.io/agent"
)

var (
	namespaceGVR     = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	resourceQuotaGVR = schema.GroupVersionResource{Version: "v1", Resource: "resourcequotas"}
)

var ErrNamespaceNotOwned = errors.New("namespace is not owned by this session")

type SessionNamespace struct {
	Name    string
	Session string
}

type SessionNamespaceSpec struct {
	Name    string
	Session string
	Agent   string
	Quota   map[string]string
}

func sessionLabels(session, agent string) map[string]any {
	labels := map[string]any{
		LabelManagedBy: ManagerName,
		LabelSession:   session,
	}
	if agent != "" {
		labels[LabelAgent] = agent
	}
	return labels
}

// EnsureSessionNamespace creates the labelled namespace and its quota. An existing namespace is accepted only if it already belongs to the session.
func (cs *ClientSet) EnsureSessionNamespace(ctx context.Context, spec SessionNamespaceSpec) error {
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": spec.Name, "labels": sessionLabels(spec.Session, spec.Agent)},
	}}
	_, err := cs.dynamicClient.Resource(namespaceGVR).Create(ctx, ns, metav1.CreateOptions{})
	switch {
	case apierrors.IsAlreadyExists(err):
		if _, ownErr := cs.ownedNamespace(ctx, spec.Name, spec.Session); ownErr != nil {
			return ownErr
		}
	case err != nil:
		return fmt.Errorf("create namespace %s: %w", spec.Name, err)
	}

	if len(spec.Quota) == 0 {
		return nil
	}
	hard := make(map[string]any, len(spec.Quota))
	for resource, quantity := range spec.Quota {
		hard[resource] = quantity
	}
	quota := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ResourceQuota",
		"metadata":   map[string]any{"name": "gitopsctl-session", "namespace": spec.Name, "labels": sessionLabels(spec.Session, spec.Agent)},
		"spec":       map[string]any{"hard": hard},
	}}
	if _, err := cs.dynamicClient.Resource(resourceQuotaGVR).Namespace(spec.Name).Create(ctx, quota, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create resource quota in %s: %w", spec.Name, err)
	}
	return nil
}

// DeleteSessionNamespace deletes the namespace only when it carries this session's labels; a missing namespace is success.
func (cs *ClientSet) DeleteSessionNamespace(ctx context.Context, name, session string) error {
	ns, err := cs.ownedNamespace(ctx, name, session)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	uid := ns.GetUID()
	err = cs.dynamicClient.Resource(namespaceGVR).Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %s: %w", name, err)
	}
	return nil
}

func (cs *ClientSet) ownedNamespace(ctx context.Context, name, session string) (*unstructured.Unstructured, error) {
	ns, err := cs.dynamicClient.Resource(namespaceGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("get namespace %s: %w", name, err)
	}
	labels := ns.GetLabels()
	if labels[LabelManagedBy] != ManagerName || labels[LabelSession] != session {
		return nil, fmt.Errorf("%w: %s", ErrNamespaceNotOwned, name)
	}
	return ns, nil
}

// SessionNamespaces lists every namespace this controller created for a session.
func (cs *ClientSet) SessionNamespaces(ctx context.Context) ([]SessionNamespace, error) {
	list, err := cs.dynamicClient.Resource(namespaceGVR).List(ctx, metav1.ListOptions{LabelSelector: LabelManagedBy + "=" + ManagerName + "," + LabelSession})
	if err != nil {
		return nil, fmt.Errorf("list session namespaces: %w", err)
	}
	namespaces := make([]SessionNamespace, 0, len(list.Items))
	for _, item := range list.Items {
		namespaces = append(namespaces, SessionNamespace{Name: item.GetName(), Session: item.GetLabels()[LabelSession]})
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })
	return namespaces, nil
}
