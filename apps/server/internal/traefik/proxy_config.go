package traefik

// ServiceProxyConfig holds per-service proxy customization options derived from
// the x-open-sandbox.proxy extension field in a compose service definition.
//
// Schema example (compose YAML):
//
//	services:
//	  web:
//	    image: nginx:latest
//	    x-open-sandbox:
//	      proxy:
//	        request_headers:
//	          X-Custom-Token: "abc"
//	        response_headers:
//	          X-Frame-Options: "DENY"
//	        cors:
//	          allow_origins: ["https://app.example.com"]
//	          allow_methods: ["GET","POST"]
//	          allow_headers: ["Authorization","Content-Type"]
//	          allow_credentials: true
//	          max_age: 3600
//	        path_prefix_strip: "/api"
//	        skip_auth: true
//
// All fields are optional. When absent the service behaves with the default
// platform middleware (forward-auth + preview header injection).
type ServiceProxyConfig struct {
	// RequestHeaders are custom headers added to every proxied request.
	RequestHeaders map[string]string `yaml:"request_headers"`

	// ResponseHeaders are custom headers added to every proxied response.
	ResponseHeaders map[string]string `yaml:"response_headers"`

	// CORS holds common cross-origin resource sharing / security header
	// settings. When non-nil a dedicated headers middleware is emitted.
	CORS *CORSConfig `yaml:"cors"`

	// PathPrefixStrip, when non-empty, strips the given path prefix before
	// forwarding the request upstream (Traefik stripPrefix middleware).
	PathPrefixStrip string `yaml:"path_prefix_strip"`

	// SkipAuth bypasses the default forward-auth middleware for this service.
	// Useful for public endpoints that do not require sandbox authentication.
	SkipAuth bool `yaml:"skip_auth"`
}

// CORSConfig holds CORS / security header values that map to Traefik headers
// middleware. Fields mirror common Traefik headers middleware knobs.
type CORSConfig struct {
	AllowOrigins     []string `yaml:"allow_origins"`
	AllowMethods     []string `yaml:"allow_methods"`
	AllowHeaders     []string `yaml:"allow_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// IsEmpty returns true when the config carries no effective customization.
func (c ServiceProxyConfig) IsEmpty() bool {
	if len(c.RequestHeaders) > 0 {
		return false
	}
	if len(c.ResponseHeaders) > 0 {
		return false
	}
	if c.CORS != nil {
		return false
	}
	if c.PathPrefixStrip != "" {
		return false
	}
	if c.SkipAuth {
		return false
	}
	return true
}
