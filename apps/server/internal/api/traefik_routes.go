package api

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
)

type traefikRouteStateResponse struct {
	Enabled          bool                             `json:"enabled"`
	DynamicConfigDir string                           `json:"dynamic_config_dir,omitempty"`
	Sandboxes        []traefikRouteWorkloadResponse   `json:"sandboxes"`
	Containers       []traefikRouteWorkloadResponse   `json:"containers"`
	ComposeProjects  []traefikRouteComposeProjectInfo `json:"compose_projects"`
}

type traefikRouteWorkloadResponse struct {
	ID    string                        `json:"id"`
	File  string                        `json:"file"`
	Ports []traefikRoutePortDescription `json:"ports"`
}

type traefikRouteComposeProjectInfo struct {
	Project  string                             `json:"project"`
	File     string                             `json:"file"`
	Services []traefikRouteComposeServiceResult `json:"services"`
}

type traefikRouteComposeServiceResult struct {
	Name  string                        `json:"name"`
	Ports []traefikRoutePortDescription `json:"ports"`
}

type traefikRoutePortDescription struct {
	Private int `json:"private"`
	Public  int `json:"public"`
}

func (s *Server) syncTraefikRoutes(ctx context.Context) {
	if s.traefikWriter == nil {
		return
	}

	syncCtx, cancel := traefikSyncContext(ctx)
	defer cancel()

	const maxAttempts = 3
	var (
		routes traefikcfg.WorkloadRoutes
		err    error
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		routes, err = s.currentTraefikRoutes(syncCtx)
		if err == nil {
			err = s.traefikWriter.Reconcile(routes)
		}
		if err == nil {
			routeSummary := summarizeTraefikRouteKeys(routes)
			s.logger.Info(
				"traefik_route_sync_complete",
				slog.Int("sandbox_count", len(routes.Sandboxes)),
				slog.Int("container_count", len(routes.Containers)),
				slog.Int("compose_project_count", len(routes.ComposeProjects)),
				slog.Any("sandbox_ids", routeSummary.Sandboxes),
				slog.Any("container_ids", routeSummary.Containers),
				slog.Any("compose_projects", routeSummary.ComposeProjects),
				slog.Int("attempt", attempt),
			)
			return
		}
		if attempt == maxAttempts || syncCtx.Err() != nil {
			break
		}
		if !sleepWithContext(syncCtx, 250*time.Millisecond) {
			break
		}
	}

	s.logger.Warn(
		"traefik_route_sync_failed",
		slog.String("reason", err.Error()),
		slog.Int("attempts", maxAttempts),
	)
	return

}

type traefikRouteKeySummary struct {
	Sandboxes       []string
	Containers      []string
	ComposeProjects []string
}

func summarizeTraefikRouteKeys(routes traefikcfg.WorkloadRoutes) traefikRouteKeySummary {
	return traefikRouteKeySummary{
		Sandboxes:       summarizeMapKeys(routes.Sandboxes, 10),
		Containers:      summarizeMapKeys(routes.Containers, 10),
		ComposeProjects: summarizeMapKeys(routes.ComposeProjects, 10),
	}
}

func summarizeMapKeys[T any](entries map[string]T, limit int) []string {
	if len(entries) == 0 {
		return nil
	}
	keys := sortedRouteKeys(entries)
	if limit <= 0 || len(keys) <= limit {
		return keys
	}
	return keys[:limit]
}

func sortedRouteKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		keys = append(keys, trimmed)
	}
	sort.Strings(keys)
	return keys
}

func traefikSyncContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Server) currentTraefikRoutes(ctx context.Context) (traefikcfg.WorkloadRoutes, error) {
	containers, err := s.runtime.ListWorkloads(ctx)
	if err != nil {
		return traefikcfg.WorkloadRoutes{}, err
	}
	managedComposeProjects, err := s.managedComposeProjects()
	if err != nil {
		return traefikcfg.WorkloadRoutes{}, err
	}
	containers = s.filterManagedRuntimeContainers(containers, managedComposeProjects)

	routes := traefikcfg.WorkloadRoutes{
		Sandboxes:       map[string][]traefikcfg.WorkloadPort{},
		Containers:      map[string][]traefikcfg.WorkloadPort{},
		ComposeProjects: map[string][]traefikcfg.ComposeServicePort{},
	}

	for _, item := range containers {
		publishedPorts := publishedPorts(item.Ports)
		if len(publishedPorts) == 0 {
			continue
		}

		if sandboxID := strings.TrimSpace(item.Labels[labelOpenSandboxSandboxID]); sandboxID != "" {
			routes.Sandboxes[sandboxID] = append(routes.Sandboxes[sandboxID], publishedPorts...)
			continue
		}

		if strings.TrimSpace(item.Labels[labelOpenSandboxKind]) == managedKindDirect {
			if managedID := strings.TrimSpace(item.Labels[labelOpenSandboxManagedID]); managedID != "" {
				routes.Containers[managedID] = append(routes.Containers[managedID], publishedPorts...)
				continue
			}
		}

		projectName := strings.TrimSpace(item.ProjectName)
		if projectName == "" {
			projectName = strings.TrimSpace(item.Labels["com.docker.compose.project"])
		}
		serviceName := strings.TrimSpace(item.ServiceName)
		if serviceName == "" {
			serviceName = strings.TrimSpace(item.Labels["com.docker.compose.service"])
		}
		if projectName == "" || serviceName == "" {
			continue
		}

		for _, port := range publishedPorts {
			routes.ComposeProjects[projectName] = append(routes.ComposeProjects[projectName], traefikcfg.ComposeServicePort{
				Service: serviceName,
				Private: port.Private,
				Public:  port.Public,
			})
		}
	}

	return routes, nil
}

