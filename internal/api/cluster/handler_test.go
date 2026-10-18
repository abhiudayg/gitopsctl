package cluster

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func TestHandler_Register(t *testing.T) {
	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)

	h := NewHandler(logger, clusters, apps, ctrl)

	reqBody := `{"name":"test-cluster","kubeconfig_path":"/path/to/kubeconfig"}`
	req := httptest.NewRequest(http.MethodPost, "/clusters", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.Register(c); err != nil {
		t.Errorf("Register() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if clusters.Len() != 1 {
		t.Error("Cluster was not registered")
	}
}

func TestHandler_List(t *testing.T) {
	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)

	clusters.Add(&clustercore.Cluster{Name: "c1"})
	h := NewHandler(logger, clusters, apps, ctrl)

	req := httptest.NewRequest(http.MethodGet, "/clusters", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h.List(c); err != nil {
		t.Errorf("List() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestHandler_Unregister(t *testing.T) {
	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)

	clusters.Add(&clustercore.Cluster{Name: "c1"})
	h := NewHandler(logger, clusters, apps, ctrl)

	req := httptest.NewRequest(http.MethodDelete, "/clusters/c1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("name")
	c.SetParamValues("c1")

	if err := h.Unregister(c); err != nil {
		t.Errorf("Unregister() error = %v", err)
	}

	if clusters.Len() != 0 {
		t.Error("Cluster was not unregistered")
	}
}

func TestHandler_Register_InvalidJSON(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir, err := os.MkdirTemp("", "cluster-api-reg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("configs", 0o755); err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	h := NewHandler(logger, clusters, apps, ctrl)

	e.POST("/clusters", h.Register)

	req := httptest.NewRequest(http.MethodPost, "/clusters", strings.NewReader(`{not-json`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestHandler_HealthCheck_Accepted(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir, err := os.MkdirTemp("", "cluster-api-health-ok")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("configs", 0o755); err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	clusters.Add(&clustercore.Cluster{Name: "c1", KubeconfigPath: "/tmp/k"})
	ctrl := controller.NewController(logger, apps, clusters)
	h := NewHandler(logger, clusters, apps, ctrl)

	e.POST("/clusters/:name/check", h.HealthCheck)

	req := httptest.NewRequest(http.MethodPost, "/clusters/c1/check", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusAccepted, rec.Code, rec.Body.String())
	}

	cl, ok := clusters.Get("c1")
	if !ok || cl.Status != "CheckRequested" {
		t.Fatalf("expected cluster marked CheckRequested, got ok=%v cluster=%+v", ok, cl)
	}
}

func TestHandler_Register_UpdateExistingCluster(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir, err := os.MkdirTemp("", "cluster-api-update")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("configs", 0o755); err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	clusters.Add(&clustercore.Cluster{Name: "c1", KubeconfigPath: "/old/path"})
	ctrl := controller.NewController(logger, apps, clusters)
	h := NewHandler(logger, clusters, apps, ctrl)

	e.POST("/clusters", h.Register)

	body := `{"name":"c1","kubeconfig_path":"/new/kubeconfig"}`
	req := httptest.NewRequest(http.MethodPost, "/clusters", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%q", http.StatusOK, rec.Code, rec.Body.String())
	}
	got, ok := clusters.Get("c1")
	if !ok || got.KubeconfigPath != "/new/kubeconfig" {
		t.Fatalf("expected updated kubeconfig path, got ok=%v cluster=%+v", ok, got)
	}
}
