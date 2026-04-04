package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "open_sandbox_session"
	roleAdmin         = "admin"
	roleMember        = "member"
)

type AuthConfig struct {
	JWTSecret []byte
	TokenTTL  time.Duration
	Issuer    string
}

type AuthIdentity struct {
	UserID   string
	Username string
	Role     string
}

func (a AuthIdentity) IsAdmin() bool {
	return a.Role == roleAdmin
}

type authClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func LoadAuthConfigFromEnv() (AuthConfig, error) {
	jwtSecret := strings.TrimSpace(os.Getenv("SANDBOX_JWT_SECRET"))

	ttl := 15 * time.Minute
	ttlRaw := strings.TrimSpace(os.Getenv("SANDBOX_JWT_TTL"))
	if ttlRaw != "" {
		parsedTTL, err := time.ParseDuration(ttlRaw)
		if err != nil {
			return AuthConfig{}, errors.New("invalid SANDBOX_JWT_TTL: must be a Go duration like 15m or 1h")
		}
		if parsedTTL <= 0 {
			return AuthConfig{}, errors.New("invalid SANDBOX_JWT_TTL: must be greater than zero")
		}
		ttl = parsedTTL
	}

	issuer := strings.TrimSpace(os.Getenv("SANDBOX_JWT_ISSUER"))
	if issuer == "" {
		issuer = "open-sandbox"
	}

	if jwtSecret == "" {
		return AuthConfig{}, errors.New("auth is not configured: set SANDBOX_JWT_SECRET")
	}

	return AuthConfig{JWTSecret: []byte(jwtSecret), TokenTTL: ttl, Issuer: issuer}, nil
}

