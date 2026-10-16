package sessions

import (
	"errors"
	"net/http"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/policy"
	"abhiudayg.com/github/gitopsctl/internal/session"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

const AgentKey = "gitopsctl.agent"

type Handler struct {
	logger  *zap.Logger
	manager *session.Manager
}

func NewHandler(logger *zap.Logger, manager *session.Manager) *Handler {
	return &Handler{logger: logger, manager: manager}
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	g.POST("/sessions", h.Create)
	g.GET("/sessions", h.List)
	g.GET("/sessions/:id", h.Get)
	g.POST("/sessions/:id/deploy", h.Deploy)
	g.DELETE("/sessions/:id", h.Teardown)
}

type CreateRequest struct {
	Agent string `json:"agent"`
	TTL   string `json:"ttl"`
}

type DeployRequest struct {
	Repo string `json:"repo"`
	Ref  string `json:"ref"`
	Path string `json:"path"`
}

type ViolationResponse struct {
	Rule    string `json:"rule"`
	Object  string `json:"object,omitempty"`
	Message string `json:"message"`
}

type PolicyErrorResponse struct {
	Error      string              `json:"error"`
	Violations []ViolationResponse `json:"violations"`
}

func caller(c echo.Context) string {
	agent, _ := c.Get(AgentKey).(string)
	return agent
}

func (h *Handler) Create(c echo.Context) error {
	var req CreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	agent := caller(c)
	switch {
	case agent == "" && req.Agent == "":
		return echo.NewHTTPError(http.StatusBadRequest, "agent is required")
	case agent != "" && req.Agent != "" && req.Agent != agent:
		return echo.NewHTTPError(http.StatusForbidden, "cannot create sessions for another agent")
	case agent == "":
		agent = req.Agent
	}
	var ttl time.Duration
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "ttl must look like 30m")
		}
		ttl = parsed
	}
	created, err := h.manager.Create(c.Request().Context(), agent, ttl)
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusCreated, created)
}

func (h *Handler) List(c echo.Context) error {
	list := h.manager.List(caller(c))
	if list == nil {
		list = []session.Session{}
	}
	return c.JSON(http.StatusOK, list)
}

func (h *Handler) Get(c echo.Context) error {
	got, err := h.manager.Get(c.Param("id"), caller(c))
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, got)
}

func (h *Handler) Deploy(c echo.Context) error {
	var req DeployRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	deployed, err := h.manager.Deploy(c.Request().Context(), c.Param("id"), caller(c), session.DeployRequest{Repo: req.Repo, Ref: req.Ref, Path: req.Path})
	if err != nil {
		return h.fail(c, err)
	}
	return c.JSON(http.StatusOK, deployed)
}

func (h *Handler) Teardown(c echo.Context) error {
	torn, err := h.manager.Teardown(c.Request().Context(), c.Param("id"), caller(c))
	if errors.Is(err, session.ErrNotFound) {
		return h.fail(c, err)
	}
	if err != nil {
		h.logger.Warn("Session teardown incomplete; the janitor will retry", zap.String("session", c.Param("id")), zap.Error(err))
		return c.JSON(http.StatusAccepted, torn)
	}
	return c.JSON(http.StatusOK, torn)
}

func (h *Handler) fail(c echo.Context, err error) error {
	if violations := collectViolations(err); len(violations) > 0 {
		body := PolicyErrorResponse{Error: "manifests rejected by policy", Violations: make([]ViolationResponse, 0, len(violations))}
		for _, v := range violations {
			body.Violations = append(body.Violations, ViolationResponse{Rule: v.Rule, Object: v.Object, Message: v.Message})
		}
		return c.JSON(http.StatusUnprocessableEntity, body)
	}
	switch {
	case errors.Is(err, session.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	case errors.Is(err, session.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	case errors.Is(err, session.ErrInvalid):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, session.ErrLimit):
		return echo.NewHTTPError(http.StatusTooManyRequests, err.Error())
	case errors.Is(err, session.ErrNotActive):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	h.logger.Error("Session request failed", zap.Error(err))
	return echo.NewHTTPError(http.StatusBadGateway, "request failed; see controller logs")
}

func collectViolations(err error) []policy.Violation {
	var found []policy.Violation
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if v, ok := e.(policy.Violation); ok {
			found = append(found, v)
			return
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	return found
}
