package app

import (
	"testing"

	appcore "abhiudayg.com/github/gitopsctl/internal/core/app"
)

func TestConvertToResponse_MapsFields(t *testing.T) {
	in := &appcore.Application{
		Name:                "a1",
		RepoURL:             "https://github.com/org/repo.git",
		Branch:              "main",
		Path:                "manifests",
		ClusterName:         "c1",
		Interval:            "1m",
		Status:              "Synced",
		Message:             "ok",
		ConsecutiveFailures: 2,
	}
	out := ConvertToResponse(in)
	if out.Name != in.Name || out.ClusterName != in.ClusterName {
		t.Fatalf("unexpected mapping result: %+v", out)
	}
	if out.ConsecutiveFailures != 2 {
		t.Fatalf("expected failures to be mapped")
	}
}
