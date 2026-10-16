package app

import (
	"net/http"
	"testing"

	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
)

func TestRegister_CreatesApplication(t *testing.T) {
	h, e, apps, clusters := newTestHandler()
	clusters.Add(&clustercore.Cluster{Name: "c1"})

	c, rec := newJSONContext(e, http.MethodPost, "/applications", `{"name":"a1","repo_url":"https://github.com/org/repo.git","branch":"main","path":"/manifests/","cluster":"c1","interval":"1m"}`)
	if err := h.Register(c); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if apps.Len() != 1 {
		t.Fatalf("expected one app to be registered, got %d", apps.Len())
	}
}
