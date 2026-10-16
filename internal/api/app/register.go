package app

import (
	"net/http"
	"strings"
	"time"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/events"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// Register handles the registration of a new application.
//
// It binds the request payload to a RegisterRequest struct, validates it,
// and either adds a new application or updates an existing one.
func (h *Handler) Register(c echo.Context) error {
	req := new(RegisterRequest)
	if err := c.Bind(req); err != nil {
		h.logger.Error("Failed to bind register application request", zap.Error(err))
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request payload")
	}
	if c.Echo().Validator != nil {
		if err := c.Validate(req); err != nil {
			h.logger.Error("Failed to validate register application request", zap.Error(err))
			return err
		}
	}

	req.Path = strings.TrimPrefix(strings.TrimSuffix(req.Path, "/"), "/")
	if req.ClusterName == "" {
		req.ClusterName = req.Cluster
	}

	// Validate the referenced cluster exists
	h.clusters.RLock()
	defer h.clusters.RUnlock()
	_, exists := h.clusters.Get(req.ClusterName)
	if !exists {
		h.logger.Error("Cluster not found for application registration", zap.String("cluster", req.ClusterName))
		return echo.NewHTTPError(http.StatusBadRequest, "Cluster '"+req.ClusterName+"' not found")
	}

	// Lock the applications map for modification
	h.apps.Lock()
	defer h.apps.Unlock()

	// Check if app already exists to decide between add/update
	existingApp, exists := h.apps.Get(req.Name)
	if exists {
		h.logger.Warn("Application with this name already exists. Updating it.", zap.String("name", req.Name))
		// Update existing application details
		existingApp.RepoURL = req.RepoURL
		existingApp.Branch = req.Branch
		existingApp.Path = req.Path
		existingApp.ClusterName = req.ClusterName
		existingApp.Interval = req.Interval
		parsedInterval, err := time.ParseDuration(req.Interval)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid interval format: "+err.Error())
		}
		existingApp.PollingInterval = parsedInterval
		// Reset status/message/failures on update, assuming it's a re-registration
		existingApp.Status = "Pending"
		existingApp.Message = "Application updated, awaiting next sync."
		existingApp.ConsecutiveFailures = 0

	} else {
		// Create new application
		parsedInterval, err := time.ParseDuration(req.Interval)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid interval format: "+err.Error())
		}
		newApp := &appcore.Application{
			Name:                req.Name,
			RepoURL:             req.RepoURL,
			Branch:              req.Branch,
			Path:                req.Path,
			ClusterName:         req.ClusterName,
			Interval:            req.Interval,
			PollingInterval:     parsedInterval,
			Status:              "Pending",
			Message:             "Application registered, awaiting first sync.",
			ConsecutiveFailures: 0,
		}
		h.apps.Add(newApp)
	}

	if err := appcore.SaveApplications(h.apps, appcore.DefaultAppConfigFile); err != nil {
		h.logger.Error("Failed to save applications after registration", zap.Error(err))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save application configuration")
	}

	h.controller.StartApp(req.Name)
	h.controller.Emit(events.TypeAppRegistered, map[string]any{
		"app":      req.Name,
		"repoURL":  req.RepoURL,
		"branch":   req.Branch,
		"path":     req.Path,
		"cluster":  req.ClusterName,
		"interval": req.Interval,
		"updated":  exists,
	})

	h.logger.Info("Application registered/updated via API", zap.String("name", req.Name))
	return c.JSON(http.StatusOK, map[string]string{"message": "Application registered/updated successfully", "name": req.Name})
}
