package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteFileAtomic(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatalf("got %q (%v), want overwritten content", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestWriteFileAtomic_MissingDirectoryFailsWithoutCreatingTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")
	if err := WriteFileAtomic(path, []byte("x"), 0o644); err == nil {
		t.Fatal("expected error for missing directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target must not exist, stat err = %v", err)
	}
}

func TestWriteFileAtomic_RenameFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0o644); err == nil {
		t.Fatal("expected error renaming file over a directory")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
