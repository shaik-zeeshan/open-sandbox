package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

type UserResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
}

type updateUserPasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

func (s *Server) listUsers(c *gin.Context) {
	if _, ok := requireAdmin(c); !ok {
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	users, err := s.userStore.ListUsers(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	out := make([]UserResponse, 0, len(users))
	for _, user := range users {
		out = append(out, userToResponse(user))
	}

	c.JSON(http.StatusOK, out)
}

func (s *Server) createUser(c *gin.Context) {
	if _, ok := requireAdmin(c); !ok {
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeError(c, http.StatusBadRequest, errors.New("username is required"))
		return
	}
	role := normalizeRole(req.Role)
	if !isValidRole(role) {
		writeError(c, http.StatusBadRequest, errors.New("role must be admin or member"))
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
			Username: username,
			Role:     role,
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

	c.JSON(http.StatusOK, userToResponse(created))
}

func (s *Server) updateUserPassword(c *gin.Context) {
	if _, ok := requireAdmin(c); !ok {
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	userID := strings.TrimSpace(c.Param("id"))
	if userID == "" {
		writeError(c, http.StatusBadRequest, errors.New("user id is required"))
		return
	}

	var req updateUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if err := s.userStore.UpdateUserPasswordHash(c.Request.Context(), userID, passwordHash); err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			writeError(c, http.StatusNotFound, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": userID, "updated": true})
}

func (s *Server) deleteUser(c *gin.Context) {
	identity, ok := requireAdmin(c)
	if !ok {
		return
	}
	if s.userStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("user store is not configured"))
		return
	}

	userID := strings.TrimSpace(c.Param("id"))
	if userID == "" {
		writeError(c, http.StatusBadRequest, errors.New("user id is required"))
		return
	}
	if userID == identity.UserID {
		writeError(c, http.StatusBadRequest, errors.New("cannot delete the current user"))
		return
	}

	if s.sandboxStore != nil {
		sandboxes, err := s.sandboxStore.ListSandboxes(c.Request.Context())
		if err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
		for _, sandbox := range sandboxes {
			if sandbox.OwnerID == userID {
				writeError(c, http.StatusConflict, errors.New("cannot delete a user that still owns sandboxes"))
				return
			}
		}
	}

	if err := s.userStore.DeleteUser(c.Request.Context(), userID); err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			writeError(c, http.StatusNotFound, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": userID, "deleted": true})
}

func userToResponse(user store.User) UserResponse {
	return UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}
