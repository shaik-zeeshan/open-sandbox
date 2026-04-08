package api

import (
	"testing"
)

func TestParseComposeServiceProxyConfigs_Empty(t *testing.T) {
	cases := []struct{ name, input string }{
		{"empty string", ""},
		{"whitespace only", "   \n  "},
		{"no services key", "version: \"3\"\n"},
		{"services no extension", "services:\n  web:\n    image: nginx:latest\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseComposeServiceProxyConfigs(tc.input)
			if len(got) != 0 {
				t.Fatalf("expected empty map, got %v", got)
			}
		})
	}
}

func TestParseComposeServiceProxyConfigs_InvalidYAML(t *testing.T) {
	got := parseComposeServiceProxyConfigs("{{{{not yaml")
	if len(got) != 0 {
		t.Fatalf("expected empty map for invalid YAML, got %v", got)
	}
}

func TestParseComposeServiceProxyConfigs_SkipAuth(t *testing.T) {
	input := `
services:
  public:
    image: nginx:latest
    x-open-sandbox:
      proxy:
        skip_auth: true
  private:
    image: nginx:latest
`
	got := parseComposeServiceProxyConfigs(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 service config, got %d: %v", len(got), got)
	}
	cfg, ok := got["public"]
	if !ok {
		t.Fatal("expected config for service 'public'")
	}
	if !cfg.SkipAuth {
		t.Fatal("expected SkipAuth=true")
	}
	if _, ok := got["private"]; ok {
		t.Fatal("did not expect config for 'private' (no extension)")
	}
}

func TestParseComposeServiceProxyConfigs_RequestResponseHeaders(t *testing.T) {
	input := `
services:
  api:
    image: myapp:latest
    x-open-sandbox:
      proxy:
        request_headers:
          X-Custom-Token: "abc"
          X-Tenant: "acme"
        response_headers:
          X-Frame-Options: "DENY"
`
	got := parseComposeServiceProxyConfigs(input)
	cfg, ok := got["api"]
	if !ok {
		t.Fatal("expected config for 'api'")
	}
	if cfg.RequestHeaders["X-Custom-Token"] != "abc" {
		t.Fatalf("expected X-Custom-Token=abc, got %q", cfg.RequestHeaders["X-Custom-Token"])
	}
	if cfg.RequestHeaders["X-Tenant"] != "acme" {
		t.Fatalf("expected X-Tenant=acme, got %q", cfg.RequestHeaders["X-Tenant"])
	}
	if cfg.ResponseHeaders["X-Frame-Options"] != "DENY" {
		t.Fatalf("expected X-Frame-Options=DENY, got %q", cfg.ResponseHeaders["X-Frame-Options"])
	}
}

func TestParseComposeServiceProxyConfigs_CORS(t *testing.T) {
	input := `
services:
  frontend:
    image: myapp:latest
    x-open-sandbox:
      proxy:
        cors:
          allow_origins:
            - "https://app.example.com"
            - "https://staging.example.com"
          allow_methods:
            - "GET"
            - "POST"
          allow_headers:
            - "Authorization"
            - "Content-Type"
          allow_credentials: true
          max_age: 3600
`
	got := parseComposeServiceProxyConfigs(input)
	cfg, ok := got["frontend"]
	if !ok {
		t.Fatal("expected config for 'frontend'")
	}
	if cfg.CORS == nil {
		t.Fatal("expected non-nil CORS config")
	}
	if len(cfg.CORS.AllowOrigins) != 2 || cfg.CORS.AllowOrigins[0] != "https://app.example.com" {
		t.Fatalf("unexpected AllowOrigins: %v", cfg.CORS.AllowOrigins)
	}
	if len(cfg.CORS.AllowMethods) != 2 {
		t.Fatalf("unexpected AllowMethods: %v", cfg.CORS.AllowMethods)
	}
	if len(cfg.CORS.AllowHeaders) != 2 {
		t.Fatalf("unexpected AllowHeaders: %v", cfg.CORS.AllowHeaders)
	}
	if !cfg.CORS.AllowCredentials {
		t.Fatal("expected AllowCredentials=true")
	}
	if cfg.CORS.MaxAge != 3600 {
		t.Fatalf("expected MaxAge=3600, got %d", cfg.CORS.MaxAge)
	}
}

func TestParseComposeServiceProxyConfigs_PathPrefixStrip(t *testing.T) {
	input := `
services:
  api:
    image: myapp:latest
    x-open-sandbox:
      proxy:
        path_prefix_strip: "  /api  "
`
	got := parseComposeServiceProxyConfigs(input)
	cfg, ok := got["api"]
	if !ok {
		t.Fatal("expected config for 'api'")
	}
	// Leading/trailing whitespace should be trimmed.
	if cfg.PathPrefixStrip != "/api" {
		t.Fatalf("expected PathPrefixStrip=/api, got %q", cfg.PathPrefixStrip)
	}
}

func TestParseComposeServiceProxyConfigs_EmptyProxyBlock(t *testing.T) {
	// A service with x-open-sandbox.proxy present but all zero values should
	// not appear in the result (IsEmpty guard).
	input := `
services:
  web:
    image: nginx:latest
    x-open-sandbox:
      proxy:
        skip_auth: false
`
	got := parseComposeServiceProxyConfigs(input)
	if len(got) != 0 {
		t.Fatalf("expected empty map for effectively-empty proxy config, got %v", got)
	}
}

func TestParseComposeServiceProxyConfigs_MultipleServices(t *testing.T) {
	input := `
services:
  frontend:
    image: ui:latest
    x-open-sandbox:
      proxy:
        skip_auth: true
  backend:
    image: api:latest
    x-open-sandbox:
      proxy:
        request_headers:
          X-Internal: "true"
  db:
    image: postgres:15
`
	got := parseComposeServiceProxyConfigs(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 configs, got %d: %v", len(got), got)
	}
	if !got["frontend"].SkipAuth {
		t.Fatal("expected frontend SkipAuth=true")
	}
	if got["backend"].RequestHeaders["X-Internal"] != "true" {
		t.Fatalf("expected backend X-Internal=true, got %q", got["backend"].RequestHeaders["X-Internal"])
	}
	if _, ok := got["db"]; ok {
		t.Fatal("did not expect config for 'db'")
	}
}
