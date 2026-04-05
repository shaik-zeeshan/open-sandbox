package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

const workerAuthHeader = "X-Open-Sandbox-Worker-Token"

type workerRegistrationRequest struct {
	WorkerID            string            `json:"worker_id"`
	Name                string            `json:"name" binding:"required"`
	AdvertiseAddress    string            `json:"advertise_address"`
	ExecutionMode       string            `json:"execution_mode"`
	Version             string            `json:"version"`
	HeartbeatTTLSeconds int64             `json:"heartbeat_ttl_seconds"`
	Labels              map[string]string `json:"labels"`
}

type workerHeartbeatRequest struct {
	Status           string            `json:"status"`
	AdvertiseAddress string            `json:"advertise_address"`
	Version          string            `json:"version"`
	Labels           map[string]string `json:"labels"`
}

type workerResponse struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	AdvertiseAddress    string            `json:"advertise_address,omitempty"`
	ExecutionMode       string            `json:"execution_mode"`
	Status              string            `json:"status"`
	Version             string            `json:"version,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	RegisteredAt        int64             `json:"registered_at"`
	LastHeartbeatAt     int64             `json:"last_heartbeat_at"`
	HeartbeatTTLSeconds int64             `json:"heartbeat_ttl_seconds"`
	UpdatedAt           int64             `json:"updated_at"`
	ControlPlaneOwned   bool              `json:"control_plane_owned"`
	ExecutionReachable  bool              `json:"execution_reachable"`
}

func (s *Server) ensureLocalWorkerRegistration(ctx context.Context) {
	if s.workerStore == nil {
		return
	}
	hostname, _ := os.Hostname()
	now := time.Now().Unix()
	_ = s.workerStore.UpsertRuntimeWorker(ctx, store.RuntimeWorker{
		ID:                  localRuntimeWorkerID,
		Name:                hostnameOrFallback(hostname, "local"),
		ExecutionMode:       "docker",
		Status:              "active",
		Version:             "control-plane-local",
		Labels:              map[string]string{"topology": "single-server", "docker": "local"},
		RegisteredAt:        now,
		LastHeartbeatAt:     now,
		HeartbeatTTLSeconds: 30,
		UpdatedAt:           now,
	})
}

func hostnameOrFallback(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func normalizeManagedWorkerID(workerID string) string {
	trimmed := strings.TrimSpace(workerID)
	if trimmed == "" {
		return localRuntimeWorkerID
	}
	return trimmed
}

func (s *Server) workerIDForSandbox(sandbox store.Sandbox) string {
	return normalizeManagedWorkerID(sandbox.WorkerID)
}

func (s *Server) workerIDForContainerSummary(item ContainerSummary) string {
	if workerID := strings.TrimSpace(item.WorkerID); workerID != "" {
		return workerID
	}
	if workerID := strings.TrimSpace(item.Labels[labelOpenSandboxWorkerID]); workerID != "" {
		return workerID
	}
	return localRuntimeWorkerID
}

func (s *Server) registerWorker(c *gin.Context) {
	if s.workerStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("worker store is not configured"))
		return
	}

	var req workerRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	workerID := normalizeManagedWorkerID(req.WorkerID)
	now := time.Now().Unix()
	worker := store.RuntimeWorker{
		ID:                  workerID,
		Name:                strings.TrimSpace(req.Name),
		AdvertiseAddress:    strings.TrimSpace(req.AdvertiseAddress),
		ExecutionMode:       strings.TrimSpace(req.ExecutionMode),
		Status:              "active",
		Version:             strings.TrimSpace(req.Version),
		Labels:              req.Labels,
		RegisteredAt:        now,
		LastHeartbeatAt:     now,
		HeartbeatTTLSeconds: req.HeartbeatTTLSeconds,
		UpdatedAt:           now,
	}
	if err := s.workerStore.UpsertRuntimeWorker(c.Request.Context(), worker); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	stored, err := s.workerStore.GetRuntimeWorker(c.Request.Context(), workerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, workerToResponse(stored, workerID == localRuntimeWorkerID, workerID == localRuntimeWorkerID))
}

func (s *Server) heartbeatWorker(c *gin.Context) {
	if s.workerStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("worker store is not configured"))
		return
	}

	workerID := normalizeManagedWorkerID(c.Param("id"))
	var req workerHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	if err := s.workerStore.TouchRuntimeWorkerHeartbeat(c.Request.Context(), workerID, time.Now().Unix(), req.Status, req.AdvertiseAddress, req.Version, req.Labels); err != nil {
		if errors.Is(err, store.ErrRuntimeWorkerNotFound) {
			writeError(c, http.StatusNotFound, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	stored, err := s.workerStore.GetRuntimeWorker(c.Request.Context(), workerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, workerToResponse(stored, workerID == localRuntimeWorkerID, workerID == localRuntimeWorkerID))
}

func (s *Server) listWorkers(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	if !identity.IsAdmin() {
		writeError(c, http.StatusForbidden, errors.New("admin access required"))
		return
	}
	if s.workerStore == nil {
		c.JSON(http.StatusOK, []workerResponse{workerToResponse(store.RuntimeWorker{ID: localRuntimeWorkerID, Name: "local", ExecutionMode: "docker", Status: "active"}, true, true)})
		return
	}

	workers, err := s.workerStore.ListRuntimeWorkers(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	out := make([]workerResponse, 0, len(workers))
	for _, worker := range workers {
		workerID := normalizeManagedWorkerID(worker.ID)
		out = append(out, workerToResponse(worker, workerID == localRuntimeWorkerID, workerID == localRuntimeWorkerID))
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) workerAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		secret := strings.TrimSpace(os.Getenv("SANDBOX_WORKER_SHARED_SECRET"))
		if secret == "" {
			c.AbortWithStatusJSON(http.StatusNotFound, ErrorResponse{Error: "worker control plane is disabled", Reason: "worker_shared_secret_missing"})
			return
		}
		provided := strings.TrimSpace(c.GetHeader(workerAuthHeader))
		if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{Error: "unauthorized", Reason: "worker_token_invalid"})
			return
		}
		c.Next()
	}
}

func workerToResponse(worker store.RuntimeWorker, controlPlaneOwned bool, executionReachable bool) workerResponse {
	return workerResponse{
		ID:                  normalizeManagedWorkerID(worker.ID),
		Name:                worker.Name,
		AdvertiseAddress:    worker.AdvertiseAddress,
		ExecutionMode:       worker.ExecutionMode,
		Status:              worker.Status,
		Version:             worker.Version,
		Labels:              worker.Labels,
		RegisteredAt:        worker.RegisteredAt,
		LastHeartbeatAt:     worker.LastHeartbeatAt,
		HeartbeatTTLSeconds: worker.HeartbeatTTLSeconds,
		UpdatedAt:           worker.UpdatedAt,
		ControlPlaneOwned:   controlPlaneOwned,
		ExecutionReachable:  executionReachable,
	}
}
