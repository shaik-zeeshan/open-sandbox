package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	"golang.org/x/time/rate"
)

func TestParseProxyAuthorizationTarget(t *testing.T) {
	target, err := parseProxyAuthorizationTargetFromHeaders(http.Header{
		proxyTypeHeaderName:        []string{previewTargetTypeCompose},
		proxyProjectHeaderName:     []string{"demo"},
		proxyServiceHeaderName:     []string{"web"},
		proxyPrivatePortHeaderName: []string{"3000"},
	})
	if err != nil {
		t.Fatalf("expected target to parse: %v", err)
	}
	if target.WorkloadType != "compose" || target.ProjectName != "demo" || target.ServiceName != "web" || target.PrivatePort != 3000 {
		t.Fatalf("unexpected parsed target: %+v", target)
	}

	if _, err := parseProxyAuthorizationTargetFromHeaders(http.Header{}); err == nil {
		t.Fatal("expected missing headers to be rejected")
	}
}

func TestProxyAuthorizeRejectsMissingCredentials(t *testing.T) {
	s := newTestServerWithStore(&mockDocker{}, &mockSandboxStore{})
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	setSandboxProxyHeaders(req, "sandbox-1", 3000)
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
	setSandboxProxyHeaders(req, "sandbox-1", 80)
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
	setSandboxProxyHeaders(first, "sandbox-1", 80)
	firstWriter := httptest.NewRecorder()
	s.Router().ServeHTTP(firstWriter, first)
	if firstWriter.Code != http.StatusOK {
		t.Fatalf("expected first request to be allowed, got %d", firstWriter.Code)
	}

	second := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	second.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	setSandboxProxyHeaders(second, "sandbox-1", 80)
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
	setSandboxProxyHeaders(req, "sandbox-1", 80)
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
	setComposeProxyHeaders(req, "demo", "web", 3000)
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
	setSandboxProxyHeaders(req, "sandbox-1", 3000)
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
	setComposeProxyHeaders(req, "demo", "web", 3000)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestProxyAuthorizeAllowsOwnedDirectContainerPublishedPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"direct-web","Image":"nginx:latest","Names":"direct-web","Ports":"0.0.0.0:58080->80/tcp,3000/tcp","Status":"Up 1 minute","Labels":"open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	setContainerProxyHeaders(req, "ctr-123", 80)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestProxyAuthorizeRejectsUnpublishedDirectContainerPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"direct-web","Image":"nginx:latest","Names":"direct-web","Ports":"0.0.0.0:58080->80/tcp,3000/tcp","Status":"Up 1 minute","Labels":"open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	setContainerProxyHeaders(req, "ctr-123", 3000)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unpublished direct port, got %d", w.Code)
	}
}

func TestProxyAuthorizeRejectsForeignDirectContainerAccess(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"direct-web","Image":"nginx:latest","Names":"direct-web","Ports":"0.0.0.0:58080->80/tcp","Status":"Up 1 minute","Labels":"open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/auth/proxy/authorize", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-2", Username: "bob", Role: roleMember}))
	setContainerProxyHeaders(req, "ctr-123", 80)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for foreign direct container, got %d", w.Code)
	}
}

func TestPreviewLaunchRedirectIncludesConfiguredPublicPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	t.Setenv("SANDBOX_PUBLIC_BASE_URL", "http://app.lvh.me:3000")
	t.Setenv("SANDBOX_PREVIEW_CALLBACK_PATH", "/_sandbox/auth/callback")

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/auth/preview/launch/sandboxes/sandbox-1/80", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d (%s)", w.Code, w.Body.String())
	}

	redirectURL, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect url: %v", err)
	}
	if redirectURL.Scheme != "http" {
		t.Fatalf("expected http scheme, got %q", redirectURL.Scheme)
	}
	if redirectURL.Path != "/_sandbox/auth/callback" {
		t.Fatalf("expected callback path without trailing slash, got %q", redirectURL.Path)
	}
	if !strings.Contains(redirectURL.Host, ":3000") {
		t.Fatalf("expected redirect host to include configured public port, got %q", redirectURL.Host)
	}
	if strings.TrimSpace(redirectURL.Query().Get("grant")) == "" {
		t.Fatal("expected grant query token in redirect")
	}
}

func TestPreviewAuthCallbackAcceptsGrantForForwardedHostWithConfiguredPort(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	t.Setenv("SANDBOX_PUBLIC_BASE_URL", "http://app.lvh.me:3000")
	t.Setenv("SANDBOX_PREVIEW_CALLBACK_PATH", "/_sandbox/auth/callback/")

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	launchReq := httptest.NewRequest(http.MethodGet, "/auth/preview/launch/sandboxes/sandbox-1/80", nil)
	launchReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	launchW := httptest.NewRecorder()

	s.Router().ServeHTTP(launchW, launchReq)

	if launchW.Code != http.StatusFound {
		t.Fatalf("expected launch 302, got %d (%s)", launchW.Code, launchW.Body.String())
	}

	redirectURL, err := url.Parse(launchW.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse launch redirect url: %v", err)
	}
	grant := strings.TrimSpace(redirectURL.Query().Get("grant"))
	if grant == "" {
		t.Fatal("expected grant query token in launch redirect")
	}

	callbackReq := httptest.NewRequest(http.MethodGet, s.previewRouting.CallbackPath+"?grant="+url.QueryEscape(grant), nil)
	callbackReq.Header.Set("X-Forwarded-Host", redirectURL.Host)
	callbackW := httptest.NewRecorder()

	s.Router().ServeHTTP(callbackW, callbackReq)

	if callbackW.Code != http.StatusFound {
		t.Fatalf("expected callback 302, got %d (%s)", callbackW.Code, callbackW.Body.String())
	}
	if callbackW.Header().Get("Set-Cookie") == "" {
		t.Fatal("expected callback to set preview session cookie")
	}
}

func setSandboxProxyHeaders(req *http.Request, sandboxID string, privatePort int) {
	req.Header.Set(proxyTypeHeaderName, previewTargetTypeSandbox)
	req.Header.Set(proxyWorkloadIDHeaderName, sandboxID)
	req.Header.Set(proxyPrivatePortHeaderName, strconv.Itoa(privatePort))
}

func setContainerProxyHeaders(req *http.Request, managedID string, privatePort int) {
	req.Header.Set(proxyTypeHeaderName, previewTargetTypeDirect)
	req.Header.Set(proxyWorkloadIDHeaderName, managedID)
	req.Header.Set(proxyPrivatePortHeaderName, strconv.Itoa(privatePort))
}

func setComposeProxyHeaders(req *http.Request, projectName string, serviceName string, privatePort int) {
	req.Header.Set(proxyTypeHeaderName, previewTargetTypeCompose)
	req.Header.Set(proxyProjectHeaderName, projectName)
	req.Header.Set(proxyServiceHeaderName, serviceName)
	req.Header.Set(proxyPrivatePortHeaderName, strconv.Itoa(privatePort))
}