func (s *Server) getTraefikRouteState(c *gin.Context) {
	if _, ok := requireAdmin(c); !ok {
		return
	}

	routes, err := s.currentTraefikRoutes(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	response := traefikRouteStateResponse{
		Enabled:         s.traefikWriter != nil,
		Sandboxes:       buildTraefikWorkloadResponse("sandbox", routes.Sandboxes),
		Containers:      buildTraefikWorkloadResponse("container", routes.Containers),
		ComposeProjects: buildTraefikComposeResponse(routes.ComposeProjects),
	}
	if s.traefikWriter != nil {
		response.DynamicConfigDir = s.traefikWriter.Dir()
	}

	c.JSON(http.StatusOK, response)
}

func publishedPorts(ports []PortSummary) []traefikcfg.WorkloadPort {
	out := make([]traefikcfg.WorkloadPort, 0, len(ports))
	for _, port := range ports {
		if port.Private <= 0 || port.Public <= 0 {
			continue
		}
		out = append(out, traefikcfg.WorkloadPort{Private: port.Private, Public: port.Public})
	}
	return out
}

func buildTraefikWorkloadResponse(prefix string, entries map[string][]traefikcfg.WorkloadPort) []traefikRouteWorkloadResponse {
	out := make([]traefikRouteWorkloadResponse, 0, len(entries))
	for id, ports := range entries {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" {
			continue
		}
		portDescriptions := make([]traefikRoutePortDescription, 0, len(ports))
		for _, port := range ports {
			if port.Private <= 0 || port.Public <= 0 {
				continue
			}
			portDescriptions = append(portDescriptions, traefikRoutePortDescription{Private: port.Private, Public: port.Public})
		}
		sort.Slice(portDescriptions, func(i, j int) bool {
			if portDescriptions[i].Private != portDescriptions[j].Private {
				return portDescriptions[i].Private < portDescriptions[j].Private
			}
			return portDescriptions[i].Public < portDescriptions[j].Public
		})
		out = append(out, traefikRouteWorkloadResponse{
			ID:    trimmedID,
			File:  prefix + "-" + sanitizeTraefikFileToken(trimmedID) + ".yaml",
			Ports: portDescriptions,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func buildTraefikComposeResponse(entries map[string][]traefikcfg.ComposeServicePort) []traefikRouteComposeProjectInfo {
	out := make([]traefikRouteComposeProjectInfo, 0, len(entries))
	for project, ports := range entries {
		trimmedProject := strings.TrimSpace(project)
		if trimmedProject == "" {
			continue
		}
		servicesByName := map[string][]traefikRoutePortDescription{}
		for _, item := range ports {
			serviceName := strings.TrimSpace(item.Service)
			if serviceName == "" || item.Private <= 0 || item.Public <= 0 {
				continue
			}
			servicesByName[serviceName] = append(servicesByName[serviceName], traefikRoutePortDescription{Private: item.Private, Public: item.Public})
		}
		services := make([]traefikRouteComposeServiceResult, 0, len(servicesByName))
		for name, servicePorts := range servicesByName {
			sort.Slice(servicePorts, func(i, j int) bool {
				if servicePorts[i].Private != servicePorts[j].Private {
					return servicePorts[i].Private < servicePorts[j].Private
				}
				return servicePorts[i].Public < servicePorts[j].Public
			})
			services = append(services, traefikRouteComposeServiceResult{Name: name, Ports: servicePorts})
		}
		sort.Slice(services, func(i, j int) bool {
			return services[i].Name < services[j].Name
		})
		out = append(out, traefikRouteComposeProjectInfo{
			Project:  trimmedProject,
			File:     "compose-" + sanitizeTraefikFileToken(trimmedProject) + ".yaml",
			Services: services,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Project < out[j].Project
	})
	return out
}

func sanitizeTraefikFileToken(raw string) string {
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
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "workload"
	}
	return out
}
