package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/core/k8s"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type stubPlanner struct {
	items []k8s.PlanItem
	errs  []error
}

func (s stubPlanner) Plan(context.Context, string, string, string, []k8s.ResourceMetadata) ([]k8s.PlanItem, []error) {
	return s.items, s.errs
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func planHandler(t *testing.T, planner stubPlanner, application *appcore.Application, withCluster bool) (*Handler, *echo.Echo) {
	t.Helper()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	if withCluster {
		clusters.Add(&clustercore.Cluster{Name: "c1"})
	}
	if application != nil {
		apps.Add(application)
	}
	ctrl := controller.NewController(zap.NewNop(), apps, clusters,
		controller.WithPlannerFactory(func(*zap.Logger, string, []string) (controller.K8sPlanner, error) { return planner, nil }))
	return NewHandler(zap.NewNop(), apps, clusters, ctrl), echo.New()
}

func TestPlan_Success(t *testing.T) {
	application := &appcore.Application{Name: "web", RepoURL: initRepo(t), Branch: "main", Path: ".", ClusterName: "c1"}
	planner := stubPlanner{items: []k8s.PlanItem{
		{Resource: k8s.ResourceMetadata{Version: "v1", Kind: "ConfigMap", Namespace: "default", Name: "cm"}, Action: k8s.PlanCreate},
	}}
	h, e := planHandler(t, planner, application, true)

	c, rec := newJSONContext(e, http.MethodPost, "/applications/web/plan", "")
	c.SetParamNames("name")
	c.SetParamValues("web")
	if err := h.Plan(c); err != nil {
		t.Fatal(err)
	}

	var got PlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || got.App != "web" || got.Commit == "" || len(got.Items) != 1 ||
		got.Items[0].Action != "create" || got.Items[0].Kind != "ConfigMap" || got.Items[0].Name != "cm" {
		t.Fatalf("unexpected response %d: %+v", rec.Code, got)
	}
}

func TestPlan_ErrorMapping(t *testing.T) {
	good := func() *appcore.Application {
		return &appcore.Application{Name: "web", RepoURL: initRepo(t), Branch: "main", Path: ".", ClusterName: "c1"}
	}
	tests := []struct {
		name        string
		application *appcore.Application
		withCluster bool
		planner     stubPlanner
		want        int
	}{
		{"unknown app", nil, true, stubPlanner{}, http.StatusNotFound},
		{"unknown cluster", good(), false, stubPlanner{}, http.StatusUnprocessableEntity},
		{"planner failure", good(), true, stubPlanner{errs: []error{errors.New("secret-detail")}}, http.StatusBadGateway},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, e := planHandler(t, tc.planner, tc.application, tc.withCluster)
			c, _ := newJSONContext(e, http.MethodPost, "/applications/web/plan", "")
			c.SetParamNames("name")
			c.SetParamValues("web")

			err := h.Plan(c)
			var httpErr *echo.HTTPError
			if !errors.As(err, &httpErr) || httpErr.Code != tc.want {
				t.Fatalf("got %v, want status %d", err, tc.want)
			}
			if msg, _ := httpErr.Message.(string); msg == "" || msg == "secret-detail" {
				t.Fatalf("unexpected message %q", msg)
			}
		})
	}
}
