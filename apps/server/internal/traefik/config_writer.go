package traefik

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	coreConfigFileName = "00-core.yaml"
	hostGatewayURL     = "http://host.docker.internal"
)

type WorkloadPort struct {
	Private int
	Public  int
}

type ComposeServicePort struct {
	Service string
	Private int
	Public  int
}

type WorkloadRoutes struct {
	Sandboxes       map[string][]WorkloadPort
	Containers      map[string][]WorkloadPort
	ComposeProjects map[string][]ComposeServicePort
}

type ConfigWriter struct {
	dir string
	mu  sync.Mutex
}

func (w *ConfigWriter) Dir() string {
	if w == nil {
		return ""
	}
	return w.dir
}

func NewConfigWriter(dir string) (*ConfigWriter, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return nil, fmt.Errorf("traefik dynamic config dir is required")
	}

	return &ConfigWriter{dir: filepath.Clean(trimmed)}, nil
}

func (w *ConfigWriter) Reconcile(routes WorkloadRoutes) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fmt.Errorf("create traefik dynamic config dir: %w", err)
	}

	desiredFiles := map[string][]byte{
		coreConfigFileName: []byte(coreConfig()),
	}

	for _, sandboxID := range sortedKeys(routes.Sandboxes) {
		fileName := "sandbox-" + sanitizeFileToken(sandboxID) + ".yaml"
		content := workloadConfig("sandboxes", sandboxID, routes.Sandboxes[sandboxID])
		if len(content) == 0 {
			continue
		}
		desiredFiles[fileName] = content
	}

	for _, managedID := range sortedKeys(routes.Containers) {
		fileName := "container-" + sanitizeFileToken(managedID) + ".yaml"
		content := workloadConfig("containers", managedID, routes.Containers[managedID])
		if len(content) == 0 {
			continue
		}
		desiredFiles[fileName] = content
	}

	for _, projectName := range sortedKeys(routes.ComposeProjects) {
		fileName := "compose-" + sanitizeFileToken(projectName) + ".yaml"
		content := composeConfig(projectName, routes.ComposeProjects[projectName])
		if len(content) == 0 {
			continue
		}
		desiredFiles[fileName] = content
	}

	for fileName, content := range desiredFiles {
		if err := writeFileIfChanged(filepath.Join(w.dir, fileName), content); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return fmt.Errorf("list traefik dynamic config dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := desiredFiles[name]; ok {
			continue
		}
		if strings.HasPrefix(name, "sandbox-") || strings.HasPrefix(name, "container-") || strings.HasPrefix(name, "compose-") {
			if strings.HasSuffix(name, ".yaml") {
				if err := os.Remove(filepath.Join(w.dir, name)); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove stale traefik config %q: %w", name, err)
				}
			}
		}
	}

	return nil
}

func coreConfig() string {
	bt := "`"
	return "http:\n" +
		"  routers:\n" +
		"    server:\n" +
		"      entryPoints:\n" +
		"        - web\n" +
		"      rule: \"PathPrefix(" + bt + "/api" + bt + ") || PathPrefix(" + bt + "/auth" + bt + ") || PathPrefix(" + bt + "/health" + bt + ") || PathPrefix(" + bt + "/metrics" + bt + ") || PathPrefix(" + bt + "/swagger" + bt + ")\"\n" +
		"      service: server\n" +
		"      priority: 100\n" +
		"    client:\n" +
		"      entryPoints:\n" +
		"        - web\n" +
		"      rule: \"PathPrefix(" + bt + "/" + bt + ")\"\n" +
		"      service: client\n" +
		"      priority: 1\n" +
		"\n" +
		"  services:\n" +
		"    server:\n" +
		"      loadBalancer:\n" +
		"        servers:\n" +
		"          - url: \"http://server:8080\"\n" +
		"    client:\n" +
		"      loadBalancer:\n" +
		"        servers:\n" +
		"          - url: \"http://client:80\"\n" +
		"\n" +
		"  middlewares:\n" +
		"    preview-header-placeholder:\n" +
		"      headers:\n" +
		"        customRequestHeaders:\n" +
		"          X-Open-Sandbox-Preview: \"1\"\n" +
		"    preview-forward-auth-placeholder:\n" +
		"      forwardAuth:\n" +
		"        address: \"http://server:8080/auth/proxy/authorize\"\n" +
		"        trustForwardHeader: false\n"
}

