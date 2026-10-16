package app

import (
	"net/http/httptest"
	"strings"

	"abhiudayg.com/github/gitopsctl/internal/controller"
	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func newTestHandler() (*Handler, *echo.Echo, *appcore.Applications, *clustercore.Clusters) {
	e := echo.New()
	apps := appcore.NewApplications()
	clusters := clustercore.NewClusters()
	ctrl := controller.NewController(zap.NewNop(), apps, clusters)
	h := NewHandler(zap.NewNop(), apps, clusters, ctrl)
	return h, e, apps, clusters
}

func newJSONContext(e *echo.Echo, method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}
