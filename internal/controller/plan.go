package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"aeswibon.com/github/gitopsctl/internal/core/git"
	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

var (
	ErrAppNotFound     = errors.New("application not found")
	ErrClusterNotFound = errors.New("cluster not found")
)

type PlanResult struct {
	App    string
	Commit string
	Items  []k8s.PlanItem
}

type K8sPlanner interface {
	Plan(ctx context.Context, manifestsDir, appName, clusterName string, previous []k8s.ResourceMetadata) ([]k8s.PlanItem, []error)
}

type PlannerFactory func(logger *zap.Logger, kubeconfigPath string, allowedNamespaces []string) (K8sPlanner, error)

func defaultPlannerFactory(logger *zap.Logger, kubeconfigPath string, allowedNamespaces []string) (K8sPlanner, error) {
	return k8s.NewClientSet(logger, kubeconfigPath, allowedNamespaces)
}

func WithPlannerFactory(f PlannerFactory) Option {
	return func(c *Controller) {
		c.plannerFactory = f
	}
}

// PlanApp clones the application's repository and reports what a sync would change, without applying anything.
func (c *Controller) PlanApp(ctx context.Context, appName string) (*PlanResult, error) {
	c.apps.RLock()
	registered, ok := c.apps.Get(appName)
	if !ok {
		c.apps.RUnlock()
		return nil, ErrAppNotFound
	}
	application := *registered
	c.apps.RUnlock()

	c.clusters.RLock()
	cl, ok := c.clusters.Get(application.ClusterName)
	c.clusters.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrClusterNotFound, application.ClusterName)
	}

	logger := c.logger.With(zap.String("app", appName))
	repoDir, err := git.CreateTempRepoDir()
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = git.CleanUpRepo(logger, repoDir) }()

	commit, err := git.CloneOrPull(ctx, logger, application.RepoURL, application.Branch, repoDir)
	if err != nil {
		return nil, fmt.Errorf("git: %w", err)
	}
	manifestsDir := filepath.Join(repoDir, application.Path)
	if _, err := os.Stat(manifestsDir); err != nil {
		return nil, fmt.Errorf("manifest path: %w", err)
	}

	planner, err := c.plannerFactory(c.logger, cl.KubeconfigPath, cl.AllowedNamespaces)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}

	var previous []k8s.ResourceMetadata
	if application.Prune {
		previous = toK8sResources(application.AppliedResources)
	}
	items, errs := planner.Plan(ctx, manifestsDir, application.Name, application.ClusterName, previous)
	if len(errs) > 0 {
		return nil, fmt.Errorf("plan: %w", errors.Join(errs...))
	}
	return &PlanResult{App: appName, Commit: commit, Items: items}, nil
}
