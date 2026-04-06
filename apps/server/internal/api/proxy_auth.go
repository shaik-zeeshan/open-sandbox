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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

var errProxyAccessDenied = errors.New("proxy access denied")

const (
	proxyTypeHeaderName          = "X-Open-Sandbox-Proxy-Type"
	proxyWorkloadIDHeaderName    = "X-Open-Sandbox-Proxy-Workload-Id"
	proxyProjectHeaderName       = "X-Open-Sandbox-Proxy-Project"
	proxyServiceHeaderName       = "X-Open-Sandbox-Proxy-Service"
	proxyPrivatePortHeaderName   = "X-Open-Sandbox-Proxy-Private-Port"
	previewSessionCookieName     = "open_sandbox_preview"
	previewTokenAudience         = "open-sandbox-preview"
	previewTokenTypeGrant        = "preview_grant"
	previewTokenTypeSession      = "preview_session"
	defaultPreviewRedirectTarget = "/"
)

type proxyAuthorizationTarget struct {
	WorkloadType string
	WorkloadID   string
	ProjectName  string
	ServiceName  string
	PrivatePort  int
}

type previewAuthClaims struct {
	TokenType string `json:"token_type"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Host      string `json:"host"`

	WorkloadType string `json:"workload_type"`
	WorkloadID   string `json:"workload_id,omitempty"`
	ProjectName  string `json:"project_name,omitempty"`
	ServiceName  string `json:"service_name,omitempty"`
	PrivatePort  int    `json:"private_port"`

	jwt.RegisteredClaims
}

func (s *Server) proxyAuthorize(c *gin.Context) {
	requestID, _ := c.Get("request_id")
	requestIDValue := fmt.Sprint(requestID)

	target, err := parseProxyAuthorizationTargetFromHeaders(c.Request.Header)
	if err != nil {
		s.logProxyAuthorizationDecision(slog.LevelInfo, requestIDValue, AuthIdentity{}, proxyAuthorizationTarget{}, "deny", "invalid_proxy_target")
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	identity, reason, err := s.authenticateProxyIdentity(c.Request, target)
	if err != nil {
		s.logProxyAuthorizationDecision(slog.LevelInfo, requestIDValue, AuthIdentity{}, target, "deny", reason)
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized", Reason: reason})
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

func (s *Server) previewLaunch(c *gin.Context) {
	identity, _, reason, err := s.auth.authenticateRequest(c.Request)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized", Reason: reason})
		return
	}

	target, err := parsePreviewLaunchTarget(c.Param("target"))
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if err := s.authorizeProxyTarget(c.Request.Context(), identity, target); err != nil {
		if errors.Is(err, errProxyAccessDenied) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	previewHost := s.previewHostForTarget(target)
	if previewHost == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	grantToken, err := s.issuePreviewToken(previewTokenTypeGrant, identity, target, previewHost)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	callbackURL := url.URL{
		Scheme: s.previewRouting.PublicBaseScheme,
		Host:   previewHost,
		Path:   s.previewRouting.CallbackPath,
	}
	query := callbackURL.Query()
	query.Set("grant", grantToken)
	query.Set("next", defaultPreviewRedirectTarget)
	callbackURL.RawQuery = query.Encode()

	c.Redirect(http.StatusFound, callbackURL.String())
}

func (s *Server) previewAuthCallback(c *gin.Context) {
	rawGrant := strings.TrimSpace(c.Query("grant"))
	if rawGrant == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	targetHost := forwardedRequestHost(c.Request)
	claims, err := s.validatePreviewToken(rawGrant, previewTokenTypeGrant, targetHost)
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	identity := AuthIdentity{UserID: strings.TrimSpace(claims.UserID), Username: strings.TrimSpace(claims.Username), Role: strings.TrimSpace(claims.Role)}
	target := claims.proxyTarget()

	if err := s.authorizeProxyTarget(c.Request.Context(), identity, target); err != nil {
		if errors.Is(err, errProxyAccessDenied) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	sessionToken, err := s.issuePreviewToken(previewTokenTypeSession, identity, target, targetHost)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     previewSessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(c.Request),
		Expires:  time.Now().Add(s.previewRouting.SessionTTL),
		MaxAge:   max(1, int(s.previewRouting.SessionTTL.Seconds())),
	})

	next := normalizePreviewNextPath(c.Query("next"))
	c.Redirect(http.StatusFound, next)
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

func parseProxyAuthorizationTargetFromHeaders(headers http.Header) (proxyAuthorizationTarget, error) {
	if headers == nil {
		return proxyAuthorizationTarget{}, errors.New("proxy headers are required")
	}

	workloadType := strings.TrimSpace(headers.Get(proxyTypeHeaderName))
	if workloadType == "" {
		return proxyAuthorizationTarget{}, errors.New("proxy workload type is required")
	}

	privatePort, err := parseProxyPrivatePort(headers.Get(proxyPrivatePortHeaderName))
	if err != nil {
		return proxyAuthorizationTarget{}, err
	}

	target := proxyAuthorizationTarget{WorkloadType: workloadType, PrivatePort: privatePort}
	switch workloadType {
	case previewTargetTypeSandbox, previewTargetTypeDirect:
		target.WorkloadID = strings.TrimSpace(headers.Get(proxyWorkloadIDHeaderName))
		if target.WorkloadID == "" {
			return proxyAuthorizationTarget{}, errors.New("workload id is required")
		}
		return target, nil
	case previewTargetTypeCompose:
		target.ProjectName = strings.TrimSpace(headers.Get(proxyProjectHeaderName))
		target.ServiceName = strings.TrimSpace(headers.Get(proxyServiceHeaderName))
		if target.ProjectName == "" || target.ServiceName == "" {
			return proxyAuthorizationTarget{}, errors.New("compose target headers are required")
		}
		return target, nil
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

func parsePreviewLaunchTarget(rawTarget string) (proxyAuthorizationTarget, error) {
	trimmed := strings.Trim(strings.TrimSpace(rawTarget), "/")
	if trimmed == "" {
		return proxyAuthorizationTarget{}, errors.New("preview launch target is required")
	}

	cleanPath := path.Clean("/" + trimmed)
	if cleanPath == "/" {
		return proxyAuthorizationTarget{}, errors.New("preview launch target is required")
	}
	segments := strings.Split(strings.Trim(cleanPath, "/"), "/")

	switch segments[0] {
	case previewTargetTypeSandbox, previewTargetTypeDirect:
		if len(segments) != 3 {
			return proxyAuthorizationTarget{}, errors.New("invalid preview launch target")
		}
		workloadID, err := url.PathUnescape(strings.TrimSpace(segments[1]))
		if err != nil || strings.TrimSpace(workloadID) == "" {
			return proxyAuthorizationTarget{}, errors.New("invalid preview workload id")
		}
		privatePort, err := parseProxyPrivatePort(segments[2])
		if err != nil {
			return proxyAuthorizationTarget{}, err
		}
		return proxyAuthorizationTarget{WorkloadType: segments[0], WorkloadID: workloadID, PrivatePort: privatePort}, nil
	case previewTargetTypeCompose:
		if len(segments) != 4 {
			return proxyAuthorizationTarget{}, errors.New("invalid compose preview launch target")
		}
		projectName, err := url.PathUnescape(strings.TrimSpace(segments[1]))
		if err != nil || strings.TrimSpace(projectName) == "" {
			return proxyAuthorizationTarget{}, errors.New("invalid compose project")
		}
		serviceName, err := url.PathUnescape(strings.TrimSpace(segments[2]))
		if err != nil || strings.TrimSpace(serviceName) == "" {
			return proxyAuthorizationTarget{}, errors.New("invalid compose service")
		}
		privatePort, err := parseProxyPrivatePort(segments[3])
		if err != nil {
			return proxyAuthorizationTarget{}, err
		}
		return proxyAuthorizationTarget{WorkloadType: segments[0], ProjectName: projectName, ServiceName: serviceName, PrivatePort: privatePort}, nil
	default:
		return proxyAuthorizationTarget{}, errors.New("unsupported preview target type")
	}
}

func normalizePreviewNextPath(raw string) string {
	next := strings.TrimSpace(raw)
	if next == "" {
		return defaultPreviewRedirectTarget
	}
	if strings.HasPrefix(next, "/") {
		return next
	}
	return defaultPreviewRedirectTarget
}

func (s *Server) previewHostForTarget(target proxyAuthorizationTarget) string {
	switch target.WorkloadType {
	case previewTargetTypeSandbox:
		return s.previewHostForSandbox(target.WorkloadID, target.PrivatePort)
	case previewTargetTypeDirect:
		return s.previewHostForManagedContainer(target.WorkloadID, target.PrivatePort)
	case previewTargetTypeCompose:
		return s.previewHostForComposeService(target.ProjectName, target.ServiceName, target.PrivatePort)
	default:
		return ""
	}
}

func (s *Server) authenticateProxyIdentity(r *http.Request, target proxyAuthorizationTarget) (AuthIdentity, string, error) {
	identity, _, reason, err := s.auth.authenticateRequest(r)
	if err == nil {
		return identity, "ok", nil
	}

	previewToken := ""
	if cookie, cookieErr := r.Cookie(previewSessionCookieName); cookieErr == nil {
		previewToken = strings.TrimSpace(cookie.Value)
	}
	if previewToken == "" {
		return AuthIdentity{}, reason, err
	}

	claims, validateErr := s.validatePreviewToken(previewToken, previewTokenTypeSession, forwardedRequestHost(r))
	if validateErr != nil {
		return AuthIdentity{}, "token_invalid", validateErr
	}
	if !targetsEqual(claims.proxyTarget(), target) {
		return AuthIdentity{}, "token_invalid", errors.New("preview token target mismatch")
	}

	identity = AuthIdentity{UserID: strings.TrimSpace(claims.UserID), Username: strings.TrimSpace(claims.Username), Role: strings.TrimSpace(claims.Role)}
	if identity.UserID == "" {
		return AuthIdentity{}, "token_invalid", errors.New("preview token missing user identity")
	}
	return identity, "ok", nil
}

func targetsEqual(a proxyAuthorizationTarget, b proxyAuthorizationTarget) bool {
	return strings.TrimSpace(a.WorkloadType) == strings.TrimSpace(b.WorkloadType) &&
		strings.TrimSpace(a.WorkloadID) == strings.TrimSpace(b.WorkloadID) &&
		strings.TrimSpace(a.ProjectName) == strings.TrimSpace(b.ProjectName) &&
		strings.TrimSpace(a.ServiceName) == strings.TrimSpace(b.ServiceName) &&
		a.PrivatePort == b.PrivatePort
}

func (s *Server) issuePreviewToken(tokenType string, identity AuthIdentity, target proxyAuthorizationTarget, host string) (string, error) {
	if len(s.auth.JWTSecret) == 0 {
		return "", errors.New("jwt secret is not configured")
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.previewRouting.SessionTTL)
	claims := previewAuthClaims{
		TokenType:    tokenType,
		UserID:       strings.TrimSpace(identity.UserID),
		Username:     strings.TrimSpace(identity.Username),
		Role:         strings.TrimSpace(identity.Role),
		Host:         strings.ToLower(strings.TrimSpace(host)),
		WorkloadType: strings.TrimSpace(target.WorkloadType),
		WorkloadID:   strings.TrimSpace(target.WorkloadID),
		ProjectName:  strings.TrimSpace(target.ProjectName),
		ServiceName:  strings.TrimSpace(target.ServiceName),
		PrivatePort:  target.PrivatePort,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.auth.Issuer,
			Audience:  jwt.ClaimStrings{previewTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.auth.JWTSecret)
}

func (s *Server) validatePreviewToken(raw string, tokenType string, host string) (*previewAuthClaims, error) {
	if len(s.auth.JWTSecret) == 0 {
		return nil, errors.New("jwt secret is not configured")
	}
	claims := &previewAuthClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unsupported token signing method")
		}
		return s.auth.JWTSecret, nil
	}, jwt.WithAudience(previewTokenAudience), jwt.WithIssuer(s.auth.Issuer), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	if err != nil || token == nil || !token.Valid {
		return nil, errors.New("invalid preview token")
	}
	if strings.TrimSpace(claims.TokenType) != tokenType {
		return nil, errors.New("invalid preview token type")
	}
	expectedHost := strings.ToLower(strings.TrimSpace(host))
	tokenHost := strings.ToLower(strings.TrimSpace(claims.Host))
	if expectedHost == "" || tokenHost == "" || expectedHost != tokenHost {
		return nil, errors.New("preview token host mismatch")
	}
	if claims.PrivatePort <= 0 || strings.TrimSpace(claims.WorkloadType) == "" {
		return nil, errors.New("preview token target is invalid")
	}
	return claims, nil
}

func (c *previewAuthClaims) proxyTarget() proxyAuthorizationTarget {
	if c == nil {
		return proxyAuthorizationTarget{}
	}
	return proxyAuthorizationTarget{
		WorkloadType: strings.TrimSpace(c.WorkloadType),
		WorkloadID:   strings.TrimSpace(c.WorkloadID),
		ProjectName:  strings.TrimSpace(c.ProjectName),
		ServiceName:  strings.TrimSpace(c.ServiceName),
		PrivatePort:  c.PrivatePort,
	}
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

	if !containerHasPrivatePort(container, target.PrivatePort, true) {
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
		if containerHasPrivatePort(item, target.PrivatePort, true) {
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
