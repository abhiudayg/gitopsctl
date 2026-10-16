package app

import (
	"net/http"
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
)

func TestGet_Returns404WhenMissing(t *testing.T) {
	h, e, _, _ := newTestHandler()
	c, _ := newJSONContext(e, http.MethodGet, "/applications/missing", "")
	c.SetParamNames("name")
	c.SetParamValues("missing")

	err := h.Get(c)
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestGet_ReturnsApplicationWhenFound(t *testing.T) {
	h, e, apps, _ := newTestHandler()
	apps.Add(&appcore.Application{Name: "a1"})
	c, rec := newJSONContext(e, http.MethodGet, "/applications/a1", "")
	c.SetParamNames("name")
	c.SetParamValues("a1")

	if err := h.Get(c); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
