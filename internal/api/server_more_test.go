package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestServer_StreamEvents_NotImplementedWithoutSink(t *testing.T) {
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	s := NewServer(logger, apps, clusters, ctrl, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	c := s.Echo().NewContext(req, rec)
	if err := s.StreamEvents(c); err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rec.Code)
	}
}

func TestServer_StartAndStop_Methods(t *testing.T) {
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	s := NewServer(logger, apps, clusters, ctrl, nil, nil)

	// invalid address should produce an error and still cover Start path.
	if err := s.Start("invalid-address"); err == nil {
		t.Fatal("expected start error for invalid address")
	}
	_ = s.Stop(context.Background())
}
