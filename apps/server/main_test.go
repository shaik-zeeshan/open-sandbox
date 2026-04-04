package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDBPathFromServerRoot(t *testing.T) {
	serverRoot := newServerRoot(t, t.TempDir())

	got := defaultDBPathFrom(serverRoot)
	want := filepath.Join(serverRoot, "open-sandbox.db")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultDBPathFromRepoRoot(t *testing.T) {
	repoRoot := t.TempDir()
	serverRoot := newServerRoot(t, filepath.Join(repoRoot, "apps", "server"))

	got := defaultDBPathFrom(repoRoot)
	want := filepath.Join(serverRoot, "open-sandbox.db")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultDBPathFromNestedServerDir(t *testing.T) {
	repoRoot := t.TempDir()
	serverRoot := newServerRoot(t, filepath.Join(repoRoot, "apps", "server"))
	nestedDir := filepath.Join(serverRoot, "tmp", "watcher")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	got := defaultDBPathFrom(nestedDir)
	want := filepath.Join(serverRoot, "open-sandbox.db")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultDBPathFromUnknownDir(t *testing.T) {
	got := defaultDBPathFrom(t.TempDir())
	if got != "open-sandbox.db" {
		t.Fatalf("expected fallback db path, got %q", got)
	}
}

func newServerRoot(t *testing.T, dir string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create server root: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "main.go"))
	writeTestFile(t, filepath.Join(dir, "go.mod"))

	return dir
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to write test file %q: %v", path, err)
	}
}
