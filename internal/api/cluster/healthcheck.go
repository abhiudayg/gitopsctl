package cluster

import (
	"net/http"

	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// HealthCheck handles manual health check requests for a Kubernetes cluster.
// It updates the cluster's status to "CheckRequested" and logs the request.
func (h *Handler) HealthCheck(c echo.Context) error {
	name := c.Param("name")

	h.clusters.Lock()
	defer h.clusters.Unlock()

	clusterToUpdate, exists := h.clusters.Get(name)
	if !exists {
		h.logger.Warn("Attempted to trigger check for non-existent cluster", zap.String("name", name))
		return echo.NewHTTPError(http.StatusNotFound, ErrorResponse{Message: "Cluster not found"})
	}

	h.controller.TriggerClusterHealthCheck(name)
	h.controller.Emit(events.TypeClusterHealthCheckRequested, map[string]any{
		"cluster": name,
	})
	clusterToUpdate.Status = "CheckRequested"
	clusterToUpdate.Message = "Manual health check requested. Controller received signal."
	h.logger.Info("Manual cluster health check requested via API", zap.String("name", name))

	return c.JSON(http.StatusAccepted, HealthCheckTriggerResponse{
		Message: "Manual cluster health check requested. The controller will process it shortly.",
		Status:  "CheckRequested",
	})
}
