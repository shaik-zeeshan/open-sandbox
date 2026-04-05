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
