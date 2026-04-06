package traefik

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	dir                 string
	appHost             string
	previewBaseDomain   string
	previewCallbackPath string
	mu                  sync.Mutex
}

type ConfigWriterOptions struct {
	AppHost             string
	PreviewBaseDomain   string
	PreviewCallbackPath string
}

func (w *ConfigWriter) Dir() string {
	if w == nil {
		return ""
	}
	return w.dir
}

func NewConfigWriter(dir string, options ...ConfigWriterOptions) (*ConfigWriter, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return nil, fmt.Errorf("traefik dynamic config dir is required")
	}

	cfg := ConfigWriterOptions{}
	if len(options) > 0 {
		cfg = options[0]
	}

	previewCallbackPath := strings.TrimSpace(cfg.PreviewCallbackPath)
	if previewCallbackPath == "" {
		previewCallbackPath = "/_sandbox/auth/callback"
	}
	previewCallbackPath = strings.TrimSuffix(ensureLeadingSlash(previewCallbackPath), "/")
	if previewCallbackPath == "" {
		previewCallbackPath = "/_sandbox/auth/callback"
	}

	return &ConfigWriter{
		dir:                 filepath.Clean(trimmed),
		appHost:             strings.TrimSpace(cfg.AppHost),
		previewBaseDomain:   strings.TrimSpace(strings.ToLower(cfg.PreviewBaseDomain)),
		previewCallbackPath: previewCallbackPath,
	}, nil
}

