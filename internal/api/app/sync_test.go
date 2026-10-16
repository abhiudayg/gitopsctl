package app

import (
	"net/http"
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
)

func TestSync_UpdatesApplicationState(t *testing.T) {
	h, e, apps, _ := newTestHandler()
	apps.Add(&appcore.Application{Name: "a1", Status: "Pending"})
	c, rec := newJSONContext(e, http.MethodPost, "/applications/a1/sync", "")
	c.SetParamNames("name")
	c.SetParamValues("a1")

	if err := h.Sync(c); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", rec.Code)
	}
	a, _ := apps.Get("a1")
	if a.Status != "SyncRequested" {
		t.Fatalf("expected status SyncRequested, got %s", a.Status)
	}
}
