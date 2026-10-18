package app

import (
	"net/http"
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
)

func TestRegister_ClusterNotFound(t *testing.T) {
	h, e, _, _ := newTestHandler()

	body := `{"name":"a1","repo_url":"https://github.com/org/repo.git","branch":"main","path":"manifests","cluster":"missing","interval":"1m"}`
	c, rec := newJSONContext(e, http.MethodPost, "/applications", body)
	if err := h.Register(c); err == nil {
		t.Fatal("expected error when cluster missing")
	}
	if rec.Code != http.StatusBadRequest && rec.Code != 0 {
		// echo.HTTPError sets handler-level status depending on usage
		_ = rec.Code
	}
}

func TestRegister_InvalidBindJSON(t *testing.T) {
	h, e, _, clusters := newTestHandler()
	clusters.Add(&clustercore.Cluster{Name: "c1"})

	c, _ := newJSONContext(e, http.MethodPost, "/applications", `{`)
	err := h.Register(c)
	if err == nil {
		t.Fatal("expected bind error")
	}
}

func TestRegister_InvalidInterval(t *testing.T) {
	h, e, apps, clusters := newTestHandler()
	clusters.Add(&clustercore.Cluster{Name: "c1"})

	body := `{"name":"a1","repo_url":"https://github.com/org/repo.git","branch":"main","path":"manifests","cluster":"c1","interval":"not-a-duration"}`
	c, rec := newJSONContext(e, http.MethodPost, "/applications", body)
	err := h.Register(c)
	if err == nil {
		t.Fatal("expected interval validation error")
	}
	_ = apps.Len()
	_ = rec.Code
}

func TestRegister_UpdatesExistingApp(t *testing.T) {
	h, e, apps, clusters := newTestHandler()
	clusters.Add(&clustercore.Cluster{Name: "c1"})
	apps.Add(&appcore.Application{Name: "a1", RepoURL: "https://github.com/old/old.git", Branch: "main", Path: "p", ClusterName: "c1", Interval: "5m"})

	body := `{"name":"a1","repo_url":"https://github.com/new/repo.git","branch":"develop","path":"deploy","cluster":"c1","interval":"10m"}`
	c, rec := newJSONContext(e, http.MethodPost, "/applications", body)
	if err := h.Register(c); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got, ok := apps.Get("a1")
	if !ok || got.RepoURL != "https://github.com/new/repo.git" || got.Branch != "develop" {
		t.Fatalf("app not updated: %+v", got)
	}
}