func workloadConfig(workloadType string, workloadID string, ports []WorkloadPort) []byte {
	trimmedID := strings.TrimSpace(workloadID)
	if trimmedID == "" {
		return nil
	}

	normalizedPorts := normalizedWorkloadPorts(ports)
	if len(normalizedPorts) == 0 {
		return nil
	}

	escapedID := url.PathEscape(trimmedID)
	safeID := sanitizeResourceToken(trimmedID)

	var buf bytes.Buffer
	buf.WriteString("http:\n")
	buf.WriteString("  routers:\n")
	for _, port := range normalizedPorts {
		routerName := fmt.Sprintf("preview-%s-%s-%d-router", workloadType, safeID, port.Private)
		exactRouterName := fmt.Sprintf("preview-%s-%s-%d-exact-router", workloadType, safeID, port.Private)
		serviceName := fmt.Sprintf("preview-%s-%s-%d-service", workloadType, safeID, port.Private)
		stripName := fmt.Sprintf("preview-%s-%s-%d-strip", workloadType, safeID, port.Private)

		prefix := "/proxy/" + workloadType + "/" + escapedID + "/" + strconv.Itoa(port.Private)
		exactPathRule := "^" + regexp.QuoteMeta(prefix) + "$"
		pathRule := prefix + "/"
		buf.WriteString("    ")
		buf.WriteString(exactRouterName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"PathRegexp(`")
		buf.WriteString(exactPathRule)
		buf.WriteString("`)\"\n")
		buf.WriteString("      service: ")
		buf.WriteString(serviceName)
		buf.WriteString("\n")
		buf.WriteString("      middlewares:\n")
		buf.WriteString("        - preview-forward-auth-placeholder\n")
		buf.WriteString("        - ")
		buf.WriteString(stripName)
		buf.WriteString("\n")
		buf.WriteString("        - preview-header-placeholder\n")
		buf.WriteString("      priority: 210\n")

		buf.WriteString("    ")
		buf.WriteString(routerName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"PathPrefix(`")
		buf.WriteString(pathRule)
		buf.WriteString("`)\"\n")
		buf.WriteString("      service: ")
		buf.WriteString(serviceName)
		buf.WriteString("\n")
		buf.WriteString("      middlewares:\n")
		buf.WriteString("        - preview-forward-auth-placeholder\n")
		buf.WriteString("        - ")
		buf.WriteString(stripName)
		buf.WriteString("\n")
		buf.WriteString("        - preview-header-placeholder\n")
		buf.WriteString("      priority: 200\n")
	}

	buf.WriteString("\n")
	buf.WriteString("  services:\n")
	for _, port := range normalizedPorts {
		serviceName := fmt.Sprintf("preview-%s-%s-%d-service", workloadType, safeID, port.Private)
		upstreamURL := hostGatewayURL + ":" + strconv.Itoa(port.Public)
		buf.WriteString("    ")
		buf.WriteString(serviceName)
		buf.WriteString(":\n")
		buf.WriteString("      loadBalancer:\n")
		buf.WriteString("        servers:\n")
		buf.WriteString("          - url: \"")
		buf.WriteString(upstreamURL)
		buf.WriteString("\"\n")
	}

	buf.WriteString("\n")
	buf.WriteString("  middlewares:\n")
	for _, port := range normalizedPorts {
		stripName := fmt.Sprintf("preview-%s-%s-%d-strip", workloadType, safeID, port.Private)
		prefix := "/proxy/" + workloadType + "/" + escapedID + "/" + strconv.Itoa(port.Private)
		buf.WriteString("    ")
		buf.WriteString(stripName)
		buf.WriteString(":\n")
		buf.WriteString("      stripPrefix:\n")
		buf.WriteString("        forceSlash: true\n")
		buf.WriteString("        prefixes:\n")
		buf.WriteString("          - \"")
		buf.WriteString(prefix)
		buf.WriteString("\"\n")
	}

	return buf.Bytes()
}

