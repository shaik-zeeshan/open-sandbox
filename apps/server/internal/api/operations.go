package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"
)

type runtimeLimits struct {
	MemoryBytes int64
	NanoCPUs    int64
	PidsLimit   int64
}

func loadRuntimeLimitsFromEnv() runtimeLimits {
	return runtimeLimits{
		MemoryBytes: parseMemoryLimitBytes(os.Getenv("SANDBOX_RUNTIME_MEMORY_LIMIT")),
		NanoCPUs:    parseCPULimitNano(os.Getenv("SANDBOX_RUNTIME_CPU_LIMIT")),
		PidsLimit:   parsePositiveInt64(os.Getenv("SANDBOX_RUNTIME_PIDS_LIMIT")),
	}
}

func parseMemoryLimitBytes(raw string) int64 {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return 0
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(trimmed, "ki"):
		multiplier = 1024
		trimmed = strings.TrimSuffix(trimmed, "ki")
	case strings.HasSuffix(trimmed, "mi"):
		multiplier = 1024 * 1024
		trimmed = strings.TrimSuffix(trimmed, "mi")
	case strings.HasSuffix(trimmed, "gi"):
		multiplier = 1024 * 1024 * 1024
		trimmed = strings.TrimSuffix(trimmed, "gi")
	case strings.HasSuffix(trimmed, "k"):
		multiplier = 1000
		trimmed = strings.TrimSuffix(trimmed, "k")
	case strings.HasSuffix(trimmed, "m"):
		multiplier = 1000 * 1000
		trimmed = strings.TrimSuffix(trimmed, "m")
	case strings.HasSuffix(trimmed, "g"):
		multiplier = 1000 * 1000 * 1000
		trimmed = strings.TrimSuffix(trimmed, "g")
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(trimmed), 64)
	if err != nil || value <= 0 {
		return 0
	}
	return int64(value * float64(multiplier))
}

func parseCPULimitNano(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return int64(value * 1_000_000_000)
}

func parsePositiveInt64(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func (l runtimeLimits) apply(hostConfig *container.HostConfig) {
	if hostConfig == nil {
		return
	}
	if l.MemoryBytes > 0 {
		hostConfig.Resources.Memory = l.MemoryBytes
	}
	if l.NanoCPUs > 0 {
		hostConfig.Resources.NanoCPUs = l.NanoCPUs
	}
	if l.PidsLimit > 0 {
		value := l.PidsLimit
		hostConfig.Resources.PidsLimit = &value
	}
}

type operationalMetrics struct {
	mu       sync.Mutex
	counters map[string]uint64
}

func newOperationalMetrics() *operationalMetrics {
	return &operationalMetrics{counters: map[string]uint64{}}
}

func (m *operationalMetrics) add(name string, labels map[string]string, delta uint64) {
	if m == nil {
		return
	}
	key := metricKey(name, labels)
	m.mu.Lock()
	m.counters[key] += delta
	m.mu.Unlock()
}

func (m *operationalMetrics) recordLifecycle(operation string, result string) {
	m.add("open_sandbox_sandbox_lifecycle_total", map[string]string{"operation": operation, "result": result}, 1)
}

func (m *operationalMetrics) recordCleanupRun(result string) {
	m.add("open_sandbox_cleanup_runs_total", map[string]string{"result": result}, 1)
}

func (m *operationalMetrics) recordCleanupRemoved(kind string, count uint64) {
	if count == 0 {
		return
	}
	m.add("open_sandbox_cleanup_removed_total", map[string]string{"artifact": kind}, count)
}

func (m *operationalMetrics) recordCleanupError(kind string) {
	m.add("open_sandbox_cleanup_errors_total", map[string]string{"artifact": kind}, 1)
}

func (m *operationalMetrics) renderPrometheus() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.counters))
	for key := range m.counters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(m.counters[key], 10))
		b.WriteByte('\n')
	}
	return b.String()
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf(`%s=%q`, key, labels[key]))
	}
	return fmt.Sprintf("%s{%s}", name, strings.Join(parts, ","))
}

func (s *Server) metricsHandler(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.String(http.StatusOK, s.metrics.renderPrometheus())
}

type cleanupRequest struct {
	DryRun            bool   `json:"dry_run"`
	MaxArtifactAge    string `json:"max_artifact_age"`
	MaxMissingSandbox string `json:"max_missing_sandbox_age"`
}

type cleanupResponse struct {
	DryRun            bool           `json:"dry_run"`
	ArtifactMaxAge    string         `json:"artifact_max_age"`
	MissingSandboxAge string         `json:"missing_sandbox_age"`
	Removed           map[string]int `json:"removed"`
	Errors            []string       `json:"errors,omitempty"`
	CheckedAt         int64          `json:"checked_at"`
}

type reconcileRequest struct {
	DryRun bool `json:"dry_run"`
}

