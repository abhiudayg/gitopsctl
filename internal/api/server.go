package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"aeswibon.com/github/gitopsctl/internal/api/app"
	"aeswibon.com/github/gitopsctl/internal/api/cluster"
	"aeswibon.com/github/gitopsctl/internal/controller"
	appcore "aeswibon.com/github/gitopsctl/internal/core/app"
	clustercore "aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Server represents the API server.
// It holds the Echo instance, logger, applications store, and controller reference.
type Server struct {
	// e is the Echo instance used for handling HTTP requests.
	e *echo.Echo
	// logger is the zap.Logger instance used for logging.
	logger *zap.Logger
	// apps is the reference to the applications store, which holds registered applications.
	apps *appcore.Applications
	// clusters is the reference to the clusters store, which holds registered Kubernetes clusters.
	clusters *clustercore.Clusters
	// controller is the reference to the main controller that manages application synchronization.
	controller *controller.Controller
	// stream is an in-memory subscriber sink used for SSE event streaming.
	stream *events.StreamSink
	// history is an in-memory ring buffer for recent events.
	history *events.HistorySink
	// tokens maps accepted bearer tokens to their scope; empty disables authentication.
	tokens map[string]Scope
	// corsOrigins lists allowed browser origins; empty disables CORS.
	corsOrigins []string
}

// ServerOption customises a Server.
type ServerOption func(*Server)

// WithAPITokens enables bearer-token authentication on /api/v1.
func WithAPITokens(tokens map[string]Scope) ServerOption {
	return func(s *Server) { s.tokens = tokens }
}

// WithCORSOrigins allows the given browser origins to call the API.
func WithCORSOrigins(origins []string) ServerOption {
	return func(s *Server) { s.corsOrigins = origins }
}

// NewServer creates a new API server instance.
// It initializes the Echo instance, sets up middleware, and registers routes.
func NewServer(logger *zap.Logger, apps *appcore.Applications, clusters *clustercore.Clusters, ctrl *controller.Controller, stream *events.StreamSink, history *events.HistorySink, opts ...ServerOption) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Validator = NewCustomValidator()
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info("request",
				zap.String("URI", v.URI),
				zap.Int("status", v.Status),
			)
			return nil
		},
	}))
	e.Use(middleware.Recover())

	s := &Server{
		e:          e,
		logger:     logger,
		apps:       apps,
		clusters:   clusters,
		controller: ctrl,
		stream:     stream,
		history:    history,
	}
	for _, opt := range opts {
		opt(s)
	}
	if len(s.corsOrigins) > 0 {
		e.Use(middleware.CORSWithConfig(middleware.CORSConfig{AllowOrigins: s.corsOrigins, AllowHeaders: []string{echo.HeaderAuthorization, echo.HeaderContentType}}))
	}

	s.registerRoutes()
	return s
}

// RegisterRoutes defines all API endpoints.
// It sets up the routes for managing applications, health checks, and other API functionalities.
func (s *Server) registerRoutes() {
	v1 := s.e.Group("/api/v1")
	if len(s.tokens) > 0 {
		v1.Use(authMiddleware(s.tokens, s.audit))
	}

	appHandler := app.NewHandler(s.logger, s.apps, s.clusters, s.controller)
	clusterHandler := cluster.NewHandler(s.logger, s.clusters, s.apps, s.controller)

	app.RegisterRoutes(v1, appHandler)
	cluster.RegisterRoutes(v1, clusterHandler)
	if s.stream != nil {
		v1.GET("/events", s.StreamEvents)
		v1.GET("/events/history", s.GetEventHistory)
	}

	s.e.GET("/health", s.HealthCheck)
	s.e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

}

func (s *Server) audit(typ events.Type, data map[string]any) {
	if s.controller != nil {
		s.controller.Emit(typ, data)
	}
}

// Echo returns the Echo instance used by the server.
// This is useful for accessing Echo-specific methods or configurations outside the server struct.
func (s *Server) Echo() *echo.Echo {
	return s.e
}

// Start starts the HTTP server.
// It binds the server to the specified address and begins listening for incoming requests.
func (s *Server) Start(address string) error {
	s.logger.Info("Starting API server", zap.String("address", address))
	return s.e.Start(address)
}

// Stop stops the HTTP server.
// It gracefully shuts down the server, allowing ongoing requests to complete.
// This method can be called from the controller or directly via an API endpoint.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Shutting down API server...")
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.e.Shutdown(timeoutCtx)
}

// HealthCheck is a simple endpoint to check if the API server is running.
// It responds with a 200 OK status and a simple message.
// This is useful for monitoring and health checks in production environments.
func (s *Server) HealthCheck(c echo.Context) error {
	// Simple health check: just respond with 200 OK
	return c.String(http.StatusOK, "OK")
}

// StreamEvents streams integration events over Server-Sent Events (SSE).
func (s *Server) StreamEvents(c echo.Context) error {
	if s.stream == nil {
		return c.NoContent(http.StatusNotImplemented)
	}

	w := c.Response().Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming unsupported")
	}

	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)
	flusher.Flush()

	eventsCh, unsubscribe := s.stream.Subscribe(256)
	defer unsubscribe()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	reqCtx := c.Request().Context()
	for {
		select {
		case <-reqCtx.Done():
			return nil
		case env, ok := <-eventsCh:
			if !ok {
				return nil
			}
			payload, err := json.Marshal(env)
			if err != nil {
				s.logger.Warn("failed to marshal SSE event", zap.Error(err))
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", env.ID, env.Type, payload); err != nil {
				return nil
			}
			flusher.Flush()
		case <-heartbeat.C:
			// SSE comment heartbeat keeps proxies from closing idle connections.
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}

// GetEventHistory returns the recent event history.
func (s *Server) GetEventHistory(c echo.Context) error {
	if s.history == nil {
		return c.NoContent(http.StatusNotImplemented)
	}
	return c.JSON(http.StatusOK, s.history.All())
}
