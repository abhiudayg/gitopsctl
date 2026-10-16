package cmd

import (
	"testing"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"abhiudayg.com/github/gitopsctl/internal/utils"
)

func TestFilterClustersForList(t *testing.T) {
	items := []utils.Renderable{
		&cluster.Cluster{Name: "c1", Status: "Active"},
		&cluster.Cluster{Name: "c2", Status: "Error"},
	}
	got := filterClustersForList(items, "active")
	if len(got) != 1 {
		t.Fatalf("expected one active cluster, got %d", len(got))
	}
}

func TestSortClustersForList(t *testing.T) {
	now := time.Now()
	items := []utils.Renderable{
		&cluster.Cluster{Name: "z", Status: "Error", RegisteredAt: now},
		&cluster.Cluster{Name: "a", Status: "Active", RegisteredAt: now.Add(-time.Hour)},
	}
	sortClustersForList(items, "name")
	if items[0].(*cluster.Cluster).Name != "a" {
		t.Fatalf("expected a first by name")
	}
	sortClustersForList(items, "registered")
	if items[0].(*cluster.Cluster).Name != "a" {
		t.Fatalf("expected oldest first by registered")
	}

	items = []utils.Renderable{
		&cluster.Cluster{Name: "zeta", Status: "pending", RegisteredAt: now},
		&cluster.Cluster{Name: "alpha", Status: "active", RegisteredAt: now},
		&cluster.Cluster{Name: "beta", Status: "active", RegisteredAt: now},
	}
	sortClustersForList(items, "status")
	if items[0].(*cluster.Cluster).Name != "alpha" {
		t.Fatalf("status sort: expected alpha first, got %s", items[0].(*cluster.Cluster).Name)
	}
}
