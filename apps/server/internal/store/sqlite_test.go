package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
)

func newSQLiteStoreForTest(t *testing.T) *SQLiteStore {
	t.Helper()

	s, err := OpenSQLite(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func TestRefreshTokenRotationLifecycle(t *testing.T) {
	s := newSQLiteStoreForTest(t)
	now := time.Now().Unix()

	original := RefreshTokenRecord{
		ID:        "rt-1",
		TokenHash: "hash-1",
		UserID:    "user-1",
		ExpiresAt: now + 3600,
		CreatedAt: now,
	}
	if err := s.CreateRefreshToken(t.Context(), original); err != nil {
		t.Fatalf("failed to create refresh token: %v", err)
	}

	replacement := RefreshTokenRecord{
		ID:        "rt-2",
		TokenHash: "hash-2",
		UserID:    "user-1",
		ExpiresAt: now + 7200,
		CreatedAt: now + 1,
	}
	if err := s.RotateRefreshToken(t.Context(), original.ID, replacement, now+1); err != nil {
		t.Fatalf("failed to rotate refresh token: %v", err)
	}

	oldToken, err := s.GetRefreshTokenByHash(t.Context(), original.TokenHash)
	if err != nil {
		t.Fatalf("failed to read old refresh token: %v", err)
	}
	if oldToken.RotatedAt == 0 {
		t.Fatal("expected old refresh token to be marked rotated")
	}
	if oldToken.ReplacedByTokenID != replacement.ID {
		t.Fatalf("expected old token replacement id %q, got %q", replacement.ID, oldToken.ReplacedByTokenID)
	}

	newToken, err := s.GetRefreshTokenByHash(t.Context(), replacement.TokenHash)
	if err != nil {
		t.Fatalf("failed to read replacement refresh token: %v", err)
	}
	if newToken.ID != replacement.ID {
		t.Fatalf("expected replacement token id %q, got %q", replacement.ID, newToken.ID)
	}

	if err := s.RotateRefreshToken(t.Context(), original.ID, RefreshTokenRecord{
		ID:        "rt-3",
		TokenHash: "hash-3",
		UserID:    "user-1",
		ExpiresAt: now + 10800,
	}, now+2); !errors.Is(err, ErrRefreshTokenInactive) {
		t.Fatalf("expected ErrRefreshTokenInactive on second rotate, got %v", err)
	}
}

func TestRevokeRefreshTokenByHash(t *testing.T) {
	s := newSQLiteStoreForTest(t)
	now := time.Now().Unix()

	if err := s.CreateRefreshToken(t.Context(), RefreshTokenRecord{
		ID:        "rt-1",
		TokenHash: "hash-1",
		UserID:    "user-1",
		ExpiresAt: now + 3600,
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("failed to create refresh token: %v", err)
	}

	if err := s.RevokeRefreshTokenByHash(t.Context(), "hash-1", now+1); err != nil {
		t.Fatalf("failed to revoke refresh token: %v", err)
	}

	token, err := s.GetRefreshTokenByHash(t.Context(), "hash-1")
	if err != nil {
		t.Fatalf("failed to read revoked refresh token: %v", err)
	}
	if token.RevokedAt == 0 {
		t.Fatal("expected token to be revoked")
	}

	if err := s.RevokeRefreshTokenByHash(t.Context(), "hash-1", now+2); !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Fatalf("expected ErrRefreshTokenNotFound when revoking inactive token, got %v", err)
	}
}

func TestCreateSandboxDefaultsWorkerIDToLocal(t *testing.T) {
	s := newSQLiteStoreForTest(t)

	if err := s.CreateSandbox(t.Context(), Sandbox{
		ID:            "sb-1",
		Name:          "sandbox",
		Image:         "alpine:3.20",
		ContainerID:   "ctr-1",
		WorkspaceDir:  "/workspace",
		RepoURL:       "",
		Status:        "running",
		OwnerID:       "user-1",
		OwnerUsername: "alice",
	}); err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	stored, err := s.GetSandbox(t.Context(), "sb-1")
	if err != nil {
		t.Fatalf("failed to read sandbox: %v", err)
	}
	if stored.WorkerID != "local" {
		t.Fatalf("expected default worker_id local, got %q", stored.WorkerID)
	}
}

func TestRuntimeWorkerLifecycle(t *testing.T) {
	s := newSQLiteStoreForTest(t)
	now := time.Now().Unix()

	if err := s.UpsertRuntimeWorker(t.Context(), RuntimeWorker{
		ID:                  "worker-a",
		Name:                "worker-a",
		AdvertiseAddress:    "http://10.0.0.2:8080",
		ExecutionMode:       "docker",
		Status:              "active",
		Version:             "v1",
		Labels:              map[string]string{"zone": "lab"},
		RegisteredAt:        now,
		LastHeartbeatAt:     now,
		HeartbeatTTLSeconds: 15,
		UpdatedAt:           now,
	}); err != nil {
		t.Fatalf("failed to upsert worker: %v", err)
	}

	if err := s.TouchRuntimeWorkerHeartbeat(t.Context(), "worker-a", now+5, "degraded", "http://10.0.0.2:8081", "v2", map[string]string{"zone": "lab", "gpu": "false"}); err != nil {
		t.Fatalf("failed to touch worker heartbeat: %v", err)
	}

	stored, err := s.GetRuntimeWorker(t.Context(), "worker-a")
	if err != nil {
		t.Fatalf("failed to get worker: %v", err)
	}
	if stored.Status != "degraded" {
		t.Fatalf("expected worker status degraded, got %q", stored.Status)
	}
	if stored.AdvertiseAddress != "http://10.0.0.2:8081" {
		t.Fatalf("expected updated advertise address, got %q", stored.AdvertiseAddress)
	}
	if stored.LastHeartbeatAt != now+5 {
		t.Fatalf("expected updated heartbeat time %d, got %d", now+5, stored.LastHeartbeatAt)
	}
	if stored.Labels["gpu"] != "false" {
		t.Fatalf("expected labels to persist, got %#v", stored.Labels)
	}

	workers, err := s.ListRuntimeWorkers(t.Context())
	if err != nil {
		t.Fatalf("failed to list workers: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected one worker, got %d", len(workers))
	}
}

func TestSandboxProxyConfigRoundTrip(t *testing.T) {
	s := newSQLiteStoreForTest(t)

	proxyConfig := map[int]traefikcfg.ServiceProxyConfig{
		3000: {
			RequestHeaders:  map[string]string{"X-Tenant": "acme"},
			ResponseHeaders: map[string]string{"X-Frame-Options": "DENY"},
			PathPrefixStrip: "/api",
			SkipAuth:        false,
		},
		8080: {
			SkipAuth: true,
			CORS: &traefikcfg.CORSConfig{
				AllowOrigins:     []string{"https://example.com"},
				AllowMethods:     []string{"GET", "POST"},
				AllowHeaders:     []string{"Authorization"},
				AllowCredentials: true,
				MaxAge:           3600,
			},
		},
	}

	if err := s.CreateSandbox(t.Context(), Sandbox{
		ID:            "sb-proxy-1",
		Name:          "proxy-test",
		Image:         "alpine:3.20",
		ContainerID:   "ctr-proxy-1",
		WorkspaceDir:  "/workspace",
		Status:        "running",
		OwnerID:       "user-1",
		OwnerUsername: "alice",
		ProxyConfig:   proxyConfig,
	}); err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	// GetSandbox round-trip
	got, err := s.GetSandbox(t.Context(), "sb-proxy-1")
	if err != nil {
		t.Fatalf("failed to get sandbox: %v", err)
	}
	if len(got.ProxyConfig) != 2 {
		t.Fatalf("expected 2 proxy config entries, got %d", len(got.ProxyConfig))
	}
	cfg3000 := got.ProxyConfig[3000]
	if cfg3000.RequestHeaders["X-Tenant"] != "acme" {
		t.Fatalf("expected request header X-Tenant=acme, got %q", cfg3000.RequestHeaders["X-Tenant"])
	}
	if cfg3000.PathPrefixStrip != "/api" {
		t.Fatalf("expected path_prefix_strip /api, got %q", cfg3000.PathPrefixStrip)
	}
	cfg8080 := got.ProxyConfig[8080]
	if !cfg8080.SkipAuth {
		t.Fatal("expected skip_auth=true for port 8080")
	}
	if cfg8080.CORS == nil || cfg8080.CORS.MaxAge != 3600 {
		t.Fatalf("expected CORS max_age=3600 for port 8080, got %v", cfg8080.CORS)
	}

	// ListSandboxes round-trip
	list, err := s.ListSandboxes(t.Context())
	if err != nil {
		t.Fatalf("failed to list sandboxes: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 sandbox, got %d", len(list))
	}
	if len(list[0].ProxyConfig) != 2 {
		t.Fatalf("expected 2 proxy config entries in list, got %d", len(list[0].ProxyConfig))
	}
}

func TestSandboxProxyConfigEmptyByDefault(t *testing.T) {
	s := newSQLiteStoreForTest(t)

	if err := s.CreateSandbox(t.Context(), Sandbox{
		ID:            "sb-no-proxy",
		Name:          "no-proxy-test",
		Image:         "alpine:3.20",
		ContainerID:   "ctr-no-proxy-1",
		WorkspaceDir:  "/workspace",
		Status:        "running",
		OwnerID:       "user-1",
		OwnerUsername: "alice",
		// ProxyConfig intentionally nil/empty
	}); err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	got, err := s.GetSandbox(t.Context(), "sb-no-proxy")
	if err != nil {
		t.Fatalf("failed to get sandbox: %v", err)
	}
	if got.ProxyConfig == nil {
		// nil is acceptable; just check length is 0
		return
	}
	if len(got.ProxyConfig) != 0 {
		t.Fatalf("expected empty proxy config, got %d entries", len(got.ProxyConfig))
	}
}

func TestUpdateSandboxProxyConfig(t *testing.T) {
	s := newSQLiteStoreForTest(t)

	if err := s.CreateSandbox(t.Context(), Sandbox{
		ID:            "sb-update-proxy",
		Name:          "update-proxy-test",
		Image:         "alpine:3.20",
		ContainerID:   "ctr-update-proxy-1",
		WorkspaceDir:  "/workspace",
		Status:        "running",
		OwnerID:       "user-1",
		OwnerUsername: "alice",
		CreatedAt:     100,
		UpdatedAt:     100,
	}); err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	proxyConfig := map[int]traefikcfg.ServiceProxyConfig{
		3000: {
			RequestHeaders: map[string]string{"X-Test": "one"},
			CORS: &traefikcfg.CORSConfig{
				AllowOrigins: []string{"https://example.com"},
				MaxAge:       60,
			},
		},
	}
	if err := s.UpdateSandboxProxyConfig(t.Context(), "sb-update-proxy", proxyConfig); err != nil {
		t.Fatalf("failed to update sandbox proxy config: %v", err)
	}

	got, err := s.GetSandbox(t.Context(), "sb-update-proxy")
	if err != nil {
		t.Fatalf("failed to read updated sandbox: %v", err)
	}
	if got.ProxyConfig[3000].RequestHeaders["X-Test"] != "one" {
		t.Fatalf("unexpected proxy config after update: %+v", got.ProxyConfig)
	}
	if got.ProxyConfig[3000].CORS == nil || got.ProxyConfig[3000].CORS.MaxAge != 60 {
		t.Fatalf("unexpected cors config after update: %+v", got.ProxyConfig[3000])
	}
	if got.UpdatedAt <= 100 {
		t.Fatalf("expected updated_at to advance, got %d", got.UpdatedAt)
	}

	if err := s.UpdateSandboxProxyConfig(t.Context(), "sb-update-proxy", nil); err != nil {
		t.Fatalf("failed to clear sandbox proxy config: %v", err)
	}

	cleared, err := s.GetSandbox(t.Context(), "sb-update-proxy")
	if err != nil {
		t.Fatalf("failed to read cleared sandbox: %v", err)
	}
	if len(cleared.ProxyConfig) != 0 {
		t.Fatalf("expected cleared proxy config, got %+v", cleared.ProxyConfig)
	}
}

func TestUnmarshalSandboxProxyConfigRejectsMalformedPortKeys(t *testing.T) {
	got, err := unmarshalSandboxProxyConfig(`{"3000/tcp":{"PathPrefixStrip":"/bad"},"3000abc":{"PathPrefixStrip":"/bad"}," 8080 ":{"PathPrefixStrip":"/api"}}`)
	if err != nil {
		t.Fatalf("unmarshal sandbox proxy config: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only strictly numeric ports to be accepted, got %+v", got)
	}
	if got[8080].PathPrefixStrip != "/api" {
		t.Fatalf("expected parsed config for port 8080, got %+v", got[8080])
	}
}