func composeConfig(projectName string, ports []ComposeServicePort) []byte {
	trimmedProject := strings.TrimSpace(projectName)
	if trimmedProject == "" {
		return nil
	}

	normalizedPorts := normalizedComposePorts(ports)
	if len(normalizedPorts) == 0 {
		return nil
	}

	escapedProject := url.PathEscape(trimmedProject)
	safeProject := sanitizeResourceToken(trimmedProject)

	var buf bytes.Buffer
	buf.WriteString("http:\n")
	buf.WriteString("  routers:\n")
	for _, item := range normalizedPorts {
		escapedService := url.PathEscape(item.Service)
		safeService := sanitizeResourceToken(item.Service)
		routerName := fmt.Sprintf("preview-compose-%s-%s-%d-router", safeProject, safeService, item.Private)
		exactRouterName := fmt.Sprintf("preview-compose-%s-%s-%d-exact-router", safeProject, safeService, item.Private)
		serviceName := fmt.Sprintf("preview-compose-%s-%s-%d-service", safeProject, safeService, item.Private)
		stripName := fmt.Sprintf("preview-compose-%s-%s-%d-strip", safeProject, safeService, item.Private)

		prefix := "/proxy/compose/" + escapedProject + "/" + escapedService + "/" + strconv.Itoa(item.Private)
		exactPathRule := "^" + regexp.QuoteMeta(prefix) + "$"
		pathRule := prefix + "/"

		buf.WriteString("    ")
		buf.WriteString(exactRouterName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"PathRegexp(`")
		buf.WriteString(exactPathRule)
		buf.WriteString("`)\"\n")
		buf.WriteString("      service: ")
		buf.WriteString(serviceName)
		buf.WriteString("\n")
		buf.WriteString("      middlewares:\n")
		buf.WriteString("        - preview-forward-auth-placeholder\n")
		buf.WriteString("        - ")
		buf.WriteString(stripName)
		buf.WriteString("\n")
		buf.WriteString("        - preview-header-placeholder\n")
		buf.WriteString("      priority: 210\n")

		buf.WriteString("    ")
		buf.WriteString(routerName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"PathPrefix(`")
		buf.WriteString(pathRule)
		buf.WriteString("`)\"\n")
		buf.WriteString("      service: ")
		buf.WriteString(serviceName)
		buf.WriteString("\n")
		buf.WriteString("      middlewares:\n")
		buf.WriteString("        - preview-forward-auth-placeholder\n")
		buf.WriteString("        - ")
		buf.WriteString(stripName)
		buf.WriteString("\n")
		buf.WriteString("        - preview-header-placeholder\n")
		buf.WriteString("      priority: 200\n")
	}

	buf.WriteString("\n")
	buf.WriteString("  services:\n")
	for _, item := range normalizedPorts {
		safeService := sanitizeResourceToken(item.Service)
		serviceName := fmt.Sprintf("preview-compose-%s-%s-%d-service", safeProject, safeService, item.Private)
		upstreamURL := hostGatewayURL + ":" + strconv.Itoa(item.Public)
		buf.WriteString("    ")
		buf.WriteString(serviceName)
		buf.WriteString(":\n")
		buf.WriteString("      loadBalancer:\n")
		buf.WriteString("        servers:\n")
		buf.WriteString("          - url: \"")
		buf.WriteString(upstreamURL)
		buf.WriteString("\"\n")
	}

	buf.WriteString("\n")
	buf.WriteString("  middlewares:\n")
	for _, item := range normalizedPorts {
		escapedService := url.PathEscape(item.Service)
		safeService := sanitizeResourceToken(item.Service)
		stripName := fmt.Sprintf("preview-compose-%s-%s-%d-strip", safeProject, safeService, item.Private)
		prefix := "/proxy/compose/" + escapedProject + "/" + escapedService + "/" + strconv.Itoa(item.Private)
		buf.WriteString("    ")
		buf.WriteString(stripName)
		buf.WriteString(":\n")
		buf.WriteString("      stripPrefix:\n")
		buf.WriteString("        forceSlash: true\n")
		buf.WriteString("        prefixes:\n")
		buf.WriteString("          - \"")
		buf.WriteString(prefix)
		buf.WriteString("\"\n")
	}

	return buf.Bytes()
}

func normalizedWorkloadPorts(ports []WorkloadPort) []WorkloadPort {
	byPrivate := map[int]int{}
	for _, port := range ports {
		if port.Private <= 0 || port.Public <= 0 {
			continue
		}
		if existing, ok := byPrivate[port.Private]; !ok || port.Public < existing {
			byPrivate[port.Private] = port.Public
		}
	}

	normalized := make([]WorkloadPort, 0, len(byPrivate))
	for private, public := range byPrivate {
		normalized = append(normalized, WorkloadPort{Private: private, Public: public})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Private != normalized[j].Private {
			return normalized[i].Private < normalized[j].Private
		}
		return normalized[i].Public < normalized[j].Public
	})

	return normalized
}

func normalizedComposePorts(ports []ComposeServicePort) []ComposeServicePort {
	type key struct {
		service string
		private int
	}
	byKey := map[key]int{}
	for _, port := range ports {
		service := strings.TrimSpace(port.Service)
		if service == "" || port.Private <= 0 || port.Public <= 0 {
			continue
		}
		k := key{service: service, private: port.Private}
		if existing, ok := byKey[k]; !ok || port.Public < existing {
			byKey[k] = port.Public
		}
	}

	normalized := make([]ComposeServicePort, 0, len(byKey))
	for k, public := range byKey {
		normalized = append(normalized, ComposeServicePort{Service: k.service, Private: k.private, Public: public})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Service != normalized[j].Service {
			return normalized[i].Service < normalized[j].Service
		}
		if normalized[i].Private != normalized[j].Private {
			return normalized[i].Private < normalized[j].Private
		}
		return normalized[i].Public < normalized[j].Public
	})

	return normalized
}

func sanitizeResourceToken(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "workload"
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	lastDash := false
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}

	value := strings.Trim(b.String(), "-")
	if value == "" {
		return "workload"
	}
	return value
}

func sanitizeFileToken(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "workload"
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	value := strings.Trim(b.String(), "-._")
	if value == "" {
		return "workload"
	}
	return value
}

func sortedKeys[T any](input map[string]T) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeFileIfChanged(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read traefik config %q: %w", path, err)
	}

	tmpPath := path + ".tmp"
	if writeErr := os.WriteFile(tmpPath, content, 0o644); writeErr != nil {
		return fmt.Errorf("write traefik config %q: %w", path, writeErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace traefik config %q: %w", path, renameErr)
	}
	return nil
}
