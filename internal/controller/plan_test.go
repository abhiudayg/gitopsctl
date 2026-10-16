package controller

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

type fakePlanner struct {
	items    []k8s.PlanItem
	errs     []error
	previous []k8s.ResourceMetadata
	dir      string
}

func (f *fakePlanner) Plan(_ context.Context, dir, _, _ string, previous []k8s.ResourceMetadata) ([]k8s.PlanItem, []error) {
	f.dir, f.previous = dir, previous
	return f.items, f.errs
}

func newPlanController(t *testing.T, planner *fakePlanner, factoryErr error, application *app.Application) *Controller {
	t.Helper()
	ctrl := NewController(zap.NewNop(), app.NewApplications(), cluster.NewClusters(),
		WithPlannerFactory(func(*zap.Logger, string, []string) (K8sPlanner, error) {
			if factoryErr != nil {
				return nil, factoryErr
			}
			return planner, nil
		}))
	if application != nil {
		ctrl.apps.Add(application)
	}
	ctrl.clusters.Add(&cluster.Cluster{Name: "c1"})
	return ctrl
}

func planApp(t *testing.T, mutate func(*app.Application)) *app.Application {
	t.Helper()
	repo := createLocalRepo(t)
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	a := &app.Application{Name: "a1", RepoURL: repo, Branch: "master", Path: ".", ClusterName: "c1"}
	if mutate != nil {
		mutate(a)
	}
	return a
}

func TestPlanApp_ReturnsPlannerItemsAndCommit(t *testing.T) {
	want := []k8s.PlanItem{{Resource: k8s.ResourceMetadata{Kind: "ConfigMap", Name: "cm"}, Action: k8s.PlanCreate}}
	planner := &fakePlanner{items: want}
	ctrl := newPlanController(t, planner, nil, planApp(t, nil))

	res, err := ctrl.PlanApp(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if res.App != "a1" || res.Commit == "" || len(res.Items) != 1 || res.Items[0] != want[0] {
		t.Fatalf("unexpected result: %+v", res)
	}
	if planner.previous != nil {
		t.Fatalf("previous resources must only be passed when prune is enabled, got %v", planner.previous)
	}
}

func TestPlanApp_PassesTrackedResourcesWhenPruneEnabled(t *testing.T) {
	planner := &fakePlanner{}
	a := planApp(t, func(a *app.Application) {
		a.Prune = true
		a.AppliedResources = []app.ResourceMetadata{{Kind: "ConfigMap", Namespace: "default", Name: "old"}}
	})
	ctrl := newPlanController(t, planner, nil, a)

	if _, err := ctrl.PlanApp(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	if len(planner.previous) != 1 || planner.previous[0].Name != "old" {
		t.Fatalf("expected tracked resources, got %v", planner.previous)
	}
}

func TestPlanApp_Errors(t *testing.T) {
	t.Run("unknown app", func(t *testing.T) {
		ctrl := newPlanController(t, &fakePlanner{}, nil, nil)
		if _, err := ctrl.PlanApp(context.Background(), "nope"); !errors.Is(err, ErrAppNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("unknown cluster", func(t *testing.T) {
		ctrl := newPlanController(t, &fakePlanner{}, nil, planApp(t, func(a *app.Application) { a.ClusterName = "missing" }))
		if _, err := ctrl.PlanApp(context.Background(), "a1"); !errors.Is(err, ErrClusterNotFound) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("git failure", func(t *testing.T) {
		ctrl := newPlanController(t, &fakePlanner{}, nil, planApp(t, func(a *app.Application) { a.RepoURL = "/definitely/not/a/repo" }))
		if _, err := ctrl.PlanApp(context.Background(), "a1"); err == nil || !strings.Contains(err.Error(), "git") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("missing manifest path", func(t *testing.T) {
		ctrl := newPlanController(t, &fakePlanner{}, nil, planApp(t, func(a *app.Application) { a.Path = "nope" }))
		if _, err := ctrl.PlanApp(context.Background(), "a1"); err == nil || !strings.Contains(err.Error(), "manifest path") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("client factory failure", func(t *testing.T) {
		ctrl := newPlanController(t, nil, errors.New("no kubeconfig"), planApp(t, nil))
		if _, err := ctrl.PlanApp(context.Background(), "a1"); err == nil || !strings.Contains(err.Error(), "kubernetes client") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("planner errors are joined", func(t *testing.T) {
		ctrl := newPlanController(t, &fakePlanner{errs: []error{errors.New("bad yaml")}}, nil, planApp(t, nil))
		if _, err := ctrl.PlanApp(context.Background(), "a1"); err == nil || !strings.Contains(err.Error(), "bad yaml") {
			t.Fatalf("got %v", err)
		}
	})
}
