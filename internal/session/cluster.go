package session

import (
	"context"
	"errors"
	"fmt"

	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"aeswibon.com/github/gitopsctl/internal/policy"
	"go.uber.org/zap"
)

type K8sCluster struct {
	logger         *zap.Logger
	kubeconfigPath string
	policy         *policy.Policy
}

func NewK8sCluster(logger *zap.Logger, kubeconfigPath string, p *policy.Policy) *K8sCluster {
	return &K8sCluster{logger: logger, kubeconfigPath: kubeconfigPath, policy: p}
}

func (c *K8sCluster) EnsureNamespace(ctx context.Context, spec k8s.SessionNamespaceSpec) error {
	cs, err := k8s.NewClientSet(c.logger, c.kubeconfigPath, nil)
	if err != nil {
		return err
	}
	return cs.EnsureSessionNamespace(ctx, spec)
}

func (c *K8sCluster) DeleteNamespace(ctx context.Context, name, session string) error {
	cs, err := k8s.NewClientSet(c.logger, c.kubeconfigPath, nil)
	if err != nil {
		return err
	}
	return cs.DeleteSessionNamespace(ctx, name, session)
}

func (c *K8sCluster) Namespaces(ctx context.Context) ([]k8s.SessionNamespace, error) {
	cs, err := k8s.NewClientSet(c.logger, c.kubeconfigPath, nil)
	if err != nil {
		return nil, err
	}
	return cs.SessionNamespaces(ctx)
}

// Apply deploys the manifests under the policy, confined to the session namespace, then prunes objects the previous deploy created that this one dropped.
func (c *K8sCluster) Apply(ctx context.Context, req ApplyRequest) ([]k8s.ResourceMetadata, error) {
	cs, err := k8s.NewClientSet(c.logger, c.kubeconfigPath, []string{req.Namespace})
	if err != nil {
		return nil, err
	}
	enforced := cs.WithPolicy(c.policy, policy.Scope{Namespaces: []string{req.Namespace}})
	appName := "session-" + req.Session
	applied, errs := enforced.ApplyManifests(ctx, req.ManifestsDir, appName, req.Session)
	if len(errs) > 0 {
		return nil, fmt.Errorf("apply: %w", errors.Join(errs...))
	}
	if _, err := enforced.Prune(ctx, appName, req.Previous, applied, k8s.DefaultMaxPrune); err != nil {
		return nil, fmt.Errorf("prune: %w", err)
	}
	return applied, nil
}
