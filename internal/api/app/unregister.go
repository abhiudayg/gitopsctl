package app

import (
	"net/http"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Unregister handles the removal of an application by name.
// It deletes the application from the applications store and saves the updated configuration.
// If the application does not exist, it returns a 404 Not Found error.
// This is useful for cleaning up applications that are no longer needed or have been removed from the Git repository.
func (h *Handler) Unregister(c echo.Context) error {
	name := c.Param("name")

	h.apps.RLock()
	_, exists := h.apps.Get(name)
	h.apps.RUnlock()
	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "Application not found")
	}

	// Stop the controller's goroutine for this application FIRST
	h.controller.StopApp(name)
	h.controller.Emit(events.TypeAppUnregistered, map[string]any{
		"app": name,
	})
	h.apps.Lock()
	defer h.apps.Unlock()

	// Remove the application from the store
	h.apps.Delete(name)
	if err := appcore.SaveApplications(h.apps, appcore.DefaultAppConfigFile); err != nil {
		h.logger.Error("Failed to save applications after unregister", zap.Error(err))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to remove application configuration")
	}

	h.logger.Info("Application unregistered via API", zap.String("name", name))
	return c.JSON(http.StatusOK, map[string]string{"message": "Application unregistered successfully", "name": name})
}
