package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileWritesCoreAndWorkloadConfigs(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{AppHost: "app.lvh.me:3000", PreviewBaseDomain: "preview.lvh.me"})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		Sandboxes: map[string][]WorkloadPort{
			"sandbox-1": {
				{Private: 3000, Public: 43000},
				{Private: 3000, Public: 43010},
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
				{Service: "web", Private: 80, Public: 50090},
				{Service: "api", Private: 3000, Public: 53000},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile routes: %v", err)
	}

	assertFileContains(t, filepath.Join(dir, "00-core.yaml"), "preview-forward-auth-placeholder")
	assertFileContains(t, filepath.Join(dir, "00-core.yaml"), "address: \"http://server:8080/auth/proxy/authorize\"")
	assertFileContains(t, filepath.Join(dir, "00-core.yaml"), "trustForwardHeader: false")
	assertFileContains(t, filepath.Join(dir, "00-core.yaml"), "Host(`app.lvh.me:3000`) && (PathPrefix(`/api`)")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "Host(`sbx-sandbox-1-p3000-")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), ".preview.lvh.me`) && PathPrefix(`/`)")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "PathPrefix(`/_sandbox/auth/`)")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "url: \"http://host.docker.internal:43000\"")
	assertFileNotContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "url: \"http://host.docker.internal:43010\"")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "preview-header-placeholder")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "preview-forward-auth-placeholder")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "X-Open-Sandbox-Proxy-Type: \"sandboxes\"")
	assertFileNotContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "-         -")
	assertFileContains(t, filepath.Join(dir, "container-ctr-1.yaml"), "Host(`ctr-ctr-1-p8080-")
	assertFileContains(t, filepath.Join(dir, "container-ctr-1.yaml"), "url: \"http://host.docker.internal:48080\"")
	assertFileContains(t, filepath.Join(dir, "compose-demo.yaml"), "Host(`cmp-demo-web-p80-")
	assertFileContains(t, filepath.Join(dir, "compose-demo.yaml"), "Host(`cmp-demo-api-p3000-")
	assertFileContains(t, filepath.Join(dir, "compose-demo.yaml"), "url: \"http://host.docker.internal:50080\"")
	assertFileNotContains(t, filepath.Join(dir, "compose-demo.yaml"), "url: \"http://host.docker.internal:50090\"")
	assertFileContains(t, filepath.Join(dir, "compose-demo.yaml"), "X-Open-Sandbox-Proxy-Type: \"compose\"")
	assertFileNotContains(t, filepath.Join(dir, "compose-demo.yaml"), "-         -")
}

func TestReconcileRemovesStaleWorkloadFiles(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{AppHost: "app.lvh.me:3000", PreviewBaseDomain: "preview.lvh.me"})
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

func TestReconcileUpdatesRouteFilesAndKeepsUnmanagedFiles(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{AppHost: "app.lvh.me:3000", PreviewBaseDomain: "preview.lvh.me"})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "sandbox-stale.yaml"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale sandbox route: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write unmanaged file: %v", err)
	}

	if err := writer.Reconcile(WorkloadRoutes{
		Sandboxes: map[string][]WorkloadPort{
			"sandbox-1": {{Private: 3000, Public: 43000}},
		},
	}); err != nil {
		t.Fatalf("seed reconcile routes: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "sandbox-stale.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected stale sandbox route to be removed, err=%v", err)
	}
	assertFileContains(t, filepath.Join(dir, "notes.txt"), "keep me")
	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "url: \"http://host.docker.internal:43000\"")

	if err := writer.Reconcile(WorkloadRoutes{
		Sandboxes: map[string][]WorkloadPort{
			"sandbox-1": {{Private: 3000, Public: 43123}},
		},
	}); err != nil {
		t.Fatalf("update reconcile routes: %v", err)
	}

	assertFileContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "url: \"http://host.docker.internal:43123\"")
	assertFileNotContains(t, filepath.Join(dir, "sandbox-sandbox-1.yaml"), "url: \"http://host.docker.internal:43000\"")
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

func assertFileNotContains(t *testing.T, path string, expected string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(content), expected) {
		t.Fatalf("did not expect %q in %s", expected, path)
	}
}

func assertMiddlewareOrder(t *testing.T, path string, first string, second string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	raw := string(content)
	firstIndex := strings.Index(raw, first)
	secondIndex := strings.Index(raw, second)
	if firstIndex == -1 || secondIndex == -1 {
		t.Fatalf("expected both %q and %q in %s", first, second, path)
	}
	if firstIndex >= secondIndex {
		t.Fatalf("expected %q to appear before %q in %s", first, second, path)
	}
}
