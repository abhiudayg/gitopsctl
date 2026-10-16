package cluster

import (
	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Handler handles cluster-related HTTP requests.
type Handler struct {
	logger     *zap.Logger
	clusters   *clustercore.Clusters
	apps       *appcore.Applications
	controller *controller.Controller
}

// NewHandler creates a new cluster handler.
func NewHandler(logger *zap.Logger, clusters *clustercore.Clusters, apps *appcore.Applications, controller *controller.Controller) *Handler {
	return &Handler{
		logger:     logger,
		clusters:   clusters,
		apps:       apps,
		controller: controller,
	}
}

// RegisterRoutes registers all cluster-related routes.
func RegisterRoutes(g *echo.Group, handler *Handler) {
	// Clusters Management
	g.POST("/clusters", handler.Register)
	g.GET("/clusters", handler.List)
	g.GET("/clusters/:name", handler.Get)
	g.DELETE("/clusters/:name", handler.Unregister)
	g.POST("/clusters/:name/check", handler.HealthCheck)
}
