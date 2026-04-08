package api

import (
	"strings"

	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
	"gopkg.in/yaml.v3"
)

// composeServiceExtension mirrors the x-open-sandbox extension field allowed
// under each service in a Docker Compose YAML file.
//
// Example compose service snippet:
//
//	services:
//	  api:
//	    image: myapp:latest
//	    x-open-sandbox:
//	      proxy:
//	        request_headers:
//	          X-Real-Tenant: "acme"
//	        response_headers:
//	          X-Frame-Options: "DENY"
//	        cors:
//	          allow_origins: ["https://app.example.com"]
//	          allow_methods: ["GET","POST","OPTIONS"]
//	          allow_headers: ["Authorization","Content-Type"]
//	          allow_credentials: true
//	          max_age: 3600
//	        path_prefix_strip: "/api"
//	        skip_auth: true
type composeServiceExtension struct {
	Proxy *composeServiceProxyExtension `yaml:"proxy"`
}

type composeServiceProxyExtension struct {
	RequestHeaders  map[string]string            `yaml:"request_headers"`
	ResponseHeaders map[string]string            `yaml:"response_headers"`
	CORS            *composeServiceCORSExtension `yaml:"cors"`
	PathPrefixStrip string                       `yaml:"path_prefix_strip"`
	SkipAuth        bool                         `yaml:"skip_auth"`
}

type composeServiceCORSExtension struct {
	AllowOrigins     []string `yaml:"allow_origins"`
	AllowMethods     []string `yaml:"allow_methods"`
	AllowHeaders     []string `yaml:"allow_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// composeFile is a minimal representation of a Docker Compose YAML file used
// only to extract x-open-sandbox extension fields per service.
type composeFile struct {
	Services map[string]composeServiceNode `yaml:"services"`
}

// composeServiceNode is a generic service node used for extracting only the
// extension field. Unknown compose keys are stored in Extra.
type composeServiceNode struct {
	XOpenSandbox *composeServiceExtension `yaml:"x-open-sandbox"`
}

// parseComposeServiceProxyConfigs parses the YAML compose content and returns
// a map of service name → ServiceProxyConfig. Missing or invalid extension
// fields are silently ignored (safe defaults apply).
func parseComposeServiceProxyConfigs(content string) map[string]traefikcfg.ServiceProxyConfig {
	result := map[string]traefikcfg.ServiceProxyConfig{}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return result
	}

	var cf composeFile
	if err := yaml.Unmarshal([]byte(trimmed), &cf); err != nil {
		// unparseable YAML – return empty, compose itself will surface errors
		return result
	}

	for svcName, svcNode := range cf.Services {
		if svcNode.XOpenSandbox == nil || svcNode.XOpenSandbox.Proxy == nil {
			continue
		}
		p := svcNode.XOpenSandbox.Proxy
		cfg := traefikcfg.ServiceProxyConfig{
			RequestHeaders:  p.RequestHeaders,
			ResponseHeaders: p.ResponseHeaders,
			PathPrefixStrip: strings.TrimSpace(p.PathPrefixStrip),
			SkipAuth:        p.SkipAuth,
		}
		if p.CORS != nil {
			cfg.CORS = &traefikcfg.CORSConfig{
				AllowOrigins:     p.CORS.AllowOrigins,
				AllowMethods:     p.CORS.AllowMethods,
				AllowHeaders:     p.CORS.AllowHeaders,
				AllowCredentials: p.CORS.AllowCredentials,
				MaxAge:           p.CORS.MaxAge,
			}
		}
		if !cfg.IsEmpty() {
			result[svcName] = cfg
		}
	}

	return result
}
