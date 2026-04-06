package api

import "testing"

func TestLoadPreviewRoutingConfigDerivesPreviewDomainFromPublicBaseURL(t *testing.T) {
	t.Setenv("SANDBOX_PUBLIC_BASE_URL", "https://sandbox.shaiks.space")
	t.Setenv("SANDBOX_PREVIEW_BASE_DOMAIN", "")

	cfg := loadPreviewRoutingConfig()
	if cfg.PreviewBaseDomain != "preview.shaiks.space" {
		t.Fatalf("expected derived preview base domain preview.shaiks.space, got %q", cfg.PreviewBaseDomain)
	}
}

func TestLoadPreviewRoutingConfigRespectsConfiguredPreviewDomain(t *testing.T) {
	t.Setenv("SANDBOX_PUBLIC_BASE_URL", "https://sandbox.shaiks.space")
	t.Setenv("SANDBOX_PREVIEW_BASE_DOMAIN", "custom-preview.example.com")

	cfg := loadPreviewRoutingConfig()
	if cfg.PreviewBaseDomain != "custom-preview.example.com" {
		t.Fatalf("expected configured preview base domain, got %q", cfg.PreviewBaseDomain)
	}
}

func TestDerivePreviewBaseDomain(t *testing.T) {
	tests := []struct {
		name       string
		publicHost string
		expected   string
	}{
		{name: "app host suffix", publicHost: "app.lvh.me", expected: "preview.lvh.me"},
		{name: "custom app host suffix", publicHost: "sandbox.shaiks.space", expected: "preview.shaiks.space"},
		{name: "two labels gets prefixed", publicHost: "example.com", expected: "preview.example.com"},
		{name: "preview host remains same", publicHost: "preview.shaiks.space", expected: "preview.shaiks.space"},
		{name: "localhost unsupported", publicHost: "localhost", expected: ""},
		{name: "ip unsupported", publicHost: "127.0.0.1", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := derivePreviewBaseDomain(tc.publicHost); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