func (a AuthConfig) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, claims, reason, err := a.authenticateRequest(c.Request)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized", Reason: reason})
			return
		}

		c.Set("auth.identity", identity)
		c.Set("auth.claims", claims)
		c.Next()
	}
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type SetupStatusResponse struct {
	BootstrapRequired bool `json:"bootstrap_required"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	ExpiresAt int64  `json:"expires_at"`
}

type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"user_id"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	ExpiresAt     int64  `json:"expires_at"`
}

func (s *Server) login(c *gin.Context) {
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	bootstrapRequired, err := s.bootstrapRequired(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if bootstrapRequired {
		writeErrorWithDetails(c, http.StatusConflict, "bootstrap required", "bootstrap_required", "create the first admin account before signing in")
		return
	}

	var req loginRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	user, err := s.userStore.GetUserByUsername(c.Request.Context(), req.Username)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "invalid credentials", Reason: "invalid_credentials"})
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "invalid credentials", Reason: "invalid_credentials"})
		return
	}

	identity := AuthIdentity{UserID: user.ID, Username: user.Username, Role: user.Role}
	signed, expiresAt, err := s.issueAuthToken(identity)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	writeSessionCookie(c, signed, expiresAt)
	c.JSON(http.StatusOK, LoginResponse{
		Token:     signed,
		TokenType: "Bearer",
		UserID:    identity.UserID,
		Username:  identity.Username,
		Role:      identity.Role,
		ExpiresAt: expiresAt.Unix(),
	})
}

func (s *Server) setupStatus(c *gin.Context) {
	bootstrapRequired, err := s.bootstrapRequired(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, SetupStatusResponse{BootstrapRequired: bootstrapRequired})
}

func (s *Server) bootstrap(c *gin.Context) {
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	bootstrapRequired, err := s.bootstrapRequired(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if !bootstrapRequired {
		writeErrorWithDetails(c, http.StatusConflict, "bootstrap already completed", "bootstrap_not_allowed", "users already exist")
		return
	}

	var req loginRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	created, err := s.userStore.CreateUser(c.Request.Context(), store.UserRecord{
		User: store.User{
			ID:       newRequestID(),
			Username: req.Username,
			Role:     roleAdmin,
		},
		PasswordHash: passwordHash,
	})
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			writeError(c, http.StatusConflict, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	identity := AuthIdentity{UserID: created.ID, Username: created.Username, Role: created.Role}
	signed, expiresAt, err := s.issueAuthToken(identity)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	writeSessionCookie(c, signed, expiresAt)
	c.JSON(http.StatusOK, LoginResponse{
		Token:     signed,
		TokenType: "Bearer",
		UserID:    identity.UserID,
		Username:  identity.Username,
		Role:      identity.Role,
		ExpiresAt: expiresAt.Unix(),
	})
}

func (s *Server) session(c *gin.Context) {
	identity, claims, reason, err := s.auth.authenticateRequest(c.Request)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized", Reason: reason})
		return
	}

	expiresAt := int64(0)
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time.Unix()
	}

	c.JSON(http.StatusOK, SessionResponse{
		Authenticated: true,
		UserID:        identity.UserID,
		Username:      identity.Username,
		Role:          identity.Role,
		ExpiresAt:     expiresAt,
	})
}

func (s *Server) logout(c *gin.Context) {
	clearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"signed_out": true})
}

func (s *Server) bootstrapRequired(ctx context.Context) (bool, error) {
	if s.userStore == nil {
		return false, errors.New("user store is not configured")
	}
	hasUsers, err := s.userStore.HasUsers(ctx)
	if err != nil {
		return false, err
	}
	return !hasUsers, nil
}

func (s *Server) issueAuthToken(identity AuthIdentity) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.auth.TokenTTL)
	claims := authClaims{
		UserID:   identity.UserID,
		Username: identity.Username,
		Role:     identity.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.auth.Issuer,
			Subject:   identity.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.auth.JWTSecret)
	if err != nil {
		return "", time.Time{}, err
	}

	return signed, expiresAt, nil
}

func (a AuthConfig) authenticateRequest(r *http.Request) (AuthIdentity, *authClaims, string, error) {
	bearerToken := extractRequestToken(r)
	if bearerToken == "" {
		return AuthIdentity{}, nil, "token_missing", errors.New("token missing")
	}

	if len(a.JWTSecret) == 0 {
		return AuthIdentity{}, nil, "token_invalid", errors.New("jwt secret is not configured")
	}

	claims := &authClaims{}
	parsed, err := jwt.ParseWithClaims(bearerToken, claims, func(token *jwt.Token) (any, error) {
		return a.JWTSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(a.Issuer))
	if err == nil && parsed != nil && parsed.Valid {
		identity := AuthIdentity{UserID: claims.UserID, Username: claims.Username, Role: claims.Role}
		if identity.UserID == "" || identity.Username == "" || !isValidRole(identity.Role) {
			return AuthIdentity{}, nil, "token_invalid", errors.New("token is missing user identity")
		}
		return identity, claims, "", nil
	}

	reason := "token_invalid"
	if errors.Is(err, jwt.ErrTokenExpired) {
		reason = "token_expired"
	}

	return AuthIdentity{}, nil, reason, err
}

func authIdentityFromContext(c *gin.Context) (AuthIdentity, bool) {
	raw, ok := c.Get("auth.identity")
	if !ok {
		return AuthIdentity{}, false
	}
	identity, ok := raw.(AuthIdentity)
	return identity, ok
}

func requireAdmin(c *gin.Context) (AuthIdentity, bool) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return AuthIdentity{}, false
	}
	if !identity.IsAdmin() {
		writeError(c, http.StatusForbidden, errors.New("admin access is required"))
		return AuthIdentity{}, false
	}
	return identity, true
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func normalizeRole(role string) string {
	trimmed := strings.ToLower(strings.TrimSpace(role))
	if trimmed == "" {
		return roleMember
	}
	return trimmed
}

func isValidRole(role string) bool {
	switch normalizeRole(role) {
	case roleAdmin, roleMember:
		return true
	default:
		return false
	}
}

func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(strings.TrimSpace(authHeader), " ", 2)
	if len(parts) != 2 {
		return ""
	}

	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

func extractRequestToken(r *http.Request) string {
	if authorization := strings.TrimSpace(r.Header.Get("Authorization")); authorization != "" {
		if token := extractBearerToken(authorization); token != "" {
			return token
		}
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if token := strings.TrimSpace(cookie.Value); token != "" {
			return token
		}
	}

	return ""
}

func writeSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(c.Request),
		Expires:  expiresAt,
		MaxAge:   max(1, int(time.Until(expiresAt).Seconds())),
	})
}

func clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(c.Request),
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}

func secureEqual(a string, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
