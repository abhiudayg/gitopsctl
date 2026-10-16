package app

import (
	"errors"
	"net/http"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Plan reports what a sync of the named application would create, update or prune, without applying anything.
func (h *Handler) Plan(c echo.Context) error {
	name := c.Param("name")

	result, err := h.controller.PlanApp(c.Request().Context(), name)
	switch {
	case errors.Is(err, controller.ErrAppNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Application not found")
	case errors.Is(err, controller.ErrClusterNotFound):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Cluster for application not found")
	case err != nil:
		h.logger.Error("Failed to compute plan", zap.String("name", name), zap.Error(err))
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to compute plan; see controller logs")
	}

	items := make([]PlanItemResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, PlanItemResponse{
			Action:    string(item.Action),
			Group:     item.Resource.Group,
			Version:   item.Resource.Version,
			Kind:      item.Resource.Kind,
			Namespace: item.Resource.Namespace,
			Name:      item.Resource.Name,
		})
	}
	return c.JSON(http.StatusOK, PlanResponse{App: result.App, Commit: result.Commit, Items: items})
}
