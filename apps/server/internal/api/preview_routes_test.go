package api

import (
	"reflect"
	"testing"
)

func TestPreviewURLGenerationUsesPublishedPortsAndEscapedTokens(t *testing.T) {
	s := newTestServer(&mockDocker{})
	ports := []PortSummary{
		{Private: 8080, Public: 48080, Type: "tcp"},
		{Private: 80, Public: 40080, Type: "tcp"},
		{Private: 80, Public: 49080, Type: "tcp"},
		{Private: 3000, Public: 0, Type: "tcp"},
		{Private: 0, Public: 30000, Type: "tcp"},
	}

	t.Run("sandbox", func(t *testing.T) {
		got := s.previewURLsForSandbox(" sandbox /one ", ports)
		want := []PreviewURL{
			{PrivatePort: 80, URL: "/auth/preview/launch/sandboxes/sandbox%20%2Fone/80"},
			{PrivatePort: 8080, URL: "/auth/preview/launch/sandboxes/sandbox%20%2Fone/8080"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected sandbox preview urls: got=%+v want=%+v", got, want)
		}
	})

	t.Run("managed_container", func(t *testing.T) {
		got := s.previewURLsForManagedContainer(" ctr/123 ", ports)
		want := []PreviewURL{
			{PrivatePort: 80, URL: "/auth/preview/launch/containers/ctr%2F123/80"},
			{PrivatePort: 8080, URL: "/auth/preview/launch/containers/ctr%2F123/8080"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected managed preview urls: got=%+v want=%+v", got, want)
		}
	})

	t.Run("compose_service", func(t *testing.T) {
		got := s.previewURLsForComposeService(" demo/proj ", " web ui ", ports)
		want := []PreviewURL{
			{PrivatePort: 80, URL: "/auth/preview/launch/compose/demo%2Fproj/web%20ui/80"},
			{PrivatePort: 8080, URL: "/auth/preview/launch/compose/demo%2Fproj/web%20ui/8080"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected compose preview urls: got=%+v want=%+v", got, want)
		}
	})
}

func TestBuildComposeProjectPreviewTypedResponse(t *testing.T) {
	s := newTestServer(&mockDocker{})
	preview := s.buildComposeProjectPreview("demo project", []ContainerSummary{
		{
			ProjectName: "demo project",
			ServiceName: "web/api",
			Ports: []PortSummary{
				{Private: 443, Public: 50443, Type: "tcp", IP: "0.0.0.0"},
				{Private: 80, Public: 50080, Type: "tcp", IP: "::"},
				{Private: 80, Public: 50080, Type: "udp", IP: "127.0.0.1"},
				{Private: 80, Public: 50080, Type: "tcp", IP: "0.0.0.0"},
				{Private: 8080, Public: 0, Type: "tcp"},
			},
		},
		{
			ProjectName: "demo project",
			ServiceName: "db",
			Ports:       []PortSummary{{Private: 5432, Public: 0, Type: "tcp"}},
		},
		{
			ProjectName: "demo project",
			ServiceName: "api",
			Ports:       []PortSummary{{Private: 3000, Public: 53000, Type: "tcp", IP: "127.0.0.1"}},
		},
		{
			ProjectName: "other-project",
			ServiceName: "hidden",
			Ports:       []PortSummary{{Private: 8080, Public: 58080, Type: "tcp"}},
		},
	})

	if preview.ProjectName != "demo project" {
		t.Fatalf("unexpected project name: %q", preview.ProjectName)
	}
	if len(preview.Services) != 3 {
		t.Fatalf("expected 3 services, got %+v", preview.Services)
	}

	if preview.Services[0].ServiceName != "api" || preview.Services[1].ServiceName != "db" || preview.Services[2].ServiceName != "web/api" {
		t.Fatalf("expected services sorted by name, got %+v", preview.Services)
	}

	if len(preview.Services[0].Ports) != 1 {
		t.Fatalf("expected api service to include one published port, got %+v", preview.Services[0].Ports)
	}
	if preview.Services[0].Ports[0].PreviewURL != "/auth/preview/launch/compose/demo%20project/api/3000" {
		t.Fatalf("unexpected api preview url: %q", preview.Services[0].Ports[0].PreviewURL)
	}

	if len(preview.Services[1].Ports) != 0 {
		t.Fatalf("expected db service to expose no preview ports, got %+v", preview.Services[1].Ports)
	}

	webPorts := preview.Services[2].Ports
	if len(webPorts) != 3 {
		t.Fatalf("expected dedup by private/public/type only, got %+v", webPorts)
	}
	if webPorts[0].PrivatePort != 80 || webPorts[0].PublicPort != 50080 || webPorts[0].Type != "tcp" {
		t.Fatalf("unexpected first web port: %+v", webPorts[0])
	}
	if webPorts[0].PreviewURL != "/auth/preview/launch/compose/demo%20project/web%2Fapi/80" {
		t.Fatalf("unexpected escaped web preview url: %q", webPorts[0].PreviewURL)
	}
	if webPorts[1].Type != "udp" || webPorts[1].IP != "127.0.0.1" {
		t.Fatalf("expected typed compose preview entries to retain protocol and IP, got %+v", webPorts)
	}
	if webPorts[2].PrivatePort != 443 || webPorts[2].PublicPort != 50443 {
		t.Fatalf("expected web ports sorted by private/public/type, got %+v", webPorts)
	}
}