func (w *ConfigWriter) Reconcile(routes WorkloadRoutes) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fmt.Errorf("create traefik dynamic config dir: %w", err)
	}

	desiredFiles := map[string][]byte{
		coreConfigFileName: []byte(coreConfig(w.appHost)),
	}

	for _, sandboxID := range sortedKeys(routes.Sandboxes) {
		fileName := "sandbox-" + sanitizeFileToken(sandboxID) + ".yaml"
		content := workloadConfig("sandboxes", sandboxID, routes.Sandboxes[sandboxID], w.previewBaseDomain, w.previewCallbackPath)
		if len(content) == 0 {
			continue
		}
		desiredFiles[fileName] = content
	}

	for _, managedID := range sortedKeys(routes.Containers) {
		fileName := "container-" + sanitizeFileToken(managedID) + ".yaml"
		content := workloadConfig("containers", managedID, routes.Containers[managedID], w.previewBaseDomain, w.previewCallbackPath)
		if len(content) == 0 {
			continue
		}
		desiredFiles[fileName] = content
	}

	for _, projectName := range sortedKeys(routes.ComposeProjects) {
		fileName := "compose-" + sanitizeFileToken(projectName) + ".yaml"
		content := composeConfig(projectName, routes.ComposeProjects[projectName], w.previewBaseDomain, w.previewCallbackPath)
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

func coreConfig(appHost string) string {
	bt := "`"
	serverRule := "PathPrefix(" + bt + "/api" + bt + ") || PathPrefix(" + bt + "/auth" + bt + ") || PathPrefix(" + bt + "/health" + bt + ") || PathPrefix(" + bt + "/metrics" + bt + ") || PathPrefix(" + bt + "/swagger" + bt + ")"
	clientRule := "PathPrefix(" + bt + "/" + bt + ")"
	host := strings.TrimSpace(appHost)
	if host != "" {
		serverRule = "Host(" + bt + host + bt + ") && (" + serverRule + ")"
		clientRule = "Host(" + bt + host + bt + ") && " + clientRule
	}
	return "http:\n" +
		"  routers:\n" +
		"    server:\n" +
		"      entryPoints:\n" +
		"        - web\n" +
		"      rule: \"" + serverRule + "\"\n" +
		"      service: server\n" +
		"      priority: 100\n" +
		"    client:\n" +
		"      entryPoints:\n" +
		"        - web\n" +
		"      rule: \"" + clientRule + "\"\n" +
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

func workloadConfig(workloadType string, workloadID string, ports []WorkloadPort, previewBaseDomain string, previewCallbackPath string) []byte {
	trimmedID := strings.TrimSpace(workloadID)
	if trimmedID == "" {
		return nil
	}
	if strings.TrimSpace(previewBaseDomain) == "" {
		return nil
	}

	normalizedPorts := normalizedWorkloadPorts(ports)
	if len(normalizedPorts) == 0 {
		return nil
	}

	safeID := sanitizeResourceToken(trimmedID)

	var buf bytes.Buffer
	buf.WriteString("http:\n")
	buf.WriteString("  routers:\n")
	for _, port := range normalizedPorts {
		routerName := fmt.Sprintf("preview-%s-%s-%d-router", workloadType, safeID, port.Private)
		callbackRouterName := fmt.Sprintf("preview-%s-%s-%d-callback-router", workloadType, safeID, port.Private)
		serviceName := fmt.Sprintf("preview-%s-%s-%d-service", workloadType, safeID, port.Private)
		targetHeadersName := fmt.Sprintf("preview-%s-%s-%d-target-headers", workloadType, safeID, port.Private)
		host := buildWorkloadPreviewHost(previewBaseDomain, workloadType, trimmedID, port.Private)
		callbackPathRule := ensureLeadingSlash(strings.TrimSpace(previewCallbackPath))

		buf.WriteString("    ")
		buf.WriteString(callbackRouterName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"Host(`")
		buf.WriteString(host)
		buf.WriteString("`) && PathPrefix(`")
		buf.WriteString(callbackPathRule)
		buf.WriteString("`)\"\n")
		buf.WriteString("      service: server\n")
		buf.WriteString("      priority: 210\n")

		buf.WriteString("    ")
		buf.WriteString(routerName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"Host(`")
		buf.WriteString(host)
		buf.WriteString("`) && PathPrefix(`/`)\"\n")
		buf.WriteString("      service: ")
		buf.WriteString(serviceName)
		buf.WriteString("\n")
		buf.WriteString("      middlewares:\n")
		buf.WriteString("        - ")
		buf.WriteString(targetHeadersName)
		buf.WriteString("\n")
		buf.WriteString("        - preview-forward-auth-placeholder\n")
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
		targetHeadersName := fmt.Sprintf("preview-%s-%s-%d-target-headers", workloadType, safeID, port.Private)
		buf.WriteString("    ")
		buf.WriteString(targetHeadersName)
		buf.WriteString(":\n")
		buf.WriteString("      headers:\n")
		buf.WriteString("        customRequestHeaders:\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Type: \"")
		buf.WriteString(workloadType)
		buf.WriteString("\"\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Workload-Id: \"")
		buf.WriteString(trimmedID)
		buf.WriteString("\"\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Private-Port: \"")
		buf.WriteString(strconv.Itoa(port.Private))
		buf.WriteString("\"\n")
	}

	return buf.Bytes()
}

func composeConfig(projectName string, ports []ComposeServicePort, previewBaseDomain string, previewCallbackPath string) []byte {
	trimmedProject := strings.TrimSpace(projectName)
	if trimmedProject == "" {
		return nil
	}
	if strings.TrimSpace(previewBaseDomain) == "" {
		return nil
	}

	normalizedPorts := normalizedComposePorts(ports)
	if len(normalizedPorts) == 0 {
		return nil
	}

	safeProject := sanitizeResourceToken(trimmedProject)

	var buf bytes.Buffer
	buf.WriteString("http:\n")
	buf.WriteString("  routers:\n")
	for _, item := range normalizedPorts {
		safeService := sanitizeResourceToken(item.Service)
		routerName := fmt.Sprintf("preview-compose-%s-%s-%d-router", safeProject, safeService, item.Private)
		callbackRouterName := fmt.Sprintf("preview-compose-%s-%s-%d-callback-router", safeProject, safeService, item.Private)
		serviceName := fmt.Sprintf("preview-compose-%s-%s-%d-service", safeProject, safeService, item.Private)
		targetHeadersName := fmt.Sprintf("preview-compose-%s-%s-%d-target-headers", safeProject, safeService, item.Private)
		host := BuildComposePreviewHost(previewBaseDomain, trimmedProject, item.Service, item.Private)
		callbackPathRule := ensureLeadingSlash(strings.TrimSpace(previewCallbackPath))

		buf.WriteString("    ")
		buf.WriteString(callbackRouterName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"Host(`")
		buf.WriteString(host)
		buf.WriteString("`) && PathPrefix(`")
		buf.WriteString(callbackPathRule)
		buf.WriteString("`)\"\n")
		buf.WriteString("      service: server\n")
		buf.WriteString("      priority: 210\n")

		buf.WriteString("    ")
		buf.WriteString(routerName)
		buf.WriteString(":\n")
		buf.WriteString("      entryPoints:\n")
		buf.WriteString("        - web\n")
		buf.WriteString("      rule: \"Host(`")
		buf.WriteString(host)
		buf.WriteString("`) && PathPrefix(`/`)\"\n")
		buf.WriteString("      service: ")
		buf.WriteString(serviceName)
		buf.WriteString("\n")
		buf.WriteString("      middlewares:\n")
		buf.WriteString("        - ")
		buf.WriteString(targetHeadersName)
		buf.WriteString("\n")
		buf.WriteString("        - preview-forward-auth-placeholder\n")
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
		safeService := sanitizeResourceToken(item.Service)
		targetHeadersName := fmt.Sprintf("preview-compose-%s-%s-%d-target-headers", safeProject, safeService, item.Private)
		buf.WriteString("    ")
		buf.WriteString(targetHeadersName)
		buf.WriteString(":\n")
		buf.WriteString("      headers:\n")
		buf.WriteString("        customRequestHeaders:\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Type: \"compose\"\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Project: \"")
		buf.WriteString(trimmedProject)
		buf.WriteString("\"\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Service: \"")
		buf.WriteString(item.Service)
		buf.WriteString("\"\n")
		buf.WriteString("          X-Open-Sandbox-Proxy-Private-Port: \"")
		buf.WriteString(strconv.Itoa(item.Private))
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

func ensureLeadingSlash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "/"
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return "/" + trimmed
}

func ensureTrailingSlash(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "/"
	}
	if strings.HasSuffix(trimmed, "/") {
		return trimmed
	}
	return trimmed + "/"
}

func BuildSandboxPreviewHost(previewBaseDomain string, sandboxID string, privatePort int) string {
	return buildWorkloadPreviewHost(previewBaseDomain, "sandboxes", sandboxID, privatePort)
}

func BuildContainerPreviewHost(previewBaseDomain string, managedID string, privatePort int) string {
	return buildWorkloadPreviewHost(previewBaseDomain, "containers", managedID, privatePort)
}

func BuildComposePreviewHost(previewBaseDomain string, projectName string, serviceName string, privatePort int) string {
	base := strings.TrimSpace(strings.ToLower(previewBaseDomain))
	if base == "" || privatePort <= 0 {
		return ""
	}
	safeProject := sanitizeResourceToken(projectName)
	safeService := sanitizeResourceToken(serviceName)
	hash := hashHostToken("compose", strings.TrimSpace(projectName), strings.TrimSpace(serviceName), strconv.Itoa(privatePort))
	return fmt.Sprintf("cmp-%s-%s-p%d-%s.%s", safeProject, safeService, privatePort, hash, base)
}

func buildWorkloadPreviewHost(previewBaseDomain string, workloadType string, workloadID string, privatePort int) string {
	base := strings.TrimSpace(strings.ToLower(previewBaseDomain))
	if base == "" || privatePort <= 0 {
		return ""
	}
	prefix := "wrk"
	switch strings.TrimSpace(workloadType) {
	case "sandboxes":
		prefix = "sbx"
	case "containers":
		prefix = "ctr"
	}
	safeID := sanitizeResourceToken(workloadID)
	hash := hashHostToken(strings.TrimSpace(workloadType), strings.TrimSpace(workloadID), strconv.Itoa(privatePort))
	return fmt.Sprintf("%s-%s-p%d-%s.%s", prefix, safeID, privatePort, hash, base)
}

func hashHostToken(values ...string) string {
	joined := strings.Join(values, "\n")
	sum := sha1.Sum([]byte(joined))
	encoded := hex.EncodeToString(sum[:])
	if len(encoded) <= 10 {
		return encoded
	}
	return encoded[:10]
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
	if writeErr := os.WriteFile(path, content, 0o644); writeErr != nil {
		return fmt.Errorf("write traefik config %q: %w", path, writeErr)
	}
	return nil
}
