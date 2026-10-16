package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
	"go.uber.org/zap"
)

func TestValidateAndNormalizeClusterInput_WithExplicitPath(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "kubeconfig")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kcfg := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(kcfg, []byte("apiVersion: v1\nkind: Config\n"), 0644); err != nil {
		t.Fatalf("failed writing kubeconfig: %v", err)
	}

	clusterRegName = "cluster-a"
	clusterKubeconfigPath = kcfg
	cfg, err := validateAndNormalizeClusterInput()
	if err != nil {
		t.Fatalf("validateAndNormalizeClusterInput() error = %v", err)
	}
	if cfg.name != "cluster-a" || cfg.resolvedPath == "" {
		t.Fatalf("unexpected normalized config: %+v", cfg)
	}
}

func TestHandleExistingCluster(t *testing.T) {
	forceCluster = false
	if err := handleExistingCluster(false, "c1"); err != nil {
		t.Fatalf("expected nil when not existing, got %v", err)
	}
	if err := handleExistingCluster(true, "c1"); err == nil {
		t.Fatal("expected error when cluster exists without force")
	}
	forceCluster = true
	if err := handleExistingCluster(true, "c1"); err != nil {
		t.Fatalf("expected nil with force, got %v", err)
	}
}

func TestCreateClusterConfig_StatusByTestFlag(t *testing.T) {
	cfg := &clusterRegistrationConfig{name: "c1", resolvedPath: "/tmp/kube"}
	testConnection = false
	cl := createClusterConfig(cfg)
	if cl.Status != "Pending" {
		t.Fatalf("expected Pending without test flag, got %s", cl.Status)
	}
	testConnection = true
	cl = createClusterConfig(cfg)
	if cl.Status != "Active" {
		t.Fatalf("expected Active with test flag, got %s", cl.Status)
	}
}

func TestRegisterClusterHelpers_DryRunAndSave(t *testing.T) {
	origWd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "register-cluster-helpers")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	defer func() { _ = os.Chdir(origWd) }()

	_ = os.Chdir(tmpDir)
	logger = zap.NewNop()

	cl := &clustercore.Cluster{
		Name:           "c1",
		KubeconfigPath: "/tmp/kube",
		RegisteredAt:   time.Now(),
		Status:         "Pending",
		Message:        "msg",
	}
	if err := displayDryRunClusterSummary(cl, false); err != nil {
		t.Fatalf("displayDryRunClusterSummary() error = %v", err)
	}
	if err := saveAndConfirmCluster(cl, false); err != nil {
		t.Fatalf("saveAndConfirmCluster() error = %v", err)
	}
}

func TestTestClusterConnectivity_InvalidPath(t *testing.T) {
	cfg := &clusterRegistrationConfig{name: "c1", resolvedPath: "/no/such/kubeconfig"}
	if err := testClusterConnectivity(cfg); err == nil {
		t.Fatal("expected connectivity test to fail for invalid kubeconfig path")
	}
}

func TestValidateAndNormalizeClusterInput_EmptyName(t *testing.T) {
	clusterRegName = ""
	clusterKubeconfigPath = "/tmp/kubeconfig-placeholder"
	if _, err := validateAndNormalizeClusterInput(); err == nil {
		t.Fatal("expected error for empty cluster name")
	}
}

func TestValidateAndNormalizeClusterInput_KubeconfigUndetectable(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "home")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpHome) }()

	origHome := os.Getenv("HOME")
	origKube := os.Getenv("KUBECONFIG")
	defer func() {
		_ = os.Setenv("HOME", origHome)
		if origKube == "" {
			_ = os.Unsetenv("KUBECONFIG")
		} else {
			_ = os.Setenv("KUBECONFIG", origKube)
		}
	}()

	_ = os.Setenv("HOME", tmpHome)
	_ = os.Unsetenv("KUBECONFIG")

	clusterRegName = "c1"
	clusterKubeconfigPath = ""
	if _, err := validateAndNormalizeClusterInput(); err == nil {
		t.Fatal("expected error when kubeconfig path cannot be auto-detected")
	}
}
