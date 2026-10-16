package app

import (
	"net/http"

	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Sync handles manual sync requests for an application.
// It asks the controller to run an immediate sync for the named app and returns 202 Accepted.
func (h *Handler) Sync(c echo.Context) error {
	name := c.Param("name")

	h.apps.Lock()
	app, ok := h.apps.Get(name)
	if !ok {
		h.apps.Unlock()
		h.logger.Warn("Manual sync requested for non-existent application", zap.String("name", name))
		return echo.NewHTTPError(http.StatusNotFound, "Application not found")
	}
	app.Status = "SyncRequested"
	app.Message = "Manual sync requested."
	h.apps.Unlock()

	h.controller.TriggerSync(name)
	h.controller.Emit(events.TypeAppSyncRequested, map[string]any{
		"app": name,
	})
	// No need to save to disk here, controller's next loop or signal will handle it.
	h.logger.Info("Manual sync requested for application", zap.String("name", name))
	return c.JSON(http.StatusAccepted, SyncTriggerResponse{
		Message: "Manual sync requested. The controller will process it shortly.",
		Status:  "SyncRequested",
	})
}
