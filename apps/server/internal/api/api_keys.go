package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

type APIKeyResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Preview   string `json:"preview,omitempty"`
	CreatedAt int64  `json:"created_at"`
	RevokedAt int64  `json:"revoked_at,omitempty"`
}

type CreateAPIKeyRequest struct {
	Name string `json:"name"`
}

type CreateAPIKeyResponse struct {
	APIKey APIKeyResponse `json:"api_key"`
	Secret string         `json:"secret"`
}

// listAPIKeys godoc
// @Summary List personal API keys
// @Description Returns the current user's active personal API keys.
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Security APIKeyAuth
// @Success 200 {array} APIKeyResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/api-keys [get]
func (s *Server) listAPIKeys(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	keys, err := s.userStore.ListAPIKeysByUser(c.Request.Context(), identity.UserID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	out := make([]APIKeyResponse, 0, len(keys))
	for _, key := range keys {
		out = append(out, apiKeyToResponse(key))
	}

	c.JSON(http.StatusOK, out)
}

// createAPIKey godoc
// @Summary Create personal API key
// @Description Creates an API key for the current user and returns the plaintext secret once.
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security APIKeyAuth
// @Param payload body CreateAPIKeyRequest false "API key payload"
// @Success 200 {object} CreateAPIKeyResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/api-keys [post]
func (s *Server) createAPIKey(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	var req CreateAPIKeyRequest
	if c.Request.Body != nil {
		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(string(rawBody)) != "" {
			if err := json.Unmarshal(rawBody, &req); err != nil {
				writeError(c, http.StatusBadRequest, err)
				return
			}
		}
	}

	secretSuffix, err := newOpaqueToken()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	secret := "osk_" + secretSuffix
	now := time.Now().Unix()
	key := store.APIKeyRecord{
		ID:        newRequestID(),
		Name:      strings.TrimSpace(req.Name),
		Preview:   apiKeyPreview(secret),
		KeyHash:   hashTokenValue(secret),
		UserID:    identity.UserID,
		CreatedAt: now,
	}
	if err := s.userStore.CreateAPIKey(c.Request.Context(), key); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, CreateAPIKeyResponse{APIKey: apiKeyToResponse(key), Secret: secret})
}

// revokeAPIKey godoc
// @Summary Revoke personal API key
// @Description Revokes one API key owned by the current user.
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Security APIKeyAuth
// @Param id path string true "API key ID"
// @Success 200 {object} map[string]any
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/api-keys/{id} [delete]
func (s *Server) revokeAPIKey(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	keyID := strings.TrimSpace(c.Param("id"))
	if keyID == "" {
		writeError(c, http.StatusBadRequest, errors.New("api key id is required"))
		return
	}

	if err := s.userStore.RevokeAPIKey(c.Request.Context(), keyID, identity.UserID, time.Now().Unix()); err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			writeError(c, http.StatusNotFound, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": keyID, "revoked": true})
}

func apiKeyToResponse(key store.APIKeyRecord) APIKeyResponse {
	return APIKeyResponse{
		ID:        key.ID,
		Name:      strings.TrimSpace(key.Name),
		Preview:   strings.TrimSpace(key.Preview),
		CreatedAt: key.CreatedAt,
		RevokedAt: key.RevokedAt,
	}
}

func apiKeyPreview(secret string) string {
	trimmed := strings.TrimSpace(secret)
	if len(trimmed) <= 12 {
		return trimmed
	}
	return trimmed[:8] + "..." + trimmed[len(trimmed)-4:]
}
