package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileWritesCoreAndWorkloadConfigs(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir)
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		Sandboxes: map[string][]WorkloadPort{
			"sandbox-1": {
				{Private: 3000, Public: 43000},
				{Private: 80, Public: 40080},
			},
		},
		Containers: map[string][]WorkloadPort{
			"ctr-1": {
				{Private: 8080, Public: 48080},
			},
		},
		ComposeProjects: map[string][]ComposeServicePort{
			"demo": {
				{Service: "web", Private: 80, Public: 50080},
				{Service: "api", Private: 3000, Public: 53000},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile routes: %v", err)
	}

	assertFileContains(t, filepath.Join(dir, "00-core.yaml"), "preview-forward-auth-placeholder")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "PathPrefix(`/proxy/sandboxes/sandbox-1/3000/`)")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "preview-header-placeholder")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "preview-forward-auth-placeholder")
	assertFileContains(t, filepath.Join(dir, "container-ctr-1.yaml"), "PathPrefix(`/proxy/containers/ctr-1/8080/`)")
	assertFileContains(t, filepath.Join(dir, "compose-demo.yaml"), "PathPrefix(`/proxy/compose/demo/web/80/`)")
	assertFileContains(t, filepath.Join(dir, "compose-demo.yaml"), "PathPrefix(`/proxy/compose/demo/api/3000/`)")
}

func TestReconcileRemovesStaleWorkloadFiles(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir)
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	if err := writer.Reconcile(WorkloadRoutes{
		Sandboxes: map[string][]WorkloadPort{
			"sandbox-1": []WorkloadPort{{Private: 3000, Public: 43000}},
		},
		Containers: map[string][]WorkloadPort{
			"ctr-1": []WorkloadPort{{Private: 8080, Public: 48080}},
		},
		ComposeProjects: map[string][]ComposeServicePort{
			"demo": []ComposeServicePort{{Service: "web", Private: 80, Public: 50080}},
		},
	}); err != nil {
		t.Fatalf("seed reconcile routes: %v", err)
	}

	if err := writer.Reconcile(WorkloadRoutes{}); err != nil {
		t.Fatalf("reconcile empty routes: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "00-core.yaml" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("unexpected files after cleanup: %v", names)
	}
}

func assertFileContains(t *testing.T, path string, expected string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("expected %q in %s", expected, path)
	}
}
