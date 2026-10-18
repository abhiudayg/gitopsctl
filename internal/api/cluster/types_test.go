package cluster

import (
	"testing"
	"time"

	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
)

func TestConvertToResponse_MapsFields(t *testing.T) {
	now := time.Now()
	src := &clustercore.Cluster{
		Name:           "c1",
		KubeconfigPath: "/tmp/kubeconfig",
		RegisteredAt:   now,
		Status:         "Healthy",
		Message:        "ok",
		LastCheckedAt:  now,
	}

	got := ConvertToResponse(src)
	if got.Name != src.Name || got.KubeconfigPath != src.KubeconfigPath {
		t.Fatalf("response fields not mapped correctly: %+v", got)
	}
	if !got.RegisteredAt.Equal(src.RegisteredAt) || !got.LastCheckedAt.Equal(src.LastCheckedAt) {
		t.Fatalf("time fields not mapped correctly: %+v", got)
	}
}
