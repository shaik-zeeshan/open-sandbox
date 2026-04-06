package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	"golang.org/x/time/rate"
)

func TestParseProxyAuthorizationTarget(t *testing.T) {
	target, err := parseProxyAuthorizationTarget("/proxy/compose/demo/web/3000/path?x=1")
	if err != nil {
		t.Fatalf("expected target to parse: %v", err)
	}
	if target.WorkloadType != "compose" || target.ProjectName != "demo" || target.ServiceName != "web" || target.PrivatePort != 3000 {
		t.Fatalf("unexpected parsed target: %+v", target)
	}

	if _, err := parseProxyAuthorizationTarget("/api/containers"); err == nil {
		t.Fatal("expected non-proxy path to be rejected")
	}
}

func TestProxyAuthorizeRejectsMissingCredentials(t *testing.T) {
	s := newTestServerWithStore(&mockDocker{}, &mockSandboxStore{})
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("X-Forwarded-Uri", "/proxy/sandboxes/sandbox-1/3000/")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestProxyAuthorizeAllowsOwnedSandboxPublishedPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"3000/tcp,0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("X-Forwarded-Uri", "/proxy/sandboxes/sandbox-1/80/")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestProxyAuthorizeRateLimited(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"3000/tcp,0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	s.proxyAuthLimiter = newProxyAuthRateLimiter(proxyAuthRateLimitConfig{RequestsPerSecond: rate.Limit(0.0001), Burst: 1, IdleTTL: time.Minute})

	first := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	first.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	first.Header.Set("X-Forwarded-Uri", "/proxy/sandboxes/sandbox-1/80/")
	firstWriter := httptest.NewRecorder()
	s.Router().ServeHTTP(firstWriter, first)
	if firstWriter.Code != http.StatusOK {
		t.Fatalf("expected first request to be allowed, got %d", firstWriter.Code)
	}

	second := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	second.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	second.Header.Set("X-Forwarded-Uri", "/proxy/sandboxes/sandbox-1/80/")
	secondWriter := httptest.NewRecorder()
	s.Router().ServeHTTP(secondWriter, second)
	if secondWriter.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", secondWriter.Code)
	}
}

func TestProxyAuthorizeRejectsForeignSandboxAccess(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"3000/tcp,0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-2", Username: "bob", Role: roleMember}))
	req.Header.Set("X-Forwarded-Uri", "/proxy/sandboxes/sandbox-1/80/")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestProxyAuthorizeComposeRequiresPublishedPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"compose-web","Image":"nginx:latest","Names":"demo-web-1","Ports":"3000/tcp","Status":"Up 1 minute","Labels":"open-sandbox.owner_id=member-1,com.docker.compose.project=demo,com.docker.compose.service=web"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create compose project dir: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}); err != nil {
		t.Fatalf("write compose owner metadata: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("X-Forwarded-Uri", "/proxy/compose/demo/web/3000/")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unpublished compose port, got %d", w.Code)
	}
}

func TestProxyAuthorizeSandboxRequiresPublishedPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"3000/tcp,0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("X-Forwarded-Uri", "/proxy/sandboxes/sandbox-1/3000/")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unpublished sandbox port, got %d", w.Code)
	}
}

func TestProxyAuthorizeAllowsComposePublishedPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"compose-web","Image":"nginx:latest","Names":"demo-web-1","Ports":"0.0.0.0:53000->3000/tcp","Status":"Up 1 minute","Labels":"open-sandbox.owner_id=member-1,com.docker.compose.project=demo,com.docker.compose.service=web"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create compose project dir: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}); err != nil {
		t.Fatalf("write compose owner metadata: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("X-Forwarded-Uri", "/proxy/compose/demo/web/3000/")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}
