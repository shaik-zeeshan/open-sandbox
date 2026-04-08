package traefik

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeConfigDefaultBehaviorWithNoProxyConfig(t *testing.T) {
	// Existing behavior must be preserved when ProxyConfig is zero value.
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{
		AppHost:           "app.lvh.me:3000",
		PreviewBaseDomain: "preview.lvh.me",
	})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		ComposeProjects: map[string][]ComposeServicePort{
			"myapp": {
				{Service: "web", Private: 80, Public: 50080},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	f := filepath.Join(dir, "compose-myapp.yaml")
	// Default: forward-auth middleware present
	assertFileContains(t, f, "preview-forward-auth-placeholder")
	// Default: preview header placeholder present
	assertFileContains(t, f, "preview-header-placeholder")
	// No custom-headers middleware emitted
	assertFileNotContains(t, f, "custom-headers")
	// No strip-prefix middleware emitted
	assertFileNotContains(t, f, "strip-prefix")
	// Standard target headers present
	assertFileContains(t, f, "X-Open-Sandbox-Proxy-Type: \"compose\"")
	assertFileContains(t, f, "X-Open-Sandbox-Proxy-Service: \"web\"")
}

func TestComposeConfigCustomRequestAndResponseHeaders(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{
		AppHost:           "app.lvh.me:3000",
		PreviewBaseDomain: "preview.lvh.me",
	})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		ComposeProjects: map[string][]ComposeServicePort{
			"myapp": {
				{
					Service: "api",
					Private: 3000,
					Public:  53000,
					ProxyConfig: ServiceProxyConfig{
						RequestHeaders: map[string]string{
							"X-Tenant":       "acme",
							"X-Custom-Token": "tok123",
						},
						ResponseHeaders: map[string]string{
							"X-Frame-Options": "DENY",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	f := filepath.Join(dir, "compose-myapp.yaml")
	assertFileContains(t, f, "custom-headers")
	assertFileContains(t, f, "customRequestHeaders:")
	assertFileContains(t, f, "X-Custom-Token: \"tok123\"")
	assertFileContains(t, f, "X-Tenant: \"acme\"")
	assertFileContains(t, f, "customResponseHeaders:")
	assertFileContains(t, f, "X-Frame-Options: \"DENY\"")
	// Default auth middleware still present (SkipAuth = false)
	assertFileContains(t, f, "preview-forward-auth-placeholder")
}

func TestComposeConfigCORSHeaders(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{
		AppHost:           "app.lvh.me:3000",
		PreviewBaseDomain: "preview.lvh.me",
	})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		ComposeProjects: map[string][]ComposeServicePort{
			"myapp": {
				{
					Service: "frontend",
					Private: 8080,
					Public:  58080,
					ProxyConfig: ServiceProxyConfig{
						CORS: &CORSConfig{
							AllowOrigins:     []string{"https://app.example.com", "https://staging.example.com"},
							AllowMethods:     []string{"GET", "POST", "OPTIONS"},
							AllowHeaders:     []string{"Authorization", "Content-Type"},
							AllowCredentials: true,
							MaxAge:           3600,
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	f := filepath.Join(dir, "compose-myapp.yaml")
	assertFileContains(t, f, "accessControlAllowOriginList:")
	assertFileContains(t, f, "\"https://app.example.com\"")
	assertFileContains(t, f, "\"https://staging.example.com\"")
	assertFileContains(t, f, "accessControlAllowMethods:")
	assertFileContains(t, f, "\"GET\"")
	assertFileContains(t, f, "accessControlAllowHeaders:")
	assertFileContains(t, f, "\"Authorization\"")
	assertFileContains(t, f, "accessControlAllowCredentials: true")
	assertFileContains(t, f, "accessControlMaxAge: 3600")
}

func TestComposeConfigPathPrefixStrip(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{
		AppHost:           "app.lvh.me:3000",
		PreviewBaseDomain: "preview.lvh.me",
	})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		ComposeProjects: map[string][]ComposeServicePort{
			"myapp": {
				{
					Service: "api",
					Private: 3000,
					Public:  53000,
					ProxyConfig: ServiceProxyConfig{
						PathPrefixStrip: "/api",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	f := filepath.Join(dir, "compose-myapp.yaml")
	assertFileContains(t, f, "strip-prefix")
	assertFileContains(t, f, "stripPrefix:")
	assertFileContains(t, f, "prefixes:")
	assertFileContains(t, f, "\"/api\"")
	// Default auth still present
	assertFileContains(t, f, "preview-forward-auth-placeholder")
}

func TestComposeConfigSkipAuth(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{
		AppHost:           "app.lvh.me:3000",
		PreviewBaseDomain: "preview.lvh.me",
	})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		ComposeProjects: map[string][]ComposeServicePort{
			"myapp": {
				{
					Service: "public",
					Private: 80,
					Public:  50080,
					ProxyConfig: ServiceProxyConfig{
						SkipAuth: true,
					},
				},
				{
					Service: "private",
					Private: 3000,
					Public:  53000,
					// No SkipAuth
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	f := filepath.Join(dir, "compose-myapp.yaml")
	content, err := readFileString(t, f)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// The "public" router section should NOT include forward-auth.
	// The "private" router section SHOULD include it.
	publicRouterIdx := strings.Index(content, "preview-compose-myapp-public-80-router:")
	privateRouterIdx := strings.Index(content, "preview-compose-myapp-private-3000-router:")

	if publicRouterIdx == -1 || privateRouterIdx == -1 {
		t.Fatalf("expected both routers in output, public=%d private=%d", publicRouterIdx, privateRouterIdx)
	}

	// Find the forward-auth references. They should only appear in the private
	// router's middleware block, not the public one.
	// We check that forward-auth appears after privateRouterIdx.
	forwardAuthIdx := strings.Index(content, "preview-forward-auth-placeholder")
	if forwardAuthIdx == -1 {
		t.Fatalf("expected forward-auth in output for private service")
	}
	if forwardAuthIdx < privateRouterIdx {
		// The first forward-auth reference is before private router – that's fine
		// only if it's in the core config placeholder reference... but this file
		// is a per-compose file so it should only exist for the private service.
		// We verify the public router block does NOT contain it.
		// Find end of public router block: next top-level router or services section.
		publicRouterEnd := strings.Index(content[publicRouterIdx:], "preview-compose-myapp-private")
		if publicRouterEnd == -1 {
			publicRouterEnd = strings.Index(content[publicRouterIdx:], "\n  services:")
		}
		if publicRouterEnd == -1 {
			t.Logf("compose output:\n%s", content)
			t.Fatal("could not find end of public router block")
		}
		publicRouterBlock := content[publicRouterIdx : publicRouterIdx+publicRouterEnd]
		if strings.Contains(publicRouterBlock, "preview-forward-auth-placeholder") {
			t.Fatalf("expected public router (skip_auth=true) to NOT include forward-auth middleware\nblock:\n%s", publicRouterBlock)
		}
	}
}

func TestComposeConfigProxyConfigPreservedThroughNormalization(t *testing.T) {
	// When two ports for the same service/private combo are provided, the
	// ProxyConfig from the lowest-public-port entry must be preserved.
	ports := []ComposeServicePort{
		{Service: "web", Private: 80, Public: 50090, ProxyConfig: ServiceProxyConfig{SkipAuth: true}},
		{Service: "web", Private: 80, Public: 50080, ProxyConfig: ServiceProxyConfig{SkipAuth: true}},
	}
	normalized := normalizedComposePorts(ports)
	if len(normalized) != 1 {
		t.Fatalf("expected 1 normalized port, got %d", len(normalized))
	}
	if normalized[0].Public != 50080 {
		t.Fatalf("expected lowest public port 50080, got %d", normalized[0].Public)
	}
	if !normalized[0].ProxyConfig.SkipAuth {
		t.Fatal("expected ProxyConfig to be preserved after normalization")
	}
}

func TestServiceProxyConfigIsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServiceProxyConfig
		isEmpty bool
	}{
		{"zero value", ServiceProxyConfig{}, true},
		{"skip_auth only", ServiceProxyConfig{SkipAuth: true}, false},
		{"request headers only", ServiceProxyConfig{RequestHeaders: map[string]string{"X-Foo": "bar"}}, false},
		{"response headers only", ServiceProxyConfig{ResponseHeaders: map[string]string{"X-Bar": "baz"}}, false},
		{"cors only", ServiceProxyConfig{CORS: &CORSConfig{AllowOrigins: []string{"*"}}}, false},
		{"path strip only", ServiceProxyConfig{PathPrefixStrip: "/api"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.IsEmpty()
			if got != tc.isEmpty {
				t.Fatalf("IsEmpty() = %v, want %v", got, tc.isEmpty)
			}
		})
	}
}

func readFileString(t *testing.T, path string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(path)
	return string(b), err
}

func TestIsValidHeaderName(t *testing.T) {
	valid := []string{
		"X-Custom-Token",
		"Authorization",
		"Content-Type",
		"X-Frame-Options",
		"Accept",
		"X-My-Header-123",
	}
	for _, h := range valid {
		if !isValidHeaderName(h) {
			t.Errorf("expected %q to be valid header name", h)
		}
	}

	invalid := []string{
		"",
		"Bad:Header",
		"Bad Header",
		"Bad\nHeader",
		"Bad\rHeader",
		"Bad\tHeader",
		"Bad\"Header",
		"Bad/Header",
		"Bad\\Header",
		"Header\x00Null",
		"Header\x80High",
		":StartsWithColon",
		"Has Space",
	}
	for _, h := range invalid {
		if isValidHeaderName(h) {
			t.Errorf("expected %q to be invalid header name", h)
		}
	}
}

func TestComposeConfigInvalidHeaderKeysAreSkipped(t *testing.T) {
	// Crafted header keys containing YAML-breaking characters must be silently
	// dropped; valid keys in the same map must still appear in the output.
	dir := t.TempDir()
	writer, err := NewConfigWriter(dir, ConfigWriterOptions{
		AppHost:           "app.lvh.me:3000",
		PreviewBaseDomain: "preview.lvh.me",
	})
	if err != nil {
		t.Fatalf("new config writer: %v", err)
	}

	err = writer.Reconcile(WorkloadRoutes{
		ComposeProjects: map[string][]ComposeServicePort{
			"myapp": {
				{
					Service: "api",
					Private: 3000,
					Public:  53000,
					ProxyConfig: ServiceProxyConfig{
						RequestHeaders: map[string]string{
							"X-Good-Request":      "ok",
							"Bad:Request\nInject": "evil",
							"Another: Bad":        "also-evil",
							"Newline\nKey":        "bad",
						},
						ResponseHeaders: map[string]string{
							"X-Good-Response":      "ok",
							"Bad:Response\nInject": "evil",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	f := filepath.Join(dir, "compose-myapp.yaml")
	// Valid keys must appear
	assertFileContains(t, f, "X-Good-Request: \"ok\"")
	assertFileContains(t, f, "X-Good-Response: \"ok\"")
	// Invalid keys (and their values) must NOT appear
	assertFileNotContains(t, f, "Bad:Request")
	assertFileNotContains(t, f, "Bad:Response")
	assertFileNotContains(t, f, "Inject")
	assertFileNotContains(t, f, "Another: Bad")
	assertFileNotContains(t, f, "Newline")
	assertFileNotContains(t, f, "evil")
}
