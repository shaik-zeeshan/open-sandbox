package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

type authTestUserStore struct {
	apiKeys map[string]store.APIKeyRecord
	users   map[string]store.UserRecord
}

func (s *authTestUserStore) GetAPIKeyByHash(_ context.Context, keyHash string) (store.APIKeyRecord, error) {
	record, ok := s.apiKeys[keyHash]
	if !ok {
		return store.APIKeyRecord{}, store.ErrAPIKeyNotFound
	}
	return record, nil
}

func (s *authTestUserStore) GetUserByID(_ context.Context, userID string) (store.UserRecord, error) {
	user, ok := s.users[userID]
	if !ok {
		return store.UserRecord{}, store.ErrUserNotFound
	}
	return user, nil
}

func newAuthServer(t *testing.T, auth AuthConfig) *Server {
	t.Helper()
	sqliteStore, err := store.OpenSQLite(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	gin.SetMode(gin.TestMode)
	return NewServerWithStore(&mockDocker{}, auth, sqliteStore)
}

func seedUser(t *testing.T, s *Server, username string, password string, role string) store.User {
	t.Helper()
	passwordHash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	created, err := s.userStore.CreateUser(t.Context(), store.UserRecord{
		User: store.User{
			ID:       newRequestID(),
			Username: username,
			Role:     role,
		},
		PasswordHash: passwordHash,
	})
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return created
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "valid", header: "Bearer abc123", want: "abc123"},
		{name: "wrong scheme", header: "Basic abc123", want: ""},
		{name: "missing token", header: "Bearer", want: ""},
		{name: "empty", header: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBearerToken(tt.header)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestSecureEqual(t *testing.T) {
	if !secureEqual("same", "same") {
		t.Fatal("expected equal strings to match")
	}
	if secureEqual("same", "diff") {
		t.Fatal("expected different strings to not match")
	}
	if secureEqual("short", "much-longer-value") {
		t.Fatal("expected different lengths to not match")
	}
	if secureEqual("", "") {
		t.Fatal("expected empty strings to not match")
	}
}

func TestAuthMiddlewareJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID:   "user-1",
		Username: "tester",
		Role:     roleMember,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "open-sandbox",
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
		},
	})
	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddlewareRejectsMissingCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["reason"] != "token_missing" {
		t.Fatalf("expected reason token_missing, got %v", body["reason"])
	}
}

func TestBootstrapAllowsSameHostOriginBehindProxy(t *testing.T) {
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"})
	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap", bytes.NewBufferString(`{"username":"admin","password":"local-dev-password"}`))
	req.Host = "192.168.0.8"
	req.Header.Set("Origin", "http://192.168.0.8:8010")
	req.Header.Set("X-Forwarded-Host", "192.168.0.8")
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestBootstrapAllowsConfiguredDevOrigin(t *testing.T) {
	t.Setenv("SANDBOX_CORS_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173")
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"})
	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap", bytes.NewBufferString(`{"username":"admin","password":"local-dev-password"}`))
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthMiddlewareRejectsExpiredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID:   "user-1",
		Username: "tester",
		Role:     roleMember,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "open-sandbox",
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-5 * time.Minute)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		},
	})
	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["reason"] != "token_expired" {
		t.Fatalf("expected reason token_expired, got %v", body["reason"])
	}
}

func TestAuthMiddlewareAcceptsSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID:   "user-1",
		Username: "tester",
		Role:     roleMember,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "open-sandbox",
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
		},
	})
	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: signed})
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddlewareAcceptsAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rawAPIKey := "osk_live_test"
	user := store.UserRecord{User: store.User{ID: "user-1", Username: "tester", Role: roleMember}}
	userStore := &authTestUserStore{
		apiKeys: map[string]store.APIKeyRecord{
			hashTokenValue(rawAPIKey): {ID: "key-1", KeyHash: hashTokenValue(rawAPIKey), UserID: user.ID},
		},
		users: map[string]store.UserRecord{user.ID: user},
	}

	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox", UserStore: userStore}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		identity, ok := authIdentityFromContext(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing identity"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": identity.UserID, "username": identity.Username, "role": identity.Role})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", rawAPIKey)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"user_id":"user-1"`)) {
		t.Fatalf("expected API key identity in response: %s", w.Body.String())
	}
}

func TestAuthMiddlewareFallsBackToBearerWhenAPIKeyInvalid(t *testing.T) {
	gin.SetMode(gin.TestMode)

	userStore := &authTestUserStore{
		apiKeys: map[string]store.APIKeyRecord{},
		users:   map[string]store.UserRecord{},
	}

	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox", UserStore: userStore}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID:   "user-1",
		Username: "tester",
		Role:     roleMember,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "open-sandbox",
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
		},
	})
	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "invalid-key")
	req.Header.Set("Authorization", "Bearer "+signed)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthenticateAPIKeyRequiresUserStore(t *testing.T) {
	auth := AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"}

	_, err := auth.authenticateAPIKey(t.Context(), "key")
	if err == nil {
		t.Fatal("expected error when user store is not configured")
	}
}

func TestAuthenticateAPIKeyRejectsInvalidIdentity(t *testing.T) {
	userStore := &authTestUserStore{
		apiKeys: map[string]store.APIKeyRecord{
			hashTokenValue("key"): {ID: "key-1", KeyHash: hashTokenValue("key"), UserID: "user-1"},
		},
		users: map[string]store.UserRecord{
			"user-1": {User: store.User{ID: "user-1", Username: "tester", Role: "invalid-role"}},
		},
	}

	auth := AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox", UserStore: userStore}
	_, err := auth.authenticateAPIKey(t.Context(), "key")
	if err == nil {
		t.Fatal("expected invalid identity error")
	}
	if errors.Is(err, store.ErrAPIKeyNotFound) {
		t.Fatal("expected identity validation error, got key not found")
	}
}

func TestAuthMiddlewareRejectsQueryToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthConfig{JWTSecret: []byte("jwt-secret"), Issuer: "open-sandbox"}.Middleware())
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID:   "user-1",
		Username: "tester",
		Role:     roleMember,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "open-sandbox",
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
		},
	})
	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected?access_token="+signed, nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["reason"] != "token_missing" {
		t.Fatalf("expected reason token_missing, got %v", body["reason"])
	}
}

func TestLoginEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, RefreshTTL: 24 * time.Hour, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte(`"token"`)) {
		t.Fatalf("expected token in response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"role":"admin"`)) {
		t.Fatalf("expected role in response: %s", w.Body.String())
	}

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected login to set a session cookie")
	}

	sessionCookie := findCookie(cookies, sessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie to be set", sessionCookieName)
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("expected session cookie to be httpOnly")
	}

	refreshCookie := findCookie(cookies, refreshCookieName)
	if refreshCookie == nil {
		t.Fatalf("expected %s cookie to be set", refreshCookieName)
	}
}

func TestLoginEndpointRejectsWrongUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"other","password":"test-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestSessionEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, RefreshTTL: 24 * time.Hour, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	s.Router().ServeHTTP(loginW, loginReq)

	sessionCookie := findCookie(loginW.Result().Cookies(), sessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie to be set", sessionCookieName)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body SessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode session response: %v", err)
	}
	if !body.Authenticated {
		t.Fatal("expected authenticated session response")
	}
	if body.Username != "admin" {
		t.Fatalf("expected username admin, got %q", body.Username)
	}
	if body.Role != roleAdmin {
		t.Fatalf("expected role admin, got %q", body.Role)
	}
	if body.ExpiresAt == 0 {
		t.Fatal("expected session expiry timestamp")
	}
}

func TestLogoutEndpointClearsSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, RefreshTTL: 24 * time.Hour, Issuer: "open-sandbox"})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	sessionCookie := findCookie(w.Result().Cookies(), sessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie to be cleared", sessionCookieName)
	}
	if sessionCookie.MaxAge != -1 {
		t.Fatalf("expected cleared session cookie max-age -1, got %d", sessionCookie.MaxAge)
	}

	refreshCookie := findCookie(w.Result().Cookies(), refreshCookieName)
	if refreshCookie == nil {
		t.Fatalf("expected %s cookie to be cleared", refreshCookieName)
	}
	if refreshCookie.MaxAge != -1 {
		t.Fatalf("expected cleared refresh cookie max-age -1, got %d", refreshCookie.MaxAge)
	}
}

func TestRefreshEndpointRotatesRefreshToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, RefreshTTL: 24 * time.Hour, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	s.Router().ServeHTTP(loginW, loginReq)

	oldRefresh := findCookie(loginW.Result().Cookies(), refreshCookieName)
	if oldRefresh == nil {
		t.Fatalf("expected %s cookie to be set", refreshCookieName)
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshReq.AddCookie(oldRefresh)
	refreshW := httptest.NewRecorder()
	s.Router().ServeHTTP(refreshW, refreshReq)

	if refreshW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", refreshW.Code, refreshW.Body.String())
	}

	newRefresh := findCookie(refreshW.Result().Cookies(), refreshCookieName)
	if newRefresh == nil {
		t.Fatalf("expected rotated %s cookie to be set", refreshCookieName)
	}
	if secureEqual(oldRefresh.Value, newRefresh.Value) {
		t.Fatal("expected refresh token cookie value to rotate")
	}

	reuseReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	reuseReq.AddCookie(oldRefresh)
	reuseW := httptest.NewRecorder()
	s.Router().ServeHTTP(reuseW, reuseReq)

	if reuseW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for reused refresh token, got %d", reuseW.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(reuseW.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode reuse response: %v", err)
	}
	if payload["reason"] != "refresh_token_invalid" {
		t.Fatalf("expected reason refresh_token_invalid, got %v", payload["reason"])
	}
}

func TestRefreshEndpointRejectsExpiredRefreshToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, RefreshTTL: time.Second, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	s.Router().ServeHTTP(loginW, loginReq)

	refreshCookie := findCookie(loginW.Result().Cookies(), refreshCookieName)
	if refreshCookie == nil {
		t.Fatalf("expected %s cookie to be set", refreshCookieName)
	}

	time.Sleep(1200 * time.Millisecond)

	refreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshReq.AddCookie(refreshCookie)
	refreshW := httptest.NewRecorder()
	s.Router().ServeHTTP(refreshW, refreshReq)

	if refreshW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", refreshW.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(refreshW.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode expired response: %v", err)
	}
	if payload["reason"] != "refresh_token_expired" {
		t.Fatalf("expected reason refresh_token_expired, got %v", payload["reason"])
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, RefreshTTL: 24 * time.Hour, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	s.Router().ServeHTTP(loginW, loginReq)

	refreshCookie := findCookie(loginW.Result().Cookies(), refreshCookieName)
	if refreshCookie == nil {
		t.Fatalf("expected %s cookie to be set", refreshCookieName)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutReq.AddCookie(refreshCookie)
	logoutW := httptest.NewRecorder()
	s.Router().ServeHTTP(logoutW, logoutReq)

	if logoutW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", logoutW.Code)
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshReq.AddCookie(refreshCookie)
	refreshW := httptest.NewRecorder()
	s.Router().ServeHTTP(refreshW, refreshReq)

	if refreshW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout revoke, got %d", refreshW.Code)
	}
}

func TestCreateUserEndpoint(t *testing.T) {
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
	admin := seedUser(t, s, "admin", "test-password", roleAdmin)

	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewBufferString(`{"username":"alice","password":"secret-password","role":"member"}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: admin.ID, Username: admin.Username, Role: admin.Role}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"username":"alice"`)) {
		t.Fatalf("expected created user in response: %s", w.Body.String())
	}
}

func TestPersonalAPIKeyLifecycle(t *testing.T) {
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
	member := seedUser(t, s, "alice", "test-password", roleMember)

	createReq := httptest.NewRequest(http.MethodPost, "/api/api-keys", bytes.NewBufferString(`{"name":"local-cli"}`))
	createReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: member.ID, Username: member.Username, Role: member.Role}))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()

	s.Router().ServeHTTP(createW, createReq)

	if createW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", createW.Code, createW.Body.String())
	}

	var created CreateAPIKeyResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create api key response: %v", err)
	}
	if strings.TrimSpace(created.Secret) == "" {
		t.Fatal("expected secret to be returned on create")
	}
	if !strings.HasPrefix(created.Secret, "osk_") {
		t.Fatalf("expected secret prefix osk_, got %q", created.Secret)
	}
	if created.APIKey.ID == "" {
		t.Fatal("expected api key id in create response")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/api-keys", nil)
	listReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: member.ID, Username: member.Username, Role: member.Role}))
	listW := httptest.NewRecorder()

	s.Router().ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if bytes.Contains(listW.Body.Bytes(), []byte("secret")) {
		t.Fatalf("did not expect secret in list response: %s", listW.Body.String())
	}

	var listed []APIKeyResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &listed); err != nil {
		t.Fatalf("failed to decode list api keys response: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one listed key, got %d", len(listed))
	}
	if listed[0].ID != created.APIKey.ID {
		t.Fatalf("expected listed key id %q, got %q", created.APIKey.ID, listed[0].ID)
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/api-keys/"+created.APIKey.ID, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: member.ID, Username: member.Username, Role: member.Role}))
	revokeW := httptest.NewRecorder()

	s.Router().ServeHTTP(revokeW, revokeReq)

	if revokeW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", revokeW.Code, revokeW.Body.String())
	}

	listAfterReq := httptest.NewRequest(http.MethodGet, "/api/api-keys", nil)
	listAfterReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: member.ID, Username: member.Username, Role: member.Role}))
	listAfterW := httptest.NewRecorder()

	s.Router().ServeHTTP(listAfterW, listAfterReq)

	if listAfterW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listAfterW.Code, listAfterW.Body.String())
	}
	if !bytes.Equal(bytes.TrimSpace(listAfterW.Body.Bytes()), []byte("[]")) {
		t.Fatalf("expected empty list after revoke, got %s", listAfterW.Body.String())
	}
}

