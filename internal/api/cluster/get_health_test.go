package cluster

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func TestHandler_GetFoundAndNotFound(t *testing.T) {
	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	h := NewHandler(logger, clusters, apps, ctrl)

	req := httptest.NewRequest(http.MethodGet, "/clusters/missing", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("missing")
	if err := h.Get(c); err == nil {
		t.Fatal("expected not found error")
	}

	clusters.Add(&clustercore.Cluster{Name: "c1"})
	req = httptest.NewRequest(http.MethodGet, "/clusters/c1", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("c1")
	if err := h.Get(c); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_HealthCheck_NotFound(t *testing.T) {
	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	h := NewHandler(logger, clusters, apps, ctrl)

	req := httptest.NewRequest(http.MethodPost, "/clusters/missing/check", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("missing")
	if err := h.HealthCheck(c); err == nil {
		t.Fatal("expected not found error")
	}
}
