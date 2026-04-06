package api

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
)

const (
	defaultPublicBaseURL     = "http://app.lvh.me:3000"
	defaultPreviewBaseDomain = "preview.lvh.me"
	defaultPreviewLaunchPath = "/auth/preview/launch"
	defaultPreviewCallback   = "/_sandbox/auth/callback"
	defaultPreviewSessionTTL = 10 * time.Minute
	previewTargetTypeSandbox = "sandboxes"
	previewTargetTypeDirect  = "containers"
	previewTargetTypeCompose = "compose"
)

type previewRoutingConfig struct {
	PublicBaseURL      string
	PublicBaseURLHost  string
	PublicBaseURLPort  string
	PublicBaseScheme   string
	AppHost            string
	PreviewBaseDomain  string
	LaunchPathPrefix   string
	CallbackPath       string
	CallbackPathPrefix string
	SessionTTL         time.Duration
}

func loadPreviewRoutingConfig() previewRoutingConfig {
	publicBase := strings.TrimSpace(os.Getenv("SANDBOX_PUBLIC_BASE_URL"))
	if publicBase == "" {
		publicBase = defaultPublicBaseURL
	}
	publicBase = normalizeAbsoluteURL(publicBase)
	parsedPublicBase, err := url.Parse(publicBase)
	if err != nil || strings.TrimSpace(parsedPublicBase.Host) == "" {
		publicBase = defaultPublicBaseURL
		parsedPublicBase, _ = url.Parse(publicBase)
	}

	launchPathPrefix := ensureLeadingSlash(strings.TrimSpace(os.Getenv("SANDBOX_PREVIEW_LAUNCH_PATH_PREFIX")))
	if launchPathPrefix == "/" {
		launchPathPrefix = defaultPreviewLaunchPath
	}
	launchPathPrefix = strings.TrimSuffix(launchPathPrefix, "/")

	callbackPath := ensureLeadingSlash(strings.TrimSpace(os.Getenv("SANDBOX_PREVIEW_CALLBACK_PATH")))
	if callbackPath == "/" {
		callbackPath = defaultPreviewCallback
	}
	callbackPath = strings.TrimSuffix(callbackPath, "/")
	callbackPathPrefix := ensureTrailingSlash(callbackPath)

	sessionTTL := defaultPreviewSessionTTL
	if rawTTL := strings.TrimSpace(os.Getenv("SANDBOX_PREVIEW_SESSION_TTL")); rawTTL != "" {
		if parsedTTL, err := time.ParseDuration(rawTTL); err == nil && parsedTTL > 0 {
			sessionTTL = parsedTTL
		}
	}

	appHost := ""
	publicBasePort := ""
	publicBaseScheme := "http"
	if parsedPublicBase != nil {
		appHost = strings.TrimSpace(parsedPublicBase.Hostname())
		publicBasePort = strings.TrimSpace(parsedPublicBase.Port())
		if scheme := strings.TrimSpace(parsedPublicBase.Scheme); scheme != "" {
			publicBaseScheme = scheme
		}
	}

	previewBaseDomain := strings.TrimSpace(strings.ToLower(os.Getenv("SANDBOX_PREVIEW_BASE_DOMAIN")))
	if previewBaseDomain == "" {
		previewBaseDomain = derivePreviewBaseDomain(appHost)
	}
	if previewBaseDomain == "" {
		previewBaseDomain = defaultPreviewBaseDomain
	}

	return previewRoutingConfig{
		PublicBaseURL:      strings.TrimRight(publicBase, "/"),
		PublicBaseURLHost:  appHost,
		PublicBaseURLPort:  publicBasePort,
		PublicBaseScheme:   publicBaseScheme,
		AppHost:            appHost,
		PreviewBaseDomain:  previewBaseDomain,
		LaunchPathPrefix:   launchPathPrefix,
		CallbackPath:       callbackPath,
		CallbackPathPrefix: callbackPathPrefix,
		SessionTTL:         sessionTTL,
	}
}

func normalizeAbsoluteURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultPublicBaseURL
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/")
	}
	return "http://" + strings.TrimRight(trimmed, "/")
}

func (s *Server) previewLaunchURLForSandbox(sandboxID string, privatePort int) string {
	return s.previewLaunchURL(previewTargetTypeSandbox, []string{previewTargetTypeSandbox, url.PathEscape(strings.TrimSpace(sandboxID)), strconv.Itoa(privatePort)})
}

func (s *Server) previewLaunchURLForManagedContainer(managedID string, privatePort int) string {
	return s.previewLaunchURL(previewTargetTypeDirect, []string{previewTargetTypeDirect, url.PathEscape(strings.TrimSpace(managedID)), strconv.Itoa(privatePort)})
}

func (s *Server) previewLaunchURLForComposeService(projectName string, serviceName string, privatePort int) string {
	return s.previewLaunchURL(previewTargetTypeCompose, []string{previewTargetTypeCompose, url.PathEscape(strings.TrimSpace(projectName)), url.PathEscape(strings.TrimSpace(serviceName)), strconv.Itoa(privatePort)})
}

func (s *Server) previewLaunchURL(_ string, parts []string) string {
	prefix := strings.TrimSuffix(ensureLeadingSlash(s.previewRouting.LaunchPathPrefix), "/")
	joined := strings.Join(parts, "/")
	if joined == "" {
		return prefix
	}
	return prefix + "/" + strings.TrimPrefix(joined, "/")
}

func (s *Server) previewHostForSandbox(sandboxID string, privatePort int) string {
	return traefikcfg.BuildSandboxPreviewHost(s.previewRouting.PreviewBaseDomain, strings.TrimSpace(sandboxID), privatePort)
}

func (s *Server) previewHostForManagedContainer(managedID string, privatePort int) string {
	return traefikcfg.BuildContainerPreviewHost(s.previewRouting.PreviewBaseDomain, strings.TrimSpace(managedID), privatePort)
}

func (s *Server) previewHostForComposeService(projectName string, serviceName string, privatePort int) string {
	return traefikcfg.BuildComposePreviewHost(s.previewRouting.PreviewBaseDomain, strings.TrimSpace(projectName), strings.TrimSpace(serviceName), privatePort)
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

func derivePreviewBaseDomain(publicHost string) string {
	host := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(publicHost)), ".")
	if host == "" {
		return ""
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	if host == "localhost" {
		return ""
	}

	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return ""
	}
	if parts[0] == "preview" {
		return host
	}
	if len(parts) == 2 {
		return "preview." + host
	}

	return "preview." + strings.Join(parts[1:], ".")
}
