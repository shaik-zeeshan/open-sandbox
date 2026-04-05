package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
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
