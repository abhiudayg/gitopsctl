package app

import (
	"net/http"
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
)

func TestUnregister_RemovesApplication(t *testing.T) {
	h, e, apps, _ := newTestHandler()
	apps.Add(&appcore.Application{Name: "a1"})

	c, rec := newJSONContext(e, http.MethodDelete, "/applications/a1", "")
	c.SetParamNames("name")
	c.SetParamValues("a1")
	if err := h.Unregister(c); err != nil {
		t.Fatalf("Unregister() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if apps.Len() != 0 {
		t.Fatalf("expected app to be deleted, len=%d", apps.Len())
	}
}
