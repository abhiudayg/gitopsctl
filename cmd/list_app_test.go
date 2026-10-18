package cmd

import (
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
	"abhiudayg.com/github/gitopsctl/internal/utils"
)

func TestHandleEmptyAppsForList(t *testing.T) {
	if err := handleEmptyAppsForList(""); err != nil {
		t.Fatalf("handleEmptyAppsForList(\"\") error = %v", err)
	}
	if err := handleEmptyAppsForList("pending"); err != nil {
		t.Fatalf("handleEmptyAppsForList(filtered) error = %v", err)
	}
}

func TestFilterAppsForList(t *testing.T) {
	items := []utils.Renderable{
		&app.Application{Name: "a", Status: "active"},
		&app.Application{Name: "b", Status: "pending"},
		&app.Application{Name: "c", Status: "ACTIVE"},
	}

	filtered := filterAppsForList(items, "active")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 active apps, got %d", len(filtered))
	}
}

func TestSortAppsForList(t *testing.T) {
	items := []utils.Renderable{
		&app.Application{Name: "zeta", Status: "pending", Branch: "main"},
		&app.Application{Name: "alpha", Status: "active", Branch: "dev"},
		&app.Application{Name: "beta", Status: "active", Branch: "main"},
	}

	sortAppsForList(items, "status")
	if items[0].(*app.Application).Name != "alpha" {
		t.Fatalf("expected alpha first after status sort, got %s", items[0].(*app.Application).Name)
	}

	sortAppsForList(items, "branch")
	if items[0].(*app.Application).Branch != "dev" {
		t.Fatalf("expected dev branch first after branch sort, got %s", items[0].(*app.Application).Branch)
	}

	sortAppsForList(items, "name")
	if items[0].(*app.Application).Name != "alpha" {
		t.Fatalf("expected alpha first after name sort, got %s", items[0].(*app.Application).Name)
	}
}
