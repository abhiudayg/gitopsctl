package cluster

import (
	"net/http"
	"time"

	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Register handles the registration of a new Kubernetes cluster.
// It binds the request payload to a RegisterRequest struct, validates it,
// and either adds a new cluster or updates an existing one.
func (h *Handler) Register(c echo.Context) error {
	req := new(RegisterRequest)
	if err := c.Bind(req); err != nil {
		h.logger.Error("Failed to bind register cluster request", zap.Error(err))
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request payload")
	}
	if c.Echo().Validator != nil {
		if err := c.Validate(req); err != nil {
			h.logger.Error("Failed to validate register cluster request", zap.Error(err))
			return err
		}
	}

	h.clusters.Lock()
	defer h.clusters.Unlock()

	if _, exists := h.clusters.Get(req.Name); exists {
		h.logger.Warn("Cluster with this name already exists. Updating its kubeconfig.", zap.String("name", req.Name))
	}

	newCluster := &clustercore.Cluster{
		Name:           req.Name,
		KubeconfigPath: req.KubeconfigPath,
		RegisteredAt:   time.Now(),
		Status:         "Active",
		Message:        "Cluster registered successfully.",
	}
	h.clusters.Add(newCluster)

	if err := clustercore.SaveClusters(h.clusters, clustercore.DefaultClusterConfigFile); err != nil {
		h.logger.Error("Failed to save clusters after registration", zap.Error(err))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save cluster configuration")
	}

	h.controller.TriggerClusterHealthCheck(req.Name)
	h.controller.Emit(events.TypeClusterRegistered, map[string]any{
		"cluster":    req.Name,
		"kubeconfig": req.KubeconfigPath,
	})

	h.logger.Info("Cluster registered/updated via API", zap.String("name", req.Name))
	return c.JSON(http.StatusOK, map[string]string{"message": "Cluster registered/updated successfully", "name": req.Name})
}