func TestPersonalAPIKeyOwnershipEnforced(t *testing.T) {
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
	owner := seedUser(t, s, "owner", "test-password", roleMember)
	other := seedUser(t, s, "other", "test-password", roleMember)

	createReq := httptest.NewRequest(http.MethodPost, "/api/api-keys", bytes.NewBufferString(`{"name":"shared"}`))
	createReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: owner.ID, Username: owner.Username, Role: owner.Role}))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	s.Router().ServeHTTP(createW, createReq)

	if createW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", createW.Code, createW.Body.String())
	}

	var created CreateAPIKeyResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create api key response: %v", err)
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/api-keys/"+created.APIKey.ID, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: other.ID, Username: other.Username, Role: other.Role}))
	revokeW := httptest.NewRecorder()
	s.Router().ServeHTTP(revokeW, revokeReq)

	if revokeW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when revoking another user's key, got %d (%s)", revokeW.Code, revokeW.Body.String())
	}
}

func TestListUsersEndpointRejectsNonAdmin(t *testing.T) {
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestBootstrapEndpointCreatesInitialAdmin(t *testing.T) {
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})

	req := httptest.NewRequest(http.MethodPost, "/auth/bootstrap", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"role":"admin"`)) {
		t.Fatalf("expected bootstrap to create admin: %s", w.Body.String())
	}
}

func TestLoadAuthConfigFromEnv(t *testing.T) {
	t.Setenv("SANDBOX_JWT_SECRET", "secret")
	t.Setenv("SANDBOX_JWT_TTL", "30m")
	t.Setenv("SANDBOX_JWT_ISSUER", "custom-issuer")

	auth, err := LoadAuthConfigFromEnv()
	if err != nil {
		t.Fatalf("expected auth config to load: %v", err)
	}

	if string(auth.JWTSecret) != "secret" {
		t.Fatalf("unexpected auth config values: %+v", auth)
	}
	if auth.TokenTTL != 30*time.Minute {
		t.Fatalf("expected token ttl 30m, got %s", auth.TokenTTL)
	}
	if auth.RefreshTTL != 30*24*time.Hour {
		t.Fatalf("expected default refresh ttl 30d, got %s", auth.RefreshTTL)
	}
	if auth.Issuer != "custom-issuer" {
		t.Fatalf("expected custom issuer, got %q", auth.Issuer)
	}
}

func TestLoadAuthConfigFromEnvRefreshTTL(t *testing.T) {
	t.Setenv("SANDBOX_JWT_SECRET", "secret")
	t.Setenv("SANDBOX_REFRESH_TTL", "168h")

	auth, err := LoadAuthConfigFromEnv()
	if err != nil {
		t.Fatalf("expected auth config to load: %v", err)
	}

	if auth.RefreshTTL != 168*time.Hour {
		t.Fatalf("expected refresh ttl 168h, got %s", auth.RefreshTTL)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}

func TestLoadAuthConfigFromEnvRequiresJWTSecret(t *testing.T) {
	t.Setenv("SANDBOX_JWT_SECRET", "")

	if _, err := LoadAuthConfigFromEnv(); err == nil {
		t.Fatal("expected auth config loading to fail without env vars")
	}
}
