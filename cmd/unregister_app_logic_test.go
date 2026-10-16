package cmd

import (
	"os"
	"strings"
	"testing"

	"abhiudayg.com/github/gitopsctl/internal/core/app"
)

func TestValidateUnregisterInput(t *testing.T) {
	unregisterAppName = "  app-a  "
	if err := validateUnregisterInput(); err != nil {
		t.Fatalf("validateUnregisterInput() error = %v", err)
	}
	if unregisterAppName != "app-a" {
		t.Fatalf("expected trimmed name, got %q", unregisterAppName)
	}

	unregisterAppName = strings.Repeat("a", 64)
	if err := validateUnregisterInput(); err == nil {
		t.Fatal("expected error for too-long name")
	}
}

func TestConfirmAction_YesInput(t *testing.T) {
	orig := os.Stdin
	defer func() { os.Stdin = orig }()

	f, err := os.CreateTemp("", "stdin-confirm")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = f

	if !confirmAction("confirm?") {
		t.Fatal("expected confirmAction to return true for yes input")
	}
}

func TestConfirmUnregister_AcceptsYes(t *testing.T) {
	orig := os.Stdin
	defer func() { os.Stdin = orig }()

	f, err := os.CreateTemp("", "stdin-unregister")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = f

	target := &app.Application{
		Name:        "my-app",
		RepoURL:     "https://example.com/org/repo.git",
		Branch:      "main",
		ClusterName: "c1",
		Status:      "Synced",
	}
	if !confirmUnregister(target) {
		t.Fatal("expected confirmUnregister to accept yes")
	}
}
