package app

import (
	"errors"
	"net/http"
	"testing"

	appcore "aeswibon.com/github/gitopsctl/internal/core/app"
	"github.com/labstack/echo/v4"
)

func TestApprove_RejectsMissingCommit(t *testing.T) {
	h, e, _, _ := newTestHandler()
	c, _ := newJSONContext(e, http.MethodPost, "/applications/a1/approve", `{}`)
	c.SetParamNames("name")
	c.SetParamValues("a1")

	err := h.Approve(c)
	if err == nil {
		t.Fatal("expected bad request error when commitHash is missing")
	}
}

func TestApprove_RejectsInvalidJSON(t *testing.T) {
	h, e, _, _ := newTestHandler()
	c, _ := newJSONContext(e, http.MethodPost, "/applications/a1/approve", `{broken`)
	c.SetParamNames("name")
	c.SetParamValues("a1")

	if err := h.Approve(c); err == nil {
		t.Fatal("expected error for invalid JSON body")
	}
}

func TestApprove_AcceptsCommitHash(t *testing.T) {
	h, e, apps, _ := newTestHandler()
	apps.Add(&appcore.Application{Name: "a1", Interval: "1m"})

	c, rec := newJSONContext(e, http.MethodPost, "/applications/a1/approve", `{"commitHash":"deadbeef"}`)
	c.SetParamNames("name")
	c.SetParamValues("a1")

	if err := h.Approve(c); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
}

func TestApprove_UnknownApplicationIsNotFound(t *testing.T) {
	h, e, _, _ := newTestHandler()
	c, _ := newJSONContext(e, http.MethodPost, "/applications/ghost/approve", `{"commitHash":"deadbeef"}`)
	c.SetParamNames("name")
	c.SetParamValues("ghost")

	err := h.Approve(c)
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %v", err)
	}
}