type reconcileResponse struct {
	DryRun    bool           `json:"dry_run"`
	Removed   map[string]int `json:"removed"`
	Errors    []string       `json:"errors,omitempty"`
	CheckedAt int64          `json:"checked_at"`
}

func (s *Server) runMaintenanceCleanup(c *gin.Context) {
	identity, ok := requireAdmin(c)
	if !ok {
		return
	}

	var req cleanupRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, err)
			return
		}
	}

	artifactMaxAge := loadCleanupDuration(req.MaxArtifactAge, 7*24*time.Hour, "SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE")
	missingSandboxAge := loadCleanupDuration(req.MaxMissingSandbox, 24*time.Hour, "SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE")
	result := cleanupResponse{
		DryRun:            req.DryRun,
		ArtifactMaxAge:    artifactMaxAge.String(),
		MissingSandboxAge: missingSandboxAge.String(),
		Removed:           map[string]int{},
		CheckedAt:         time.Now().Unix(),
	}

	runtimeContainers, err := s.runtimeContainersByID(c.Request.Context())
	if err != nil {
		s.metrics.recordCleanupRun("error")
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	cutoffArtifacts := time.Now().Add(-artifactMaxAge)
	cutoffMissingSandboxes := time.Now().Add(-missingSandboxAge).Unix()

	if count, errors := s.cleanupDirectContainerSpecs(runtimeContainers, cutoffArtifacts, req.DryRun); count > 0 || len(errors) > 0 {
		result.Removed["direct_container_specs"] = count
		result.Errors = append(result.Errors, errors...)
	}
	if count, errors := s.cleanupComposeProjects(runtimeContainers, cutoffArtifacts, req.DryRun); count > 0 || len(errors) > 0 {
		result.Removed["compose_projects"] = count
		result.Errors = append(result.Errors, errors...)
	}
	if count, errors := s.cleanupMissingSandboxRecords(c.Request.Context(), runtimeContainers, cutoffMissingSandboxes, req.DryRun); count > 0 || len(errors) > 0 {
		result.Removed["missing_sandbox_records"] = count
		result.Errors = append(result.Errors, errors...)
	}

	if len(result.Errors) > 0 {
		s.metrics.recordCleanupRun("partial")
	} else {
		s.metrics.recordCleanupRun("success")
	}

	s.logger.Info(
		"maintenance_cleanup_completed",
		slog.String("user_id", identity.UserID),
		slog.String("username", identity.Username),
		slog.Bool("dry_run", req.DryRun),
		slog.Any("removed", result.Removed),
		slog.Int("error_count", len(result.Errors)),
	)

	c.JSON(http.StatusOK, result)
}

func (s *Server) runMaintenanceReconcile(c *gin.Context) {
	identity, ok := requireAdmin(c)
	if !ok {
		return
	}

	var req reconcileRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, err)
			return
		}
	}

	result := reconcileResponse{
		DryRun:    req.DryRun,
		Removed:   map[string]int{},
		CheckedAt: time.Now().Unix(),
	}

	runtimeContainers, err := s.runtimeContainersByID(c.Request.Context())
	if err != nil {
		s.metrics.recordCleanupRun("error")
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	cutoff := time.Now().Add(time.Second)
	if count, errors := s.cleanupDirectContainerSpecs(runtimeContainers, cutoff, req.DryRun); count > 0 || len(errors) > 0 {
		result.Removed["direct_container_specs"] = count
		result.Errors = append(result.Errors, errors...)
	}
	if count, errors := s.cleanupComposeProjects(runtimeContainers, cutoff, req.DryRun); count > 0 || len(errors) > 0 {
		result.Removed["compose_projects"] = count
		result.Errors = append(result.Errors, errors...)
	}
	if count, errors := s.cleanupMissingSandboxRecords(c.Request.Context(), runtimeContainers, cutoff.Unix(), req.DryRun); count > 0 || len(errors) > 0 {
		result.Removed["missing_sandbox_records"] = count
		result.Errors = append(result.Errors, errors...)
	}
	if !req.DryRun {
		s.syncTraefikRoutes(c.Request.Context())
	}

	if len(result.Errors) > 0 {
		s.metrics.recordCleanupRun("partial")
	} else {
		s.metrics.recordCleanupRun("success")
	}

	s.logger.Info(
		"maintenance_reconcile_completed",
		slog.String("user_id", identity.UserID),
		slog.String("username", identity.Username),
		slog.Bool("dry_run", req.DryRun),
		slog.Any("removed", result.Removed),
		slog.Int("error_count", len(result.Errors)),
	)

	c.JSON(http.StatusOK, result)
}

