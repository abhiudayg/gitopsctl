package sessiontest

import (
	"context"
	"sync"

	"abhiudayg.com/github/gitopsctl/internal/core/k8s"
	"abhiudayg.com/github/gitopsctl/internal/session"
)

type Cluster struct {
	mu         sync.Mutex
	namespaces map[string]string
	ApplyErr   error
	DeleteErr  error
}

func NewCluster() *Cluster { return &Cluster{namespaces: map[string]string{}} }

func (c *Cluster) EnsureNamespace(_ context.Context, spec k8s.SessionNamespaceSpec) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.namespaces[spec.Name] = spec.Session
	return nil
}

func (c *Cluster) DeleteNamespace(_ context.Context, name, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.DeleteErr != nil {
		return c.DeleteErr
	}
	delete(c.namespaces, name)
	return nil
}

func (c *Cluster) Namespaces(context.Context) ([]k8s.SessionNamespace, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []k8s.SessionNamespace
	for name, id := range c.namespaces {
		out = append(out, k8s.SessionNamespace{Name: name, Session: id})
	}
	return out, nil
}

func (c *Cluster) Apply(_ context.Context, req session.ApplyRequest) ([]k8s.ResourceMetadata, error) {
	if c.ApplyErr != nil {
		return nil, c.ApplyErr
	}
	return []k8s.ResourceMetadata{{Version: "v1", Kind: "ConfigMap", Namespace: req.Namespace, Name: "cm"}}, nil
}

func (c *Cluster) Has(namespace string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.namespaces[namespace]
	return ok
}
