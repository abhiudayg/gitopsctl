package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func TestServer_StreamEvents_CancelExits(t *testing.T) {
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	stream := events.NewStreamSink()
	s := NewServer(logger, apps, clusters, ctrl, stream, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/events")

	if err := s.StreamEvents(c); err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
}

func TestServer_StreamEvents_ReceivesOneEvent(t *testing.T) {
	logger := zap.NewNop()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(logger, apps, clusters)
	stream := events.NewStreamSink()
	s := NewServer(logger, apps, clusters, ctrl, stream, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/events")

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = stream.Write(context.Background(), events.NewEnvelope("test", events.TypeAppRegistered, map[string]any{"app": "a1"}))
	}()

	err := s.StreamEvents(c)
	if err != nil {
		t.Fatalf("StreamEvents() error = %v", err)
	}
}
