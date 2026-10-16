package cluster

import (
	"net/http"

	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Unregister handles the removal of a Kubernetes cluster by name.
// It deletes the cluster from the clusters store and saves the updated configuration.
func (h *Handler) Unregister(c echo.Context) error {
	name := c.Param("name")

	h.clusters.Lock()
	defer h.clusters.Unlock()

	_, exists := h.clusters.Get(name)
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "Cluster not found")
	}

	h.apps.RLock()
	defer h.apps.RUnlock()
	for _, app := range h.apps.List() {
		if app.ClusterName == name {
			return echo.NewHTTPError(http.StatusConflict, "Cluster '"+name+"' is in use by application '"+app.Name+"'. Please unregister or update applications first.")
		}
	}

	h.clusters.Delete(name)
	h.controller.Emit(events.TypeClusterUnregistered, map[string]any{
		"cluster": name,
	})
	if err := clustercore.SaveClusters(h.clusters, clustercore.DefaultClusterConfigFile); err != nil {
		h.logger.Error("Failed to save clusters after unregister", zap.Error(err))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to remove cluster configuration")
	}

	h.logger.Info("Cluster unregistered via API", zap.String("name", name))
	return c.JSON(http.StatusOK, map[string]string{"message": "Cluster unregistered successfully", "name": name})
}
