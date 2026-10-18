package app

import (
	"net/http"
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
)

func TestList_ReturnsRegisteredApplications(t *testing.T) {
	h, e, apps, _ := newTestHandler()
	apps.Add(&appcore.Application{Name: "a1"})

	c, rec := newJSONContext(e, http.MethodGet, "/applications", "")
	if err := h.List(c); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
