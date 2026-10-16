package app

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type ApproveRequest struct {
	CommitHash string `json:"commitHash" validate:"required"`
}

func (h *Handler) Approve(c echo.Context) error {
	name := c.Param("name")
	var req ApproveRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.CommitHash == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "commitHash is required")
	}

	h.apps.RLock()
	_, exists := h.apps.Get(name)
	h.apps.RUnlock()
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "Application not found")
	}

	h.logger.Info("Approving sync for application", zap.String("app", name), zap.String("commit", req.CommitHash))
	h.controller.ApproveSync(name, req.CommitHash)

	return c.JSON(http.StatusAccepted, map[string]string{
		"message": "Sync approved and triggered",
		"app":     name,
		"commit":  req.CommitHash,
	})
}
