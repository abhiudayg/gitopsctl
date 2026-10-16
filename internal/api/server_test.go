package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestServer_HealthCheck(t *testing.T) {
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := &controller.Controller{} // Mock/minimal controller
	server := NewServer(logger, apps, clusters, ctrl, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := server.Echo().NewContext(req, rec)

	if err := server.HealthCheck(c); err != nil {
		t.Errorf("HealthCheck() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got %q", rec.Body.String())
	}
}

func TestServer_Metrics(t *testing.T) {
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := &controller.Controller{}
	server := NewServer(logger, apps, clusters, ctrl, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	server.Echo().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