func loadCleanupDuration(raw string, fallback time.Duration, envKey string) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = strings.TrimSpace(os.Getenv(envKey))
	}
	if trimmed == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func (s *Server) cleanupDirectContainerSpecs(runtimeContainers map[string]ContainerSummary, cutoff time.Time, dryRun bool) (int, []string) {
	root := s.directContainerSpecRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		s.metrics.recordCleanupError("direct_container_spec")
		return 0, []string{fmt.Sprintf("list direct container specs: %v", err)}
	}

	activeManaged := map[string]struct{}{}
	for _, item := range runtimeContainers {
		if managedID := strings.TrimSpace(item.Labels[labelOpenSandboxManagedID]); managedID != "" {
			activeManaged[managedID] = struct{}{}
		}
	}

	removed := 0
	var failures []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		managedID := strings.TrimSuffix(entry.Name(), ".json")
		if _, ok := activeManaged[managedID]; ok {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			s.metrics.recordCleanupError("direct_container_spec")
			failures = append(failures, fmt.Sprintf("stat direct container spec %s: %v", entry.Name(), statErr))
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if !dryRun {
			if err := os.Remove(path); err != nil {
				s.metrics.recordCleanupError("direct_container_spec")
				failures = append(failures, fmt.Sprintf("remove direct container spec %s: %v", entry.Name(), err))
				continue
			}
		}
		removed++
	}
	s.metrics.recordCleanupRemoved("direct_container_spec", uint64(removed))
	return removed, failures
}

func (s *Server) cleanupComposeProjects(runtimeContainers map[string]ContainerSummary, cutoff time.Time, dryRun bool) (int, []string) {
	root := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		s.metrics.recordCleanupError("compose_project")
		return 0, []string{fmt.Sprintf("list compose projects: %v", err)}
	}

	activeProjects := map[string]struct{}{}
	for _, item := range runtimeContainers {
		if project := s.runtime.ProjectName(item); project != "" {
			activeProjects[project] = struct{}{}
		}
	}

	removed := 0
	var failures []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := activeProjects[entry.Name()]; ok {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			s.metrics.recordCleanupError("compose_project")
			failures = append(failures, fmt.Sprintf("stat compose project %s: %v", entry.Name(), statErr))
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if !dryRun {
			if err := os.RemoveAll(path); err != nil {
				s.metrics.recordCleanupError("compose_project")
				failures = append(failures, fmt.Sprintf("remove compose project %s: %v", entry.Name(), err))
				continue
			}
		}
		removed++
	}
	s.metrics.recordCleanupRemoved("compose_project", uint64(removed))
	return removed, failures
}

func (s *Server) cleanupMissingSandboxRecords(ctx context.Context, runtimeContainers map[string]ContainerSummary, cutoffUnix int64, dryRun bool) (int, []string) {
	if s.sandboxStore == nil {
		return 0, nil
	}
	sandboxes, err := s.sandboxStore.ListSandboxes(ctx)
	if err != nil {
		s.metrics.recordCleanupError("missing_sandbox_record")
		return 0, []string{fmt.Sprintf("list sandboxes: %v", err)}
	}
	removed := 0
	var failures []string
	for _, sandbox := range sandboxes {
		if _, ok := runtimeContainers[sandbox.ContainerID]; ok {
			continue
		}
		if sandbox.UpdatedAt > cutoffUnix {
			continue
		}
		if !dryRun {
			if err := s.sandboxStore.DeleteSandbox(ctx, sandbox.ID); err != nil {
				s.metrics.recordCleanupError("missing_sandbox_record")
				failures = append(failures, fmt.Sprintf("delete missing sandbox record %s: %v", sandbox.ID, err))
				continue
			}
		}
		removed++
	}
	s.metrics.recordCleanupRemoved("missing_sandbox_record", uint64(removed))
	return removed, failures
}

func (s *Server) logLifecycleFailure(operation string, err error, attrs ...slog.Attr) {
	fields := []any{slog.String("operation", operation), slog.String("result", "error"), slog.String("error", err.Error())}
	for _, attr := range attrs {
		fields = append(fields, attr)
	}
	s.logger.Error("sandbox_lifecycle_failed", fields...)
	s.metrics.recordLifecycle(operation, "error")
}

func (s *Server) logLifecycleSuccess(operation string, attrs ...slog.Attr) {
	fields := []any{slog.String("operation", operation), slog.String("result", "success")}
	for _, attr := range attrs {
		fields = append(fields, attr)
	}
	s.logger.Info("sandbox_lifecycle_completed", fields...)
	s.metrics.recordLifecycle(operation, "success")
}

func (s *Server) logStreamFailure(kind string, err error, attrs ...slog.Attr) {
	fields := []any{slog.String("stream", kind), slog.String("error", err.Error())}
	for _, attr := range attrs {
		fields = append(fields, attr)
	}
	s.logger.Error("sandbox_stream_failed", fields...)
}

func encodeJSON(v any) string {
	payload, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(payload)
}
