package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

var errProxyAccessDenied = errors.New("proxy access denied")

type proxyAuthorizationTarget struct {
	WorkloadType string
	WorkloadID   string
	ProjectName  string
	ServiceName  string
	PrivatePort  int
}

func (s *Server) proxyAuthorize(c *gin.Context) {
	requestID, _ := c.Get("request_id")
	requestIDValue := fmt.Sprint(requestID)

	identity, _, reason, err := s.auth.authenticateRequest(c.Request)
	if err != nil {
		s.logProxyAuthorizationDecision(slog.LevelInfo, requestIDValue, AuthIdentity{}, proxyAuthorizationTarget{}, "deny", reason)
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized", Reason: reason})
		return
	}

	target, err := parseProxyAuthorizationTarget(c.Request.Header.Get("X-Forwarded-Uri"))
	if err != nil {
		s.logProxyAuthorizationDecision(slog.LevelInfo, requestIDValue, identity, proxyAuthorizationTarget{}, "deny", "invalid_forwarded_uri")
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if s.proxyAuthLimiter != nil && !s.proxyAuthLimiter.Allow(identity.UserID) {
		s.logProxyAuthorizationDecision(slog.LevelWarn, requestIDValue, identity, target, "deny", "rate_limited")
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatus(http.StatusTooManyRequests)
		return
	}

	if err := s.authorizeProxyTarget(c.Request.Context(), identity, target); err != nil {
		if errors.Is(err, errProxyAccessDenied) {
			s.logProxyAuthorizationDecision(slog.LevelInfo, requestIDValue, identity, target, "deny", "access_denied")
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		s.logProxyAuthorizationDecision(slog.LevelError, requestIDValue, identity, target, "error", "internal_error")
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	s.logProxyAuthorizationDecision(slog.LevelInfo, requestIDValue, identity, target, "allow", "ok")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
}

func (s *Server) logProxyAuthorizationDecision(level slog.Level, requestID string, identity AuthIdentity, target proxyAuthorizationTarget, decision string, reason string) {
	attrs := []slog.Attr{
		slog.String("request_id", strings.TrimSpace(requestID)),
		slog.String("user_id", strings.TrimSpace(identity.UserID)),
		slog.String("username", strings.TrimSpace(identity.Username)),
		slog.String("workload_type", strings.TrimSpace(target.WorkloadType)),
		slog.String("workload_id", strings.TrimSpace(target.WorkloadID)),
		slog.String("project_name", strings.TrimSpace(target.ProjectName)),
		slog.String("service_name", strings.TrimSpace(target.ServiceName)),
		slog.Int("private_port", target.PrivatePort),
		slog.String("decision", strings.TrimSpace(decision)),
		slog.String("reason", strings.TrimSpace(reason)),
	}

	s.logger.LogAttrs(context.Background(), level, "proxy_authorization_decision", attrs...)
}

func parseProxyAuthorizationTarget(rawForwardedURI string) (proxyAuthorizationTarget, error) {
	trimmedURI := strings.TrimSpace(rawForwardedURI)
	if trimmedURI == "" {
		return proxyAuthorizationTarget{}, errors.New("forwarded uri is required")
	}

	parsedURI, err := url.ParseRequestURI(trimmedURI)
	if err != nil {
		return proxyAuthorizationTarget{}, fmt.Errorf("invalid forwarded uri: %w", err)
	}

	cleanPath := path.Clean(strings.TrimSpace(parsedURI.Path))
	if cleanPath == "." || cleanPath == "/" {
		return proxyAuthorizationTarget{}, errors.New("proxy path is required")
	}

	segments := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(segments) < 4 || segments[0] != "proxy" {
		return proxyAuthorizationTarget{}, errors.New("unsupported proxy path")
	}

	switch segments[1] {
	case "sandboxes", "containers":
		if len(segments) < 4 {
			return proxyAuthorizationTarget{}, errors.New("invalid proxy path")
		}
		workloadID, err := url.PathUnescape(strings.TrimSpace(segments[2]))
		if err != nil || strings.TrimSpace(workloadID) == "" {
			return proxyAuthorizationTarget{}, errors.New("invalid workload id")
		}
		privatePort, err := parseProxyPrivatePort(segments[3])
		if err != nil {
			return proxyAuthorizationTarget{}, err
		}
		return proxyAuthorizationTarget{WorkloadType: segments[1], WorkloadID: workloadID, PrivatePort: privatePort}, nil
	case "compose":
		if len(segments) < 5 {
			return proxyAuthorizationTarget{}, errors.New("invalid compose proxy path")
		}
		projectName, err := url.PathUnescape(strings.TrimSpace(segments[2]))
		if err != nil || strings.TrimSpace(projectName) == "" {
			return proxyAuthorizationTarget{}, errors.New("invalid compose project")
		}
		serviceName, err := url.PathUnescape(strings.TrimSpace(segments[3]))
		if err != nil || strings.TrimSpace(serviceName) == "" {
			return proxyAuthorizationTarget{}, errors.New("invalid compose service")
		}
		privatePort, err := parseProxyPrivatePort(segments[4])
		if err != nil {
			return proxyAuthorizationTarget{}, err
		}
		return proxyAuthorizationTarget{WorkloadType: segments[1], ProjectName: projectName, ServiceName: serviceName, PrivatePort: privatePort}, nil
	default:
		return proxyAuthorizationTarget{}, errors.New("unsupported workload type")
	}
}

func parseProxyPrivatePort(raw string) (int, error) {
	privatePort, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || privatePort <= 0 {
		return 0, errors.New("invalid private port")
	}
	return privatePort, nil
}

func (s *Server) authorizeProxyTarget(ctx context.Context, identity AuthIdentity, target proxyAuthorizationTarget) error {
	switch target.WorkloadType {
	case "sandboxes":
		return s.authorizeSandboxProxyTarget(ctx, identity, target)
	case "containers":
		return s.authorizeContainerProxyTarget(ctx, identity, target)
	case "compose":
		return s.authorizeComposeProxyTarget(ctx, identity, target)
	default:
		return errProxyAccessDenied
	}
}

func (s *Server) authorizeSandboxProxyTarget(ctx context.Context, identity AuthIdentity, target proxyAuthorizationTarget) error {
	if s.sandboxStore == nil {
		return errors.New("sandbox store is not configured")
	}

	sandbox, err := s.sandboxStore.GetSandbox(ctx, target.WorkloadID)
	if err != nil {
		if errors.Is(err, store.ErrSandboxNotFound) {
			return errProxyAccessDenied
		}
		return err
	}
	if sandbox.OwnerID != identity.UserID {
		return errProxyAccessDenied
	}

	containersByID, err := s.runtimeContainersByID(ctx)
	if err != nil {
		return err
	}
	container, ok := containersByID[sandbox.ContainerID]
	if !ok {
		return errProxyAccessDenied
	}

	managedComposeProjects, err := s.managedComposeProjects()
	if err != nil {
		return err
	}
	if !s.runtimeContainerManagedByApp(container, managedComposeProjects) {
		return errProxyAccessDenied
	}

	if !containerHasPrivatePort(container, target.PrivatePort, false) {
		return errProxyAccessDenied
	}

	return nil
}

func (s *Server) authorizeContainerProxyTarget(ctx context.Context, identity AuthIdentity, target proxyAuthorizationTarget) error {
	visibleContainers, err := s.visibleManagedContainers(ctx, identity)
	if err != nil {
		return err
	}

	for _, item := range visibleContainers {
		managedID := strings.TrimSpace(item.Labels[labelOpenSandboxManagedID])
		if managedID != target.WorkloadID {
			continue
		}
		if containerHasPrivatePort(item, target.PrivatePort, false) {
			return nil
		}
		return errProxyAccessDenied
	}

	return errProxyAccessDenied
}

func (s *Server) authorizeComposeProxyTarget(ctx context.Context, identity AuthIdentity, target proxyAuthorizationTarget) error {
	visibleContainers, err := s.visibleManagedContainers(ctx, identity)
	if err != nil {
		return err
	}

	for _, item := range visibleContainers {
		projectName := strings.TrimSpace(item.ProjectName)
		if projectName == "" {
			projectName = strings.TrimSpace(item.Labels["com.docker.compose.project"])
		}
		if projectName != target.ProjectName {
			continue
		}

		serviceName := strings.TrimSpace(item.ServiceName)
		if serviceName == "" {
			serviceName = strings.TrimSpace(item.Labels["com.docker.compose.service"])
		}
		if serviceName != target.ServiceName {
			continue
		}

		if containerHasPrivatePort(item, target.PrivatePort, true) {
			return nil
		}
		return errProxyAccessDenied
	}

	return errProxyAccessDenied
}

func containerHasPrivatePort(item ContainerSummary, privatePort int, requirePublished bool) bool {
	for _, port := range item.Ports {
		if port.Private != privatePort {
			continue
		}
		if requirePublished && port.Public <= 0 {
			continue
		}
		return true
	}
	return false
}
