package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

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
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
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

	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie to be set", sessionCookieName)
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("expected session cookie to be httpOnly")
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
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
	seedUser(t, s, "admin", "test-password", roleAdmin)

	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(`{"username":"admin","password":"test-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	s.Router().ServeHTTP(loginW, loginReq)

	var sessionCookie *http.Cookie
	for _, cookie := range loginW.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			break
		}
	}
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
	s := newAuthServer(t, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie to be cleared", sessionCookieName)
	}
	if sessionCookie.MaxAge != -1 {
		t.Fatalf("expected cleared session cookie max-age -1, got %d", sessionCookie.MaxAge)
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
	if auth.Issuer != "custom-issuer" {
		t.Fatalf("expected custom issuer, got %q", auth.Issuer)
	}
}

func TestLoadAuthConfigFromEnvRequiresJWTSecret(t *testing.T) {
	t.Setenv("SANDBOX_JWT_SECRET", "")

	if _, err := LoadAuthConfigFromEnv(); err == nil {
		t.Fatal("expected auth config loading to fail without env vars")
	}
}
