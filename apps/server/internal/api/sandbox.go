package api

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/gin-gonic/gin"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
)

type ContainerSummary struct {
	ID           string            `json:"id"`
	ContainerID  string            `json:"container_id"`
	WorkerID     string            `json:"worker_id,omitempty"`
	Names        []string          `json:"names"`
	Image        string            `json:"image"`
	State        string            `json:"state"`
	Status       string            `json:"status"`
	Created      int64             `json:"created"`
	Labels       map[string]string `json:"labels"`
	WorkloadKind string            `json:"workload_kind,omitempty"`
	ProjectName  string            `json:"project_name,omitempty"`
	ServiceName  string            `json:"service_name,omitempty"`
	Resettable   bool              `json:"resettable"`
	PortSpecs    []string          `json:"port_specs,omitempty"`
	Ports        []PortSummary     `json:"ports,omitempty"`
	PreviewURLs  []PreviewURL      `json:"preview_urls,omitempty"`
}

type PreviewURL struct {
	PrivatePort int    `json:"private_port"`
	URL         string `json:"url"`
}

type PortSummary struct {
	Private int    `json:"private"`
	Public  int    `json:"public,omitempty"`
	Type    string `json:"type"`
	IP      string `json:"ip,omitempty"`
}

type SandboxResponse struct {
	ID            string                             `json:"id"`
	Name          string                             `json:"name"`
	Image         string                             `json:"image"`
	ContainerID   string                             `json:"container_id"`
	WorkerID      string                             `json:"worker_id,omitempty"`
	WorkspaceDir  string                             `json:"workspace_dir"`
	RepoURL       string                             `json:"repo_url,omitempty"`
	Env           []string                           `json:"env,omitempty"`
	SecretEnvKeys []string                           `json:"secret_env_keys,omitempty"`
	Status        string                             `json:"status"`
	OwnerUsername string                             `json:"owner_username,omitempty"`
	ProxyConfig   map[string]*SandboxPortProxyConfig `json:"proxy_config,omitempty"`
	PortSpecs     []string                           `json:"port_specs,omitempty"`
	Ports         []PortSummary                      `json:"ports,omitempty"`
	PreviewURLs   []PreviewURL                       `json:"preview_urls,omitempty"`
	CreatedAt     int64                              `json:"created_at"`
	UpdatedAt     int64                              `json:"updated_at"`
}

type UpdateSandboxProxyConfigRequest struct {
	ProxyConfig map[string]*SandboxPortProxyConfig `json:"proxy_config"`
}

type UpdateSandboxEnvRequest struct {
	Env                 []string `json:"env"`
	SecretEnv           []string `json:"secret_env,omitempty"`
	RemoveSecretEnvKeys []string `json:"remove_secret_env_keys,omitempty"`
}

type ResetSandboxRequest struct{}

type CreateSandboxRequest struct {
	Name               string                             `json:"name" binding:"required"`
	Image              string                             `json:"image" binding:"required"`
	RepoURL            string                             `json:"repo_url"`
	Branch             string                             `json:"branch"`
	SingleBranch       bool                               `json:"single_branch"`
	Depth              *int                               `json:"depth"`
	Filter             string                             `json:"filter"`
	BaseCommit         string                             `json:"base_commit"`
	RepoTargetPath     string                             `json:"repo_target_path"`
	UseImageDefaultCmd bool                               `json:"use_image_default_cmd"`
	Env                []string                           `json:"env"`
	SecretEnv          []string                           `json:"secret_env,omitempty"`
	Cmd                []string                           `json:"cmd"`
	Workdir            string                             `json:"workdir"`
	TTY                bool                               `json:"tty"`
	User               string                             `json:"user"`
	Ports              []string                           `json:"ports"`
	ProxyConfig        map[string]*SandboxPortProxyConfig `json:"proxy_config,omitempty"`
}

const sandboxGitCacheMountRoot = "/.open-sandbox/git-cache"

type sandboxProgressReporter struct {
	c       *gin.Context
	mu      *sync.Mutex
	enabled bool
}

func (r sandboxProgressReporter) writeError(status int, err error) {
	if !r.enabled {
		if r.c == nil {
			return
		}
		writeError(r.c, status, err)
		return
	}
	r.emit("error", ErrorResponse{Error: err.Error(), Status: status})
}

func (r sandboxProgressReporter) writeErrorWithDetails(status int, message string, reason string, stderr string) {
	if !r.enabled {
		if r.c == nil {
			return
		}
		writeErrorWithDetails(r.c, status, message, reason, stderr)
		return
	}
	payload := ErrorResponse{Error: message, Status: status}
	if strings.TrimSpace(reason) != "" {
		payload.Reason = strings.TrimSpace(reason)
	}
	if strings.TrimSpace(stderr) != "" {
		payload.Stderr = strings.TrimSpace(stderr)
	}
	r.emit("error", payload)
}

func (r sandboxProgressReporter) emit(event string, payload any) {
	if !r.enabled {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		emitSSE(r.c, r.mu, "error", err.Error())
		return
	}
	emitSSE(r.c, r.mu, event, string(data))
}

func (r sandboxProgressReporter) phase(phase string, status string, message string) {
	r.emit("progress", gin.H{"phase": phase, "status": status, "message": message})
}

// SandboxPortProxyConfig holds proxy customization for a single preview port.
// The map key in CreateSandboxRequest.ProxyConfig is the string representation
// of the private port number (e.g. "3000").
type SandboxPortProxyConfig struct {
	RequestHeaders  map[string]string      `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string      `json:"response_headers,omitempty"`
	CORS            *SandboxPortCORSConfig `json:"cors,omitempty"`
	PathPrefixStrip string                 `json:"path_prefix_strip,omitempty"`
	SkipAuth        bool                   `json:"skip_auth,omitempty"`
}

// SandboxPortCORSConfig holds CORS settings for a sandbox preview port.
type SandboxPortCORSConfig struct {
	AllowOrigins     []string `json:"allow_origins,omitempty"`
	AllowMethods     []string `json:"allow_methods,omitempty"`
	AllowHeaders     []string `json:"allow_headers,omitempty"`
	AllowCredentials bool     `json:"allow_credentials,omitempty"`
	MaxAge           int      `json:"max_age,omitempty"`
}

type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
}

type FileReadResponse struct {
	Path    string      `json:"path"`
	Name    string      `json:"name"`
	Kind    string      `json:"kind"`
	Content string      `json:"content,omitempty"`
	Entries []FileEntry `json:"entries,omitempty"`
}

type SaveFileRequest struct {
	TargetPath string `json:"target_path" binding:"required"`
	Content    string `json:"content"`
}

type dockerContainerCLIRecord struct {
	ID     string `json:"ID"`
	Image  string `json:"Image"`
	Names  string `json:"Names"`
	Ports  string `json:"Ports"`
	Size   string `json:"Size"`
	Status string `json:"Status"`
	Labels string `json:"Labels"`
}

const defaultSandboxWorkspaceDir = "/workspace"

func (s *Server) listContainers(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	containers, err := s.runtime.ListWorkloads(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if len(containers) > 0 {
		s.reconcileContainerArtifacts(containerIndexByContainerID(containers))
	} else {
		s.reconcileContainerArtifacts(map[string]ContainerSummary{})
	}
	managedComposeProjects, err := s.managedComposeProjects()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	containers = s.filterManagedRuntimeContainers(containers, managedComposeProjects)
	ownedContainerIDs, err := s.ownedRuntimeContainerIDs(c.Request.Context(), identity.UserID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	ownedComposeProjects, err := s.ownedComposeProjects(identity.UserID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	visible := make([]ContainerSummary, 0, len(containers))
	for _, item := range containers {
		if s.runtimeContainerVisibleToIdentity(item, identity, ownedContainerIDs, ownedComposeProjects) {
			item.PortSpecs = s.portSpecsForContainer(item)
			item.PreviewURLs = s.previewURLsForContainer(item)
			visible = append(visible, item)
		}
	}

	c.JSON(http.StatusOK, visible)
}

func (s *Server) restartContainer(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	workerID := s.workerIDForContainerSummary(target)
	inspect, err := s.runtime.InspectWorkload(c.Request.Context(), workerID, target.ContainerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if inspect.State != nil && inspect.State.Running {
		if err := s.runtime.StopWorkload(c.Request.Context(), workerID, target.ContainerID, container.StopOptions{}); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := s.runtime.StartWorkload(c.Request.Context(), workerID, target.ContainerID, container.StartOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if s.sandboxStore != nil {
		_ = s.sandboxStore.UpdateSandboxStatusByContainerID(c.Request.Context(), target.ContainerID, "running")
	}

	c.JSON(http.StatusOK, gin.H{"id": target.ID, "container_id": target.ContainerID, "restarted": true})
}

func (s *Server) resetContainer(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	if sandboxID := strings.TrimSpace(target.Labels[labelOpenSandboxSandboxID]); sandboxID != "" {
		writeError(c, http.StatusBadRequest, errors.New("use the sandbox reset endpoint for managed sandboxes"))
		return
	}

	workerID := s.workerIDForContainerSummary(target)
	if result, handled, err := s.runtime.ResetWorkload(c.Request.Context(), workerID, target); handled {
		if err != nil {
			writeErrorWithDetails(c, http.StatusInternalServerError, "compose reset failed", "command_failed", strings.TrimSpace(result.Stderr))
			return
		}
		s.syncTraefikRoutes(c.Request.Context())

		c.JSON(http.StatusOK, gin.H{"id": result.WorkloadID, "container_id": result.ContainerID, "reset": true, "stdout": result.Stdout, "stderr": result.Stderr})
		return
	}

	managedID := strings.TrimSpace(target.Labels[labelOpenSandboxManagedID])
	if managedID == "" || strings.TrimSpace(target.Labels[labelOpenSandboxKind]) != managedKindDirect {
		writeError(c, http.StatusBadRequest, errors.New("reset is only available for managed direct containers and compose workloads"))
		return
	}

	createReq, err := s.readDirectContainerSpec(managedID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if createReq.Name == "" && len(target.Names) > 0 {
		createReq.Name = target.Names[0]
		if err := s.writeDirectContainerSpec(managedID, createReq); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}

	containerConfig, hostConfig, err := buildDirectContainerConfigs(
		createReq,
		strings.TrimSpace(target.Labels[labelOpenSandboxOwnerID]),
		strings.TrimSpace(target.Labels[labelOpenSandboxOwnerUsername]),
		managedID,
		workerID,
	)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	s.runtimeLimits.apply(hostConfig)
	if err := s.runtime.RemoveWorkload(c.Request.Context(), workerID, target.ContainerID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	created, err := s.createContainerWithAutoPull(c.Request.Context(), workerID, createReq.Image, containerConfig, hostConfig, createReq.Name)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if createReq.Start {
		if err := s.runtime.StartWorkload(c.Request.Context(), workerID, created.ID, container.StartOptions{}); err != nil {
			writeError(c, http.StatusInternalServerError, fmt.Errorf("start container: %w", err))
			return
		}
	}
	s.syncTraefikRoutes(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{"id": target.ID, "container_id": created.ID, "reset": true})
}

func (s *Server) stopContainer(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	if err := s.runtime.StopWorkload(c.Request.Context(), s.workerIDForContainerSummary(target), target.ContainerID, container.StopOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if s.sandboxStore != nil {
		_ = s.sandboxStore.UpdateSandboxStatusByContainerID(c.Request.Context(), target.ContainerID, "stopped")
	}

	c.JSON(http.StatusOK, gin.H{"id": target.ID, "container_id": target.ContainerID, "stopped": true})
}

func (s *Server) removeContainer(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	force, _ := strconv.ParseBool(c.DefaultQuery("force", "true"))
	if err := s.runtime.RemoveWorkload(c.Request.Context(), s.workerIDForContainerSummary(target), target.ContainerID, container.RemoveOptions{Force: force, RemoveVolumes: true}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(target.Labels[labelOpenSandboxKind]) == managedKindDirect {
		managedID := strings.TrimSpace(target.Labels[labelOpenSandboxManagedID])
		if managedID != "" {
			_ = os.Remove(s.directContainerSpecPath(managedID))
		}
	}

	if s.sandboxStore != nil {
		_ = s.sandboxStore.DeleteSandboxByContainerID(c.Request.Context(), target.ContainerID)
	}
	s.syncTraefikRoutes(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{"id": target.ID, "container_id": target.ContainerID, "removed": true})
}

func (s *Server) readContainerFile(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	filePath := strings.TrimSpace(c.Query("path"))
	if filePath == "" {
		writeError(c, http.StatusBadRequest, errors.New("query parameter path is required"))
		return
	}

	response, err := s.readContainerFileByID(c.Request.Context(), s.workerIDForContainerSummary(target), target.ContainerID, filePath)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) writeContainerFile(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		var req SaveFileRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, err)
			return
		}
		if err := s.writeContainerFileByID(c.Request.Context(), s.workerIDForContainerSummary(target), target.ContainerID, req.TargetPath, path.Base(req.TargetPath), strings.NewReader(req.Content)); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": target.ID, "container_id": target.ContainerID, "path": req.TargetPath, "saved": true})
		return
	}

	targetPath := strings.TrimSpace(c.PostForm("target_path"))
	if targetPath == "" {
		writeError(c, http.StatusBadRequest, errors.New("target_path form field is required"))
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, fmt.Errorf("file form field is required: %w", err))
		return
	}
	defer file.Close()

	if err := s.writeContainerFileByID(c.Request.Context(), s.workerIDForContainerSummary(target), target.ContainerID, targetPath, header.Filename, file); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": target.ID, "container_id": target.ContainerID, "path": targetPath, "uploaded": true})
}

// createSandbox godoc
// @Summary Create sandbox
// @Description Creates and starts a managed sandbox container and optionally clones a repository into the workspace.
// @Tags sandboxes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security APIKeyAuth
// @Param payload body CreateSandboxRequest true "Sandbox create payload"
// @Success 200 {object} SandboxResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/sandboxes [post]
func (s *Server) createSandbox(c *gin.Context) {
	response, ok := s.createSandboxCommon(c, sandboxProgressReporter{c: c})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) createSandboxStream(c *gin.Context) {
	setSSEHeaders(c)
	reporter := sandboxProgressReporter{c: c, mu: &sync.Mutex{}, enabled: true}
	response, ok := s.createSandboxCommon(c, reporter)
	if !ok {
		return
	}
	reporter.emit("result", response)
	reporter.emit("done", gin.H{"id": response.ID, "created": true})
}

func (s *Server) createSandboxCommon(c *gin.Context, reporter sandboxProgressReporter) (SandboxResponse, bool) {
	if s.sandboxStore == nil {
		reporter.writeError(http.StatusInternalServerError, errors.New("sandbox store is not configured"))
		return SandboxResponse{}, false
	}

	var req CreateSandboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		reporter.writeError(http.StatusBadRequest, err)
		return SandboxResponse{}, false
	}

	depth, err := validateCloneDepth(req.Depth)
	if err != nil {
		reporter.writeError(http.StatusBadRequest, err)
		return SandboxResponse{}, false
	}
	repoBaseCommit := strings.TrimSpace(req.BaseCommit)
	if err := validateBaseCommitRevision(repoBaseCommit); err != nil {
		reporter.writeError(http.StatusBadRequest, err)
		return SandboxResponse{}, false
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		reporter.writeError(http.StatusUnauthorized, errors.New("missing auth identity"))
		return SandboxResponse{}, false
	}

	sandboxID := newRequestID()
	workerID := localRuntimeWorkerID
	visibleEnv := append([]string(nil), req.Env...)
	secretEnv, err := s.encryptSandboxSecretEnv(req.SecretEnv)
	if err != nil {
		reporter.writeError(secretEnvHTTPStatus(err), err)
		return SandboxResponse{}, false
	}
	runtimeEnv := append(append([]string(nil), visibleEnv...), secretEnv.RuntimeEnv...)
	workspaceDir, err := s.resolveSandboxWorkdir(c.Request.Context(), req.Image, req.Workdir)
	if err != nil {
		reporter.writeError(http.StatusInternalServerError, err)
		return SandboxResponse{}, false
	}
	repoURL := strings.TrimSpace(req.RepoURL)
	repoTargetPath := strings.TrimSpace(req.RepoTargetPath)
	if repoURL != "" && repoTargetPath == "" {
		repoTargetPath = path.Join(workspaceDir, "repo")
	}
	repoReferencePath, repoCacheBind := s.prepareSandboxGitReference(c.Request.Context(), workerID, repoURL, reporter)

	volumeName := ""
	if workspaceDir != "" {
		volumeName = "open-sandbox-" + sandboxID[:12]
		reporter.phase("workspace_volume", "running", "creating sandbox workspace volume")
		_, err = s.runtime.CreateVolume(c.Request.Context(), workerID, volume.CreateOptions{
			Name: volumeName,
			Labels: map[string]string{
				"open-sandbox.managed":    "true",
				"open-sandbox.sandbox_id": sandboxID,
			},
		})
		if err != nil {
			reporter.writeError(http.StatusInternalServerError, fmt.Errorf("create sandbox volume: %w", err))
			return SandboxResponse{}, false
		}
		reporter.phase("workspace_volume", "done", "sandbox workspace volume created")
	}

	containerName := fmt.Sprintf("sandbox-%s-%s", sanitizeSandboxName(req.Name), sandboxID[:6])
	containerConfig := &container.Config{
		Image: req.Image,
		Env:   runtimeEnv,
		Tty:   req.TTY,
		User:  req.User,
		Labels: map[string]string{
			labelOpenSandboxManaged:                  "true",
			labelOpenSandboxSandboxID:                sandboxID,
			labelOpenSandboxOwnerID:                  identity.UserID,
			labelOpenSandboxOwnerUsername:            identity.Username,
			labelOpenSandboxKind:                     managedKindSandbox,
			labelOpenSandboxWorkerID:                 workerID,
			"open-sandbox.name":                      req.Name,
			"open-sandbox.repo_url":                  repoURL,
			"open-sandbox.repo_branch":               strings.TrimSpace(req.Branch),
			"open-sandbox.repo_filter":               strings.TrimSpace(req.Filter),
			"open-sandbox.repo_base_commit":          repoBaseCommit,
			"open-sandbox.repo_target_path":          repoTargetPath,
			"open-sandbox.repo_cache_reference_path": repoReferencePath,
			"open-sandbox.repo_single_branch": func() string {
				if req.SingleBranch {
					return "true"
				}
				return ""
			}(),
			"open-sandbox.repo_depth": func() string {
				if depth > 0 {
					return strconv.Itoa(depth)
				}
				return ""
			}(),
		},
	}
	if workspaceDir != "" {
		containerConfig.WorkingDir = workspaceDir
	}
	if len(req.Cmd) > 0 {
		containerConfig.Cmd = req.Cmd
	} else if !req.UseImageDefaultCmd {
		containerConfig.Cmd = []string{"sleep", "infinity"}
	}
	hostConfig := &container.HostConfig{}
	if workspaceDir != "" {
		hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s", volumeName, workspaceDir))
	}
	if repoCacheBind != "" {
		hostConfig.Binds = append(hostConfig.Binds, repoCacheBind)
	}
	s.runtimeLimits.apply(hostConfig)

	if len(req.Ports) > 0 {
		exposedPorts, portBindings, err := nat.ParsePortSpecs(req.Ports)
		if err != nil {
			reporter.writeError(http.StatusBadRequest, fmt.Errorf("parse ports: %w", err))
			return SandboxResponse{}, false
		}
		containerConfig.ExposedPorts = exposedPorts
		hostConfig.PortBindings = portBindings
	}

	reporter.phase("container_create", "running", "creating sandbox container")
	created, err := s.createContainerWithAutoPull(c.Request.Context(), workerID, req.Image, containerConfig, hostConfig, containerName)
	if err != nil {
		s.cleanupCreatedSandboxArtifacts(c.Request.Context(), workerID, "", volumeName, sandboxID)
		s.logLifecycleFailure("create_sandbox", err, slog.String("sandbox_id", sandboxID), slog.String("image", req.Image))
		reporter.writeError(http.StatusInternalServerError, err)
		return SandboxResponse{}, false
	}
	reporter.phase("container_create", "done", "sandbox container created")

	reporter.phase("container_start", "running", "starting sandbox container")
	if err := s.runtime.StartWorkload(c.Request.Context(), workerID, created.ID, container.StartOptions{}); err != nil {
		s.cleanupCreatedSandboxArtifacts(c.Request.Context(), workerID, created.ID, volumeName, sandboxID)
		s.logLifecycleFailure("start_sandbox", err, slog.String("sandbox_id", sandboxID), slog.String("container_id", created.ID))
		reporter.writeError(http.StatusInternalServerError, fmt.Errorf("start sandbox container: %w", err))
		return SandboxResponse{}, false
	}
	reporter.phase("container_start", "done", "sandbox container started")

	if repoURL != "" {
		reporter.phase("repo_clone", "running", "cloning sandbox repository")
		if err := s.runSandboxGitPhase(c.Request.Context(), workerID, created.ID, "create", "clone", gitCloneCommand(repoURL, repoTargetPath, strings.TrimSpace(req.Branch), req.SingleBranch, depth, strings.TrimSpace(req.Filter), repoReferencePath)); err != nil {
			s.cleanupCreatedSandboxArtifacts(c.Request.Context(), workerID, created.ID, volumeName, sandboxID)
			s.logLifecycleFailure("clone_sandbox_repo", err, slog.String("sandbox_id", sandboxID), slog.String("container_id", created.ID))
			reporter.writeErrorWithDetails(http.StatusBadRequest, "clone repository failed", "git_clone_failed", err.Error())
			return SandboxResponse{}, false
		}
		if repoBaseCommit != "" {
			if err := s.runSandboxGitPhase(c.Request.Context(), workerID, created.ID, "create", "checkout", gitCheckoutCommand(repoTargetPath, repoBaseCommit)); err != nil {
				s.cleanupCreatedSandboxArtifacts(c.Request.Context(), workerID, created.ID, volumeName, sandboxID)
				s.logLifecycleFailure("checkout_sandbox_repo", err, slog.String("sandbox_id", sandboxID), slog.String("container_id", created.ID))
				reporter.writeErrorWithDetails(http.StatusBadRequest, "checkout repository failed", "git_checkout_failed", err.Error())
				return SandboxResponse{}, false
			}
		}
		reporter.phase("repo_clone", "done", "sandbox repository cloned")
	}

	now := timeNowUnix()
	sandboxRecord := store.Sandbox{
		ID:            sandboxID,
		Name:          req.Name,
		Image:         req.Image,
		ContainerID:   created.ID,
		WorkerID:      workerID,
		WorkspaceDir:  workspaceDir,
		RepoURL:       repoURL,
		Env:           visibleEnv,
		SecretEnv:     secretEnv.EncryptedEnv,
		SecretEnvKeys: secretEnv.Keys,
		PortSpecs:     append([]string(nil), req.Ports...),
		Status:        "running",
		OwnerID:       identity.UserID,
		OwnerUsername: identity.Username,
		ProxyConfig:   parseSandboxPortProxyConfigs(req.ProxyConfig),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.sandboxStore.CreateSandbox(c.Request.Context(), sandboxRecord); err != nil {
		s.cleanupCreatedSandboxWorkload(c.Request.Context(), workerID, created.ID, sandboxID)
		s.logLifecycleFailure("persist_sandbox", err, slog.String("sandbox_id", sandboxID), slog.String("container_id", created.ID))
		reporter.writeError(http.StatusInternalServerError, fmt.Errorf("persist sandbox: %w", err))
		return SandboxResponse{}, false
	}
	s.syncTraefikRoutes(c.Request.Context())
	s.logLifecycleSuccess("create_sandbox", slog.String("sandbox_id", sandboxID), slog.String("container_id", created.ID), slog.String("owner_id", identity.UserID))
	return sandboxToResponse(sandboxRecord), true
}

func (s *Server) cleanupCreatedSandboxWorkload(ctx context.Context, workerID string, containerID string, sandboxID string) {
	if err := s.runtime.RemoveWorkload(ctx, workerID, containerID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		s.logger.Warn("cleanup_sandbox_workload_failed", slog.String("sandbox_id", sandboxID), slog.String("container_id", containerID), slog.Any("error", err))
	}
}

func (s *Server) cleanupCreatedSandboxArtifacts(ctx context.Context, workerID string, containerID string, volumeName string, sandboxID string) {
	if strings.TrimSpace(containerID) != "" {
		s.cleanupCreatedSandboxWorkload(ctx, workerID, containerID, sandboxID)
	}
	if strings.TrimSpace(volumeName) == "" {
		return
	}
	if err := s.runtime.RemoveVolume(ctx, workerID, volumeName, true); err != nil {
		if isMissingVolumeError(err) {
			return
		}
		s.logger.Warn("cleanup_sandbox_volume_failed", slog.String("sandbox_id", sandboxID), slog.String("volume_name", volumeName), slog.Any("error", err))
	}
}

func (s *Server) listSandboxes(c *gin.Context) {
	if s.sandboxStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("sandbox store is not configured"))
		return
	}

	sandboxes, err := s.sandboxStore.ListSandboxes(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	runtimeByContainer, err := s.runtimeContainersByID(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	reconciledSandboxes := make([]store.Sandbox, 0, len(sandboxes))
	for _, sandbox := range sandboxes {
		if _, ok := runtimeByContainer[sandbox.ContainerID]; ok {
			reconciledSandboxes = append(reconciledSandboxes, sandbox)
			continue
		}
		if sandbox.OwnerID != identity.UserID {
			continue
		}
		if err := s.sandboxStore.DeleteSandbox(c.Request.Context(), sandbox.ID); err != nil {
			s.logLifecycleFailure("reconcile_missing_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
			reconciledSandboxes = append(reconciledSandboxes, sandbox)
			continue
		}
		s.logLifecycleSuccess("reconcile_missing_sandbox", slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
	}

	out := make([]SandboxResponse, 0, len(sandboxes))
	for _, sandbox := range reconciledSandboxes {
		if sandbox.OwnerID != identity.UserID {
			continue
		}
		response := sandboxToResponse(sandbox)
		if runtime, ok := runtimeByContainer[sandbox.ContainerID]; ok {
			if liveState := strings.TrimSpace(runtime.State); liveState != "" {
				response.Status = liveState
			}
			if liveStatus := strings.TrimSpace(runtime.Status); liveStatus != "" {
				response.Status = liveStatus
			}
			response.Ports = runtime.Ports
			response.PreviewURLs = s.previewURLsForSandbox(sandbox.ID, runtime.Ports)
		}
		out = append(out, response)
	}

	c.JSON(http.StatusOK, out)
}

func (s *Server) getSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	response := sandboxToResponse(sandbox)
	if runtimeByContainer, err := s.runtimeContainersByID(c.Request.Context()); err == nil {
		if runtime, ok := runtimeByContainer[sandbox.ContainerID]; ok {
			response.Status = runtime.Status
			response.Ports = runtime.Ports
			response.PreviewURLs = s.previewURLsForSandbox(sandbox.ID, runtime.Ports)
		}
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) updateSandboxProxyConfig(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	var req UpdateSandboxProxyConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	proxyConfig := parseSandboxPortProxyConfigs(req.ProxyConfig)
	if err := s.sandboxStore.UpdateSandboxProxyConfig(c.Request.Context(), sandbox.ID, proxyConfig); err != nil {
		if errors.Is(err, store.ErrSandboxNotFound) {
			writeError(c, http.StatusNotFound, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	updated, err := s.sandboxStore.GetSandbox(c.Request.Context(), sandbox.ID)
	if err != nil {
		if errors.Is(err, store.ErrSandboxNotFound) {
			writeError(c, http.StatusNotFound, err)
			return
		}
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	s.syncTraefikRoutes(c.Request.Context())

	response := sandboxToResponse(updated)
	if runtimeByContainer, err := s.runtimeContainersByID(c.Request.Context()); err == nil {
		if runtime, ok := runtimeByContainer[updated.ContainerID]; ok {
			if liveState := strings.TrimSpace(runtime.State); liveState != "" {
				response.Status = liveState
			}
			if liveStatus := strings.TrimSpace(runtime.Status); liveStatus != "" {
				response.Status = liveStatus
			}
			response.Ports = runtime.Ports
			response.PreviewURLs = s.previewURLsForSandbox(updated.ID, runtime.Ports)
		}
	}

	c.JSON(http.StatusOK, response)
}

// updateSandboxEnv godoc
// @Summary Update sandbox environment variables
// @Description Recreates the sandbox container with updated environment variables while preserving sandbox metadata and the workspace volume.
// @Tags sandboxes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security APIKeyAuth
// @Param id path string true "Sandbox ID"
// @Param payload body UpdateSandboxEnvRequest true "Sandbox env payload"
// @Success 200 {object} SandboxResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/sandboxes/{id}/env [patch]
func (s *Server) updateSandboxEnv(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	var req UpdateSandboxEnvRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	updated, err := s.recreateSandboxWithEnv(c.Request.Context(), sandbox, req.Env, req.SecretEnv, req.RemoveSecretEnvKeys)
	if err != nil {
		writeError(c, secretEnvHTTPStatus(err), err)
		return
	}

	response := sandboxToResponse(updated)
	if runtimeByContainer, err := s.runtimeContainersByID(c.Request.Context()); err == nil {
		if runtime, ok := runtimeByContainer[updated.ContainerID]; ok {
			if liveState := strings.TrimSpace(runtime.State); liveState != "" {
				response.Status = liveState
			}
			if liveStatus := strings.TrimSpace(runtime.Status); liveStatus != "" {
				response.Status = liveStatus
			}
			response.Ports = runtime.Ports
			response.PreviewURLs = s.previewURLsForSandbox(updated.ID, runtime.Ports)
		}
	}

	s.syncTraefikRoutes(c.Request.Context())
	c.JSON(http.StatusOK, response)
}

func (s *Server) restartSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	workerID := s.workerIDForSandbox(sandbox)
	inspect, err := s.runtime.InspectWorkload(c.Request.Context(), workerID, sandbox.ContainerID)
	if err != nil {
		s.logLifecycleFailure("restart_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if inspect.State != nil && inspect.State.Running {
		if err := s.runtime.StopWorkload(c.Request.Context(), workerID, sandbox.ContainerID, container.StopOptions{}); err != nil {
			s.logLifecycleFailure("restart_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := s.runtime.StartWorkload(c.Request.Context(), workerID, sandbox.ContainerID, container.StartOptions{}); err != nil {
		s.logLifecycleFailure("restart_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	_ = s.sandboxStore.UpdateSandboxStatus(c.Request.Context(), sandbox.ID, "running")
	s.logLifecycleSuccess("restart_sandbox", slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
	c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "restarted": true})
}

// resetSandbox godoc
// @Summary Reset sandbox repository
// @Description Resets a sandbox repository to its configured branch or base_commit; if refresh fails, the repository is recloned.
// @Tags sandboxes
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security APIKeyAuth
// @Param id path string true "Sandbox ID"
// @Param payload body ResetSandboxRequest false "Reset payload (empty object)"
// @Success 200 {object} map[string]any
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/sandboxes/{id}/reset [post]
func (s *Server) resetSandbox(c *gin.Context) {
	response, ok := s.resetSandboxCommon(c, sandboxProgressReporter{c: c})
	if !ok {
		return
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) resetSandboxStream(c *gin.Context) {
	setSSEHeaders(c)
	reporter := sandboxProgressReporter{c: c, mu: &sync.Mutex{}, enabled: true}
	response, ok := s.resetSandboxCommon(c, reporter)
	if !ok {
		return
	}
	reporter.emit("result", response)
	reporter.emit("done", gin.H{"id": response["id"], "reset": true})
}

func (s *Server) resetSandboxCommon(c *gin.Context, reporter sandboxProgressReporter) (gin.H, bool) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return nil, false
	}

	workerID := s.workerIDForSandbox(sandbox)
	inspect, err := s.runtime.InspectWorkload(c.Request.Context(), workerID, sandbox.ContainerID)
	if err != nil {
		s.logLifecycleFailure("reset_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
		reporter.writeError(http.StatusInternalServerError, err)
		return nil, false
	}
	if inspect.State == nil || !inspect.State.Running {
		reporter.phase("container_start", "running", "starting sandbox container")
		if err := s.runtime.StartWorkload(c.Request.Context(), workerID, sandbox.ContainerID, container.StartOptions{}); err != nil {
			s.logLifecycleFailure("reset_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
			reporter.writeError(http.StatusInternalServerError, err)
			return nil, false
		}
		reporter.phase("container_start", "done", "sandbox container started")
	}

	repoURL := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_url"])
	repoTargetPath := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_target_path"])
	repoBranch := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_branch"])
	repoBaseCommit := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_base_commit"])
	if err := validateBaseCommitRevision(repoBaseCommit); err != nil {
		reporter.writeError(http.StatusInternalServerError, fmt.Errorf("invalid stored base_commit: %w", err))
		return nil, false
	}
	repoSingleBranch, _ := strconv.ParseBool(strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_single_branch"]))
	repoFilter := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_filter"])
	repoReferencePath := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_cache_reference_path"])
	repoDepth, _ := strconv.Atoi(strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_depth"]))
	if repoURL != "" && repoTargetPath == "" {
		if sandbox.WorkspaceDir != "" {
			repoTargetPath = path.Join(sandbox.WorkspaceDir, "repo")
		} else {
			repoTargetPath = "repo"
		}
	}

	if repoURL != "" {
		_, _ = s.prepareSandboxGitReference(c.Request.Context(), workerID, repoURL, reporter)
		reporter.phase("repo_check", "running", "checking sandbox repository")
		repoExists, err := s.containerRepoExists(c.Request.Context(), workerID, sandbox.ContainerID, repoTargetPath)
		if err != nil {
			reporter.writeError(http.StatusInternalServerError, fmt.Errorf("check repository state: %w", err))
			return nil, false
		}
		reporter.phase("repo_check", "done", "sandbox repository checked")
		if repoExists {
			reporter.phase("repo_fetch", "running", "fetching sandbox repository")
			refreshErr := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "fetch", sandboxGitFetchCommand(repoTargetPath, repoURL, repoBranch, repoDepth, repoFilter))
			if refreshErr == nil {
				reporter.phase("repo_fetch", "done", "sandbox repository fetched")
				reporter.phase("repo_reset", "running", "resetting sandbox repository")
				refreshErr = s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "reset", sandboxGitResetCommand(repoTargetPath, repoBranch, repoBaseCommit))
			}
			if refreshErr == nil {
				reporter.phase("repo_reset", "done", "sandbox repository reset")
				reporter.phase("repo_clean", "running", "cleaning sandbox repository")
				refreshErr = s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "clean", []string{"git", "-C", repoTargetPath, "clean", "-fdx"})
			}
			if refreshErr == nil {
				reporter.phase("repo_clean", "done", "sandbox repository cleaned")
			} else {
				reporter.phase("repo_clone_fallback", "running", "refresh failed; re-cloning sandbox repository")
				tempRepoPath := repoTargetPath + ".reclone"
				if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "clone_prepare", sandboxGitTargetCleanupCommand(tempRepoPath)); err != nil {
					reporter.writeErrorWithDetails(http.StatusInternalServerError, "re-clone repository failed", "git_cleanup_failed", err.Error())
					return nil, false
				}
				if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "clone", gitCloneCommand(repoURL, tempRepoPath, repoBranch, repoSingleBranch, repoDepth, repoFilter, repoReferencePath)); err != nil {
					reporter.writeErrorWithDetails(http.StatusInternalServerError, "re-clone repository failed", "git_clone_failed", err.Error())
					return nil, false
				}
				if repoBaseCommit != "" {
					if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "checkout", gitCheckoutCommand(tempRepoPath, repoBaseCommit)); err != nil {
						reporter.writeErrorWithDetails(http.StatusInternalServerError, "re-clone repository failed", "git_checkout_failed", err.Error())
						return nil, false
					}
				}
				if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "swap", sandboxGitSwapRepoCommand(repoTargetPath, tempRepoPath)); err != nil {
					reporter.writeErrorWithDetails(http.StatusInternalServerError, "re-clone repository failed", "git_swap_failed", err.Error())
					return nil, false
				}
				reporter.phase("repo_clone_fallback", "done", "sandbox repository re-cloned")
			}
		} else {
			reporter.phase("repo_clone", "running", "cloning sandbox repository")
			if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "cleanup", sandboxGitTargetCleanupCommand(repoTargetPath)); err != nil {
				reporter.writeErrorWithDetails(http.StatusInternalServerError, "reset repository failed", "git_cleanup_failed", err.Error())
				return nil, false
			}
			if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "clone", gitCloneCommand(repoURL, repoTargetPath, repoBranch, repoSingleBranch, repoDepth, repoFilter, repoReferencePath)); err != nil {
				reporter.writeErrorWithDetails(http.StatusInternalServerError, "re-clone repository failed", "git_clone_failed", err.Error())
				return nil, false
			}
			if repoBaseCommit != "" {
				if err := s.runSandboxGitPhase(c.Request.Context(), workerID, sandbox.ContainerID, "reset", "checkout", gitCheckoutCommand(repoTargetPath, repoBaseCommit)); err != nil {
					reporter.writeErrorWithDetails(http.StatusInternalServerError, "re-clone repository failed", "git_checkout_failed", err.Error())
					return nil, false
				}
			}
			reporter.phase("repo_clone", "done", "sandbox repository cloned")
		}
	}

	_ = s.sandboxStore.UpdateSandboxStatus(c.Request.Context(), sandbox.ID, "running")
	s.logLifecycleSuccess("reset_sandbox", slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
	return gin.H{"id": sandbox.ID, "reset": true}, true
}

func (s *Server) stopSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	if err := s.runtime.StopWorkload(c.Request.Context(), s.workerIDForSandbox(sandbox), sandbox.ContainerID, container.StopOptions{}); err != nil {
		s.logLifecycleFailure("stop_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	_ = s.sandboxStore.UpdateSandboxStatus(c.Request.Context(), sandbox.ID, "stopped")

	updated, err := s.sandboxStore.GetSandbox(c.Request.Context(), sandbox.ID)
	if err != nil {
		s.logLifecycleFailure("stop_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	s.logLifecycleSuccess("stop_sandbox", slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
	c.JSON(http.StatusOK, sandboxToResponse(updated))
}

func (s *Server) deleteSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	workerID := s.workerIDForSandbox(sandbox)
	_ = s.runtime.StopWorkload(c.Request.Context(), workerID, sandbox.ContainerID, container.StopOptions{})
	if err := s.runtime.RemoveWorkload(c.Request.Context(), workerID, sandbox.ContainerID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		if !isMissingWorkloadError(err) {
			s.logLifecycleFailure("delete_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}

	if err := s.sandboxStore.DeleteSandbox(c.Request.Context(), sandbox.ID); err != nil {
		s.logLifecycleFailure("delete_sandbox", err, slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	s.logLifecycleSuccess("delete_sandbox", slog.String("sandbox_id", sandbox.ID), slog.String("container_id", sandbox.ContainerID))
	c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "deleted": true})
}

func (s *Server) execInSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	var req ExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	response, err := s.runContainerExec(c.Request.Context(), s.workerIDForSandbox(sandbox), sandbox.ContainerID, req)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) streamSandboxLogs(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	follow, _ := strconv.ParseBool(c.DefaultQuery("follow", "true"))
	tail := c.DefaultQuery("tail", "100")
	s.streamLogsForContainer(c, s.workerIDForSandbox(sandbox), sandbox.ContainerID, follow, tail)
}

func (s *Server) streamSandboxTerminal(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	s.streamTerminalForContainer(c, s.workerIDForSandbox(sandbox), sandbox.ContainerID, sandbox.WorkspaceDir)
}

func (s *Server) readSandboxFile(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	filePath := strings.TrimSpace(c.Query("path"))
	if filePath == "" {
		writeError(c, http.StatusBadRequest, errors.New("query parameter path is required"))
		return
	}

	response, err := s.readContainerFileByID(c.Request.Context(), s.workerIDForSandbox(sandbox), sandbox.ContainerID, filePath)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) writeSandboxFile(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		var req SaveFileRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, err)
			return
		}
		if err := s.writeContainerFileByID(c.Request.Context(), s.workerIDForSandbox(sandbox), sandbox.ContainerID, req.TargetPath, path.Base(req.TargetPath), strings.NewReader(req.Content)); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "path": req.TargetPath, "saved": true})
		return
	}

	targetPath := strings.TrimSpace(c.PostForm("target_path"))
	if targetPath == "" {
		writeError(c, http.StatusBadRequest, errors.New("target_path form field is required"))
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		writeError(c, http.StatusBadRequest, fmt.Errorf("file form field is required: %w", err))
		return
	}
	defer file.Close()

	if err := s.writeContainerFileByID(c.Request.Context(), s.workerIDForSandbox(sandbox), sandbox.ContainerID, targetPath, header.Filename, file); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "path": targetPath, "uploaded": true})
}

func (s *Server) streamLogsForContainer(c *gin.Context, workerID string, containerID string, follow bool, tail string) {
	reader, err := s.runtime.Logs(c.Request.Context(), workerID, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
	})
	if err != nil {
		s.logStreamFailure("log_stream_open", err, slog.String("container_id", containerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	defer reader.Close()

	setSSEHeaders(c)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	mu := &sync.Mutex{}
	stdoutWriter := &sseChunkWriter{ctx: c, stream: "stdout", mu: mu}
	stderrWriter := &sseChunkWriter{ctx: c, stream: "stderr", mu: mu}

	inspect, err := s.runtime.InspectWorkload(c.Request.Context(), workerID, containerID)
	if err != nil {
		s.logStreamFailure("log_stream_inspect", err, slog.String("container_id", containerID))
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if inspect.Config != nil && inspect.Config.Tty {
		if _, err := io.Copy(stdoutWriter, reader); err != nil {
			s.logStreamFailure("log_stream_copy", err, slog.String("container_id", containerID))
			emitSSE(c, mu, "error", err.Error())
			flusher.Flush()
			return
		}
	} else if _, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, reader); err != nil {
		s.logStreamFailure("log_stream_copy", err, slog.String("container_id", containerID))
		emitSSE(c, mu, "error", err.Error())
		flusher.Flush()
		return
	}

	emitSSE(c, mu, "done", "stream closed")
	flusher.Flush()
}

func (s *Server) loadSandbox(c *gin.Context) (store.Sandbox, bool) {
	if s.sandboxStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("sandbox store is not configured"))
		return store.Sandbox{}, false
	}

	sandboxID := strings.TrimSpace(c.Param("id"))
	if sandboxID == "" {
		writeError(c, http.StatusBadRequest, errors.New("sandbox id is required"))
		return store.Sandbox{}, false
	}

	sandbox, err := s.sandboxStore.GetSandbox(c.Request.Context(), sandboxID)
	if err != nil {
		if errors.Is(err, store.ErrSandboxNotFound) {
			writeError(c, http.StatusNotFound, err)
			return store.Sandbox{}, false
		}
		writeError(c, http.StatusInternalServerError, err)
		return store.Sandbox{}, false
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return store.Sandbox{}, false
	}
	if sandbox.OwnerID != identity.UserID {
		writeError(c, http.StatusNotFound, store.ErrSandboxNotFound)
		return store.Sandbox{}, false
	}

	return sandbox, true
}

func (s *Server) loadAuthorizedContainer(c *gin.Context) (ContainerSummary, bool) {
	workloadID := strings.TrimSpace(c.Param("id"))
	if workloadID == "" {
		writeError(c, http.StatusBadRequest, errors.New("workload id is required"))
		return ContainerSummary{}, false
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return ContainerSummary{}, false
	}

	containersByID, err := s.runtimeContainersByID(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return ContainerSummary{}, false
	}

	target, exists := containersByID[workloadID]
	if !exists {
		writeError(c, http.StatusNotFound, errors.New("container not found"))
		return ContainerSummary{}, false
	}
	managedComposeProjects, err := s.managedComposeProjects()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return ContainerSummary{}, false
	}
	if !s.runtimeContainerManagedByApp(target, managedComposeProjects) {
		writeError(c, http.StatusNotFound, errors.New("container not found"))
		return ContainerSummary{}, false
	}

	ownedContainerIDs, err := s.ownedRuntimeContainerIDs(c.Request.Context(), identity.UserID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return ContainerSummary{}, false
	}
	ownedComposeProjects, err := s.ownedComposeProjects(identity.UserID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return ContainerSummary{}, false
	}
	if !s.runtimeContainerVisibleToIdentity(target, identity, ownedContainerIDs, ownedComposeProjects) {
		writeError(c, http.StatusNotFound, errors.New("container not found"))
		return ContainerSummary{}, false
	}

	return target, true
}

func (s *Server) ownedRuntimeContainerIDs(ctx context.Context, userID string) (map[string]struct{}, error) {
	owned := map[string]struct{}{}
	if s.sandboxStore == nil {
		return owned, nil
	}

	sandboxes, err := s.sandboxStore.ListSandboxes(ctx)
	if err != nil {
		return nil, err
	}
	for _, sandbox := range sandboxes {
		if sandbox.OwnerID == userID {
			owned[sandbox.ContainerID] = struct{}{}
		}
	}

	return owned, nil
}

func (s *Server) ownedComposeProjects(userID string) (map[string]struct{}, error) {
	owned := map[string]struct{}{}
	composeRoot := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose")
	entries, err := os.ReadDir(composeRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return owned, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		payload, readErr := os.ReadFile(filepath.Join(composeRoot, entry.Name(), composeOwnerMetadataFile))
		if readErr != nil {
			continue
		}
		var owner managedOwnerMetadata
		if jsonErr := json.Unmarshal(payload, &owner); jsonErr != nil {
			continue
		}
		if owner.UserID == userID {
			owned[entry.Name()] = struct{}{}
		}
	}
	return owned, nil
}

func (s *Server) managedComposeProjects() (map[string]struct{}, error) {
	managed := map[string]struct{}{}
	composeRoot := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose")
	entries, err := os.ReadDir(composeRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return managed, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		composeFile := filepath.Join(composeRoot, entry.Name(), "docker-compose.yml")
		if _, err := os.Stat(composeFile); err == nil {
			managed[entry.Name()] = struct{}{}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		ownerFile := filepath.Join(composeRoot, entry.Name(), composeOwnerMetadataFile)
		if _, err := os.Stat(ownerFile); err == nil {
			managed[entry.Name()] = struct{}{}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return managed, nil
}

func (s *Server) filterManagedRuntimeContainers(containers []ContainerSummary, managedComposeProjects map[string]struct{}) []ContainerSummary {
	filtered := make([]ContainerSummary, 0, len(containers))
	for _, item := range containers {
		if s.runtimeContainerManagedByApp(item, managedComposeProjects) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Server) runtimeContainerManagedByApp(item ContainerSummary, managedComposeProjects map[string]struct{}) bool {
	if strings.EqualFold(strings.TrimSpace(item.Labels[labelOpenSandboxManaged]), "true") {
		return true
	}
	if strings.TrimSpace(item.Labels[labelOpenSandboxSandboxID]) != "" {
		return true
	}
	if strings.TrimSpace(item.Labels[labelOpenSandboxManagedID]) != "" {
		return true
	}
	projectName := strings.TrimSpace(item.ProjectName)
	if projectName == "" {
		projectName = strings.TrimSpace(item.Labels["com.docker.compose.project"])
	}
	if projectName == "" {
		return false
	}
	composeRoot := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose")
	if workingDir := strings.TrimSpace(item.Labels["com.docker.compose.project.working_dir"]); workingDir != "" {
		if rel, err := filepath.Rel(composeRoot, workingDir); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	if configFiles := strings.TrimSpace(item.Labels["com.docker.compose.project.config_files"]); configFiles != "" {
		for _, configFile := range strings.Split(configFiles, ",") {
			configFile = strings.TrimSpace(configFile)
			if configFile == "" {
				continue
			}
			if rel, err := filepath.Rel(composeRoot, configFile); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return true
			}
		}
	}
	_, ok := managedComposeProjects[projectName]
	return ok
}

func (s *Server) runtimeContainerVisibleToIdentity(item ContainerSummary, identity AuthIdentity, ownedContainerIDs map[string]struct{}, ownedComposeProjects map[string]struct{}) bool {
	if item.Labels[labelOpenSandboxOwnerID] == identity.UserID {
		return true
	}
	if projectName := s.runtime.ProjectName(item); projectName != "" {
		_, ok := ownedComposeProjects[projectName]
		if ok {
			return true
		}
	}
	_, ok := ownedContainerIDs[item.ContainerID]
	return ok
}

func (s *Server) readContainerFileByID(ctx context.Context, workerID string, containerID string, filePath string) (FileReadResponse, error) {
	reader, stat, err := s.runtime.CopyFrom(ctx, workerID, containerID, filePath)
	if err != nil {
		return FileReadResponse{}, fmt.Errorf("copy file from container: %w", err)
	}
	defer reader.Close()

	if os.FileMode(stat.Mode).IsDir() {
		entries, err := extractDirectoryEntriesFromTar(stat.Name, filePath, reader)
		if err != nil {
			return FileReadResponse{}, err
		}
		return FileReadResponse{Path: filePath, Name: stat.Name, Kind: "directory", Entries: entries}, nil
	}

	name, content, err := extractSingleFileFromTar(reader)
	if err != nil {
		return FileReadResponse{}, err
	}

	return FileReadResponse{Path: filePath, Name: name, Kind: "file", Content: content}, nil
}

func (s *Server) writeContainerFileByID(ctx context.Context, workerID string, containerID string, targetPath string, uploadFilename string, content io.Reader) error {
	trimmed := strings.TrimSpace(targetPath)
	if trimmed == "" {
		return errors.New("target path is required")
	}

	if !strings.HasPrefix(trimmed, "/") {
		return errors.New("target path must be absolute inside the container")
	}

	cleanPath := path.Clean(trimmed)
	if cleanPath == "/" || cleanPath == "." {
		return errors.New("target path must point to a file")
	}

	targetDir := path.Dir(cleanPath)
	targetName := path.Base(cleanPath)
	if targetName == "." || targetName == "/" {
		targetName = strings.TrimSpace(uploadFilename)
	}
	if targetName == "" {
		return errors.New("target file name is required")
	}

	archiveReader, err := tarSingleFile(targetName, content)
	if err != nil {
		return err
	}

	if err := s.runtime.CopyTo(ctx, workerID, containerID, targetDir, archiveReader, container.CopyToContainerOptions{AllowOverwriteDirWithFile: true}); err != nil {
		return fmt.Errorf("copy file to container: %w", err)
	}

	return nil
}

func (s *Server) liveContainerState(ctx context.Context) (map[string]string, map[string]string, map[string][]PortSummary) {
	stateByContainer := map[string]string{}
	statusByContainer := map[string]string{}
	portsByContainer := map[string][]PortSummary{}

	containers, err := s.runtime.ListWorkloads(ctx)
	if err != nil {
		return stateByContainer, statusByContainer, portsByContainer
	}

	for _, item := range containers {
		stateByContainer[item.ContainerID] = strings.TrimSpace(item.State)
		statusByContainer[item.ContainerID] = strings.TrimSpace(item.Status)
		portsByContainer[item.ContainerID] = item.Ports
	}

	return stateByContainer, statusByContainer, portsByContainer
}

func (s *Server) runtimeContainersByID(ctx context.Context) (map[string]ContainerSummary, error) {
	containers, err := s.runtime.ListWorkloads(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ContainerSummary, len(containers)*2)
	for _, item := range containers {
		byID[item.ID] = item
		byID[item.ContainerID] = item
	}
	return byID, nil
}

func containerIndexByContainerID(containers []ContainerSummary) map[string]ContainerSummary {
	byID := make(map[string]ContainerSummary, len(containers))
	for _, item := range containers {
		byID[item.ContainerID] = item
	}
	return byID
}

func (s *Server) reconcileContainerArtifacts(runtimeContainers map[string]ContainerSummary) {
	cutoff := time.Now().Add(time.Second)
	_, _ = s.cleanupDirectContainerSpecs(runtimeContainers, cutoff, false)
}

func (s *Server) resolveSandboxWorkdir(ctx context.Context, imageRef string, requestedWorkdir string) (string, error) {
	if workdir := strings.TrimSpace(requestedWorkdir); workdir != "" {
		if workdir == "/" {
			return defaultSandboxWorkspaceDir, nil
		}
		return workdir, nil
	}

	inspected, err := s.inspectImageWithAutoPull(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("inspect sandbox image: %w", err)
	}
	if inspected.Config != nil {
		if workdir := strings.TrimSpace(inspected.Config.WorkingDir); workdir != "" {
			if workdir == "/" {
				return defaultSandboxWorkspaceDir, nil
			}
			return workdir, nil
		}
	}

	return "", nil
}

func (s *Server) recreateSandboxWithEnv(ctx context.Context, sandbox store.Sandbox, env []string, secretEnv []string, removeSecretEnvKeys []string) (store.Sandbox, error) {
	workerID := s.workerIDForSandbox(sandbox)
	secretState, err := s.resolveSandboxSecretEnvState(sandbox, secretEnv, removeSecretEnvKeys)
	if err != nil {
		return store.Sandbox{}, err
	}
	runtimeEnv := append(append([]string(nil), env...), secretState.RuntimeEnv...)
	inspect, err := s.runtime.InspectWorkload(ctx, workerID, sandbox.ContainerID)
	if err != nil {
		return store.Sandbox{}, fmt.Errorf("inspect sandbox container: %w", err)
	}
	if inspect.Config == nil {
		return store.Sandbox{}, errors.New("sandbox container config is unavailable")
	}

	containerConfig := cloneSandboxContainerConfig(inspect.Config, runtimeEnv)
	hostConfig := cloneSandboxHostConfig(inspect.HostConfig)
	containerName := strings.TrimPrefix(strings.TrimSpace(inspect.Name), "/")
	replacementName := containerName
	if replacementName != "" {
		replacementName = fmt.Sprintf("%s-replacement-%d", replacementName, time.Now().UnixNano())
	}
	imageRef := strings.TrimSpace(containerConfig.Image)
	if imageRef == "" {
		imageRef = sandbox.Image
	}

	wasRunning := inspect.State != nil && inspect.State.Running
	status := sandbox.Status
	if inspect.State != nil {
		if currentStatus := strings.TrimSpace(inspect.State.Status); currentStatus != "" {
			status = currentStatus
		}
	}

	if wasRunning {
		if err := s.runtime.StopWorkload(ctx, workerID, sandbox.ContainerID, container.StopOptions{}); err != nil {
			return store.Sandbox{}, fmt.Errorf("stop sandbox container: %w", err)
		}
	}

	created, err := s.createContainerWithAutoPull(ctx, workerID, imageRef, containerConfig, hostConfig, replacementName)
	if err != nil {
		if wasRunning {
			if restartErr := s.runtime.StartWorkload(ctx, workerID, sandbox.ContainerID, container.StartOptions{}); restartErr != nil {
				return store.Sandbox{}, fmt.Errorf("recreate sandbox container: %w (failed to restart original container: %v)", err, restartErr)
			}
		}
		return store.Sandbox{}, fmt.Errorf("recreate sandbox container: %w", err)
	}
	if wasRunning {
		if err := s.runtime.StartWorkload(ctx, workerID, created.ID, container.StartOptions{}); err != nil {
			_ = s.runtime.RemoveWorkload(ctx, workerID, created.ID, container.RemoveOptions{Force: true, RemoveVolumes: false})
			if restartErr := s.runtime.StartWorkload(ctx, workerID, sandbox.ContainerID, container.StartOptions{}); restartErr != nil {
				return store.Sandbox{}, fmt.Errorf("start recreated sandbox container: %w (failed to restart original container: %v)", err, restartErr)
			}
			return store.Sandbox{}, fmt.Errorf("start recreated sandbox container: %w", err)
		}
		status = "running"
	}

	if err := s.sandboxStore.UpdateSandboxRuntime(ctx, sandbox.ID, created.ID, env, secretState.EncryptedEnv, secretState.Keys, status); err != nil {
		if rollbackErr := s.rollbackSandboxEnvReplacement(ctx, workerID, sandbox.ContainerID, created.ID, wasRunning); rollbackErr != nil {
			return store.Sandbox{}, fmt.Errorf("update sandbox runtime: %w (rollback failed: %v)", err, rollbackErr)
		}
		return store.Sandbox{}, fmt.Errorf("update sandbox runtime: %w", err)
	}
	if err := s.runtime.RemoveWorkload(ctx, workerID, sandbox.ContainerID, container.RemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
		if !isMissingWorkloadError(err) {
			return store.Sandbox{}, fmt.Errorf("remove sandbox container: %w", err)
		}
	}

	updated, err := s.sandboxStore.GetSandbox(ctx, sandbox.ID)
	if err != nil {
		return store.Sandbox{}, err
	}
	return updated, nil
}

func (s *Server) rollbackSandboxEnvReplacement(ctx context.Context, workerID, originalContainerID, replacementContainerID string, restartOriginal bool) error {
	if err := s.runtime.RemoveWorkload(ctx, workerID, replacementContainerID, container.RemoveOptions{Force: true, RemoveVolumes: false}); err != nil && !isMissingWorkloadError(err) {
		return fmt.Errorf("remove replacement sandbox container: %w", err)
	}
	if restartOriginal {
		if err := s.runtime.StartWorkload(ctx, workerID, originalContainerID, container.StartOptions{}); err != nil {
			return fmt.Errorf("restart original sandbox container: %w", err)
		}
	}
	return nil
}

func cloneSandboxContainerConfig(config *container.Config, env []string) *container.Config {
	cloned := *config
	cloned.Env = append([]string(nil), env...)
	if config.Cmd != nil {
		cloned.Cmd = append([]string(nil), config.Cmd...)
	}
	if config.Entrypoint != nil {
		cloned.Entrypoint = append([]string(nil), config.Entrypoint...)
	}
	if config.Shell != nil {
		cloned.Shell = append([]string(nil), config.Shell...)
	}
	if config.Labels != nil {
		cloned.Labels = make(map[string]string, len(config.Labels))
		for key, value := range config.Labels {
			cloned.Labels[key] = value
		}
	}
	if config.Volumes != nil {
		cloned.Volumes = make(map[string]struct{}, len(config.Volumes))
		for key, value := range config.Volumes {
			cloned.Volumes[key] = value
		}
	}
	if config.ExposedPorts != nil {
		cloned.ExposedPorts = make(nat.PortSet, len(config.ExposedPorts))
		for key, value := range config.ExposedPorts {
			cloned.ExposedPorts[key] = value
		}
	}
	return &cloned
}

func cloneSandboxHostConfig(config *container.HostConfig) *container.HostConfig {
	if config == nil {
		return &container.HostConfig{}
	}
	cloned := *config
	if config.Binds != nil {
		cloned.Binds = append([]string(nil), config.Binds...)
	}
	if config.PortBindings != nil {
		cloned.PortBindings = make(nat.PortMap, len(config.PortBindings))
		for key, value := range config.PortBindings {
			cloned.PortBindings[key] = append([]nat.PortBinding(nil), value...)
		}
	}
	if config.Tmpfs != nil {
		cloned.Tmpfs = make(map[string]string, len(config.Tmpfs))
		for key, value := range config.Tmpfs {
			cloned.Tmpfs[key] = value
		}
	}
	if config.Annotations != nil {
		cloned.Annotations = make(map[string]string, len(config.Annotations))
		for key, value := range config.Annotations {
			cloned.Annotations[key] = value
		}
	}
	return &cloned
}

func (s *Server) inspectImageWithAutoPull(ctx context.Context, imageRef string) (image.InspectResponse, error) {
	inspected, err := s.docker.ImageInspect(ctx, imageRef)
	if err == nil {
		return inspected, nil
	}

	if !isMissingImageError(err) {
		return image.InspectResponse{}, err
	}

	pullReader, pullErr := s.docker.ImagePull(ctx, imageRef, image.PullOptions{})
	if pullErr != nil {
		return image.InspectResponse{}, fmt.Errorf("inspect image failed and auto-pull failed: %w", pullErr)
	}
	defer pullReader.Close()

	if _, pullErr := io.Copy(io.Discard, pullReader); pullErr != nil {
		return image.InspectResponse{}, fmt.Errorf("read pull output: %w", pullErr)
	}

	inspected, err = s.docker.ImageInspect(ctx, imageRef)
	if err != nil {
		return image.InspectResponse{}, fmt.Errorf("inspect image after pull: %w", err)
	}

	return inspected, nil
}

func (s *Server) createContainerWithAutoPull(
	ctx context.Context,
	workerID string,
	imageRef string,
	containerConfig *container.Config,
	hostConfig *container.HostConfig,
	containerName string,
) (container.CreateResponse, error) {
	created, err := s.runtime.CreateWorkload(ctx, workerID, containerConfig, hostConfig, nil, nil, containerName)
	if err == nil {
		return created, nil
	}

	if !isMissingImageError(err) {
		return container.CreateResponse{}, err
	}

	pullReader, pullErr := s.docker.ImagePull(ctx, imageRef, image.PullOptions{})
	if pullErr != nil {
		return container.CreateResponse{}, fmt.Errorf("create container failed and auto-pull failed: %w", pullErr)
	}
	defer pullReader.Close()

	if _, pullErr := io.Copy(io.Discard, pullReader); pullErr != nil {
		return container.CreateResponse{}, fmt.Errorf("read pull output: %w", pullErr)
	}

	created, err = s.runtime.CreateWorkload(ctx, workerID, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return container.CreateResponse{}, fmt.Errorf("create container after pull: %w", err)
	}

	return created, nil
}

func sandboxToResponse(sandbox store.Sandbox) SandboxResponse {
	return SandboxResponse{
		ID:            sandbox.ID,
		Name:          sandbox.Name,
		Image:         sandbox.Image,
		ContainerID:   sandbox.ContainerID,
		WorkerID:      sandbox.WorkerID,
		WorkspaceDir:  sandbox.WorkspaceDir,
		RepoURL:       sandbox.RepoURL,
		Env:           append([]string(nil), sandbox.Env...),
		SecretEnvKeys: append([]string(nil), sandbox.SecretEnvKeys...),
		Status:        sandbox.Status,
		OwnerUsername: sandbox.OwnerUsername,
		ProxyConfig:   sandboxProxyConfigToResponse(sandbox.ProxyConfig),
		PortSpecs:     append([]string(nil), sandbox.PortSpecs...),
		Ports:         nil,
		PreviewURLs:   nil,
		CreatedAt:     sandbox.CreatedAt,
		UpdatedAt:     sandbox.UpdatedAt,
	}
}

func sandboxProxyConfigToResponse(input map[int]traefikcfg.ServiceProxyConfig) map[string]*SandboxPortProxyConfig {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]*SandboxPortProxyConfig, len(input))
	for port, cfg := range input {
		responseCfg := &SandboxPortProxyConfig{
			RequestHeaders:  cfg.RequestHeaders,
			ResponseHeaders: cfg.ResponseHeaders,
			PathPrefixStrip: strings.TrimSpace(cfg.PathPrefixStrip),
			SkipAuth:        cfg.SkipAuth,
		}
		if cfg.CORS != nil {
			responseCfg.CORS = &SandboxPortCORSConfig{
				AllowOrigins:     cfg.CORS.AllowOrigins,
				AllowMethods:     cfg.CORS.AllowMethods,
				AllowHeaders:     cfg.CORS.AllowHeaders,
				AllowCredentials: cfg.CORS.AllowCredentials,
				MaxAge:           cfg.CORS.MaxAge,
			}
		}
		result[strconv.Itoa(port)] = responseCfg
	}
	return result
}

// parseSandboxPortProxyConfigs converts the JSON-friendly string-keyed map
// from CreateSandboxRequest into the int-keyed traefik config map stored in
// the Sandbox record.
func parseSandboxPortProxyConfigs(input map[string]*SandboxPortProxyConfig) map[int]traefikcfg.ServiceProxyConfig {
	if len(input) == 0 {
		return nil
	}
	result := make(map[int]traefikcfg.ServiceProxyConfig, len(input))
	for portStr, cfg := range input {
		if cfg == nil {
			continue
		}
		port, ok := parseProxyConfigPort(portStr)
		if !ok {
			continue
		}
		spc := traefikcfg.ServiceProxyConfig{
			RequestHeaders:  cfg.RequestHeaders,
			ResponseHeaders: cfg.ResponseHeaders,
			PathPrefixStrip: strings.TrimSpace(cfg.PathPrefixStrip),
			SkipAuth:        cfg.SkipAuth,
		}
		if cfg.CORS != nil {
			spc.CORS = &traefikcfg.CORSConfig{
				AllowOrigins:     cfg.CORS.AllowOrigins,
				AllowMethods:     cfg.CORS.AllowMethods,
				AllowHeaders:     cfg.CORS.AllowHeaders,
				AllowCredentials: cfg.CORS.AllowCredentials,
				MaxAge:           cfg.CORS.MaxAge,
			}
		}
		result[port] = spc
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseProxyConfigPort(raw string) (int, bool) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port <= 0 {
		return 0, false
	}
	return port, true
}

func (s *Server) previewURLsForContainer(item ContainerSummary) []PreviewURL {
	if sandboxID := strings.TrimSpace(item.Labels[labelOpenSandboxSandboxID]); sandboxID != "" {
		return s.previewURLsForSandbox(sandboxID, item.Ports)
	}

	if managedID := strings.TrimSpace(item.Labels[labelOpenSandboxManagedID]); managedID != "" {
		return s.previewURLsForManagedContainer(managedID, item.Ports)
	}

	projectName := strings.TrimSpace(item.ProjectName)
	if projectName == "" {
		projectName = strings.TrimSpace(item.Labels["com.docker.compose.project"])
	}
	serviceName := strings.TrimSpace(item.ServiceName)
	if serviceName == "" {
		serviceName = strings.TrimSpace(item.Labels["com.docker.compose.service"])
	}
	if projectName == "" || serviceName == "" {
		return nil
	}

	return s.previewURLsForComposeService(projectName, serviceName, item.Ports)
}

func (s *Server) previewURLsForSandbox(sandboxID string, ports []PortSummary) []PreviewURL {
	trimmedID := strings.TrimSpace(sandboxID)
	if trimmedID == "" {
		return nil
	}
	return previewURLsForPorts(ports, func(privatePort int) string {
		return s.previewLaunchURLForSandbox(trimmedID, privatePort)
	}, true)
}

func (s *Server) previewURLsForManagedContainer(managedID string, ports []PortSummary) []PreviewURL {
	trimmedID := strings.TrimSpace(managedID)
	if trimmedID == "" {
		return nil
	}
	return previewURLsForPorts(ports, func(privatePort int) string {
		return s.previewLaunchURLForManagedContainer(trimmedID, privatePort)
	}, true)
}

func (s *Server) previewURLsForComposeService(projectName string, serviceName string, ports []PortSummary) []PreviewURL {
	trimmedProject := strings.TrimSpace(projectName)
	trimmedService := strings.TrimSpace(serviceName)
	if trimmedProject == "" || trimmedService == "" {
		return nil
	}
	return previewURLsForPorts(ports, func(privatePort int) string {
		return s.previewLaunchURLForComposeService(trimmedProject, trimmedService, privatePort)
	}, true)
}

func previewURLsForPorts(ports []PortSummary, buildURL func(privatePort int) string, requirePublished bool) []PreviewURL {
	if len(ports) == 0 {
		return nil
	}

	seenPrivatePorts := make(map[int]struct{}, len(ports))
	previewURLs := make([]PreviewURL, 0, len(ports))
	for _, port := range ports {
		if port.Private <= 0 {
			continue
		}
		if requirePublished && port.Public <= 0 {
			continue
		}
		if _, exists := seenPrivatePorts[port.Private]; exists {
			continue
		}
		seenPrivatePorts[port.Private] = struct{}{}
		previewURLs = append(previewURLs, PreviewURL{PrivatePort: port.Private, URL: buildURL(port.Private)})
	}

	sort.Slice(previewURLs, func(i, j int) bool {
		return previewURLs[i].PrivatePort < previewURLs[j].PrivatePort
	})

	if len(previewURLs) == 0 {
		return nil
	}
	return previewURLs
}

func (s *Server) portSpecsForContainer(item ContainerSummary) []string {
	if strings.TrimSpace(s.workspaceRoot) == "" {
		return nil
	}
	managedID := strings.TrimSpace(item.Labels[labelOpenSandboxManagedID])
	if managedID == "" || strings.TrimSpace(item.Labels[labelOpenSandboxKind]) != managedKindDirect {
		return nil
	}
	req, err := s.readDirectContainerSpec(managedID)
	if err != nil {
		return nil
	}
	if len(req.Ports) == 0 {
		return nil
	}
	return append([]string(nil), req.Ports...)
}

func extractSingleFileFromTar(reader io.Reader) (string, string, error) {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return "", "", errors.New("file not found in archive")
		}
		if err != nil {
			return "", "", fmt.Errorf("read archive: %w", err)
		}
		if header.FileInfo().IsDir() {
			continue
		}

		content, err := io.ReadAll(tarReader)
		if err != nil {
			return "", "", fmt.Errorf("read file content: %w", err)
		}

		return header.Name, string(content), nil
	}
}

func extractDirectoryEntriesFromTar(baseName string, requestedPath string, reader io.Reader) ([]FileEntry, error) {
	tarReader := tar.NewReader(reader)
	entryByName := make(map[string]FileEntry)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read directory archive: %w", err)
		}

		cleanName := strings.TrimPrefix(path.Clean(header.Name), "./")
		if cleanName == "." || cleanName == "" {
			continue
		}

		prefix := strings.Trim(strings.TrimSpace(baseName), "/")
		relative := cleanName
		if prefix != "" && relative == prefix {
			continue
		}
		if prefix != "" && strings.HasPrefix(relative, prefix+"/") {
			relative = strings.TrimPrefix(relative, prefix+"/")
		}
		relative = strings.TrimPrefix(relative, "/")
		if relative == "" {
			continue
		}

		parts := strings.Split(relative, "/")
		name := parts[0]
		if name == "" {
			continue
		}

		entry := entryByName[name]
		entry.Name = name
		entry.Path = path.Join(strings.TrimRight(requestedPath, "/"), name)
		entry.Kind = "file"
		if len(parts) > 1 || header.FileInfo().IsDir() {
			entry.Kind = "directory"
		}
		if entry.Size == 0 && header.Size > 0 {
			entry.Size = header.Size
		}
		entryByName[name] = entry
	}

	entries := make([]FileEntry, 0, len(entryByName))
	for _, entry := range entryByName {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "directory"
		}
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

func portSummaries(ports []container.Port) []PortSummary {
	if len(ports) == 0 {
		return nil
	}

	out := make([]PortSummary, 0, len(ports))
	for _, port := range ports {
		out = append(out, PortSummary{
			Private: int(port.PrivatePort),
			Public:  int(port.PublicPort),
			Type:    port.Type,
			IP:      port.IP,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Private != out[j].Private {
			return out[i].Private < out[j].Private
		}
		return out[i].Public < out[j].Public
	})

	return out
}

func tarSingleFile(name string, content io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return nil, fmt.Errorf("read upload content: %w", err)
	}

	var buffer bytes.Buffer
	tarWriter := tar.NewWriter(&buffer)
	header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}
	if err := tarWriter.WriteHeader(header); err != nil {
		return nil, fmt.Errorf("write tar header: %w", err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		return nil, fmt.Errorf("write tar content: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("close tar archive: %w", err)
	}

	return bytes.NewReader(buffer.Bytes()), nil
}

var sandboxNameNormalizer = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
var dockerCLIPortMappingPattern = regexp.MustCompile(`(?:.+:)?(\d+)->(\d+)/(tcp|udp)`)
var dockerCLIPrivatePortPattern = regexp.MustCompile(`^(\d+)/(tcp|udp)$`)

func sanitizeSandboxName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "sandbox"
	}

	normalized := sandboxNameNormalizer.ReplaceAllString(trimmed, "-")
	normalized = strings.Trim(normalized, "-._")
	if normalized == "" {
		return "sandbox"
	}

	if len(normalized) > 24 {
		normalized = normalized[:24]
	}

	return strings.ToLower(normalized)
}

func (s *Server) sandboxGitCacheRoot() string {
	return filepath.Join(s.workspaceRoot, ".open-sandbox", "git-cache")
}

func sandboxGitCacheKey(repoURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(repoURL)))
	return fmt.Sprintf("%x", sum[:12])
}

var sandboxGitSCPLikeRemotePattern = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):[^\s]+$`)

func sandboxGitCacheRemoteAllowed(ctx context.Context, repoURL string) bool {
	host, ok := sandboxGitRemoteHost(repoURL)
	if !ok {
		return false
	}
	if host == "github.com" || host == "gitlab.com" || host == "bitbucket.org" {
		return true
	}
	if strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || host == "localhost" || !strings.Contains(host, ".") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return sandboxGitCacheAddrAllowed(addr)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, ipAddr := range addrs {
		addr, ok := netip.AddrFromSlice(ipAddr.IP)
		if !ok || !sandboxGitCacheAddrAllowed(addr) {
			return false
		}
	}
	return true
}

func sandboxGitRemoteHost(repoURL string) (string, bool) {
	trimmed := strings.TrimSpace(repoURL)
	if trimmed == "" {
		return "", false
	}
	if parsed, err := url.Parse(trimmed); err == nil && strings.TrimSpace(parsed.Hostname()) != "" {
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), ".")), true
	}
	matches := sandboxGitSCPLikeRemotePattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(matches[1]), "."))
	if host == "" {
		return "", false
	}
	return host, true
}

func sandboxGitCacheAddrAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsMulticast() && !addr.IsLinkLocalUnicast() && !addr.IsLinkLocalMulticast() && !addr.IsUnspecified()
}

func (s *Server) prepareSandboxGitReference(ctx context.Context, workerID string, repoURL string, reporter sandboxProgressReporter) (string, string) {
	if workerID != localRuntimeWorkerID || strings.TrimSpace(repoURL) == "" {
		return "", ""
	}
	if !sandboxGitCacheRemoteAllowed(ctx, repoURL) {
		s.logger.Debug("sandbox_git_cache_skipped", slog.String("repo_url", repoURL), slog.String("reason", "unsafe_remote"))
		return "", ""
	}
	unlock := s.lockSandboxGitCacheRepo(repoURL)
	defer unlock()
	cacheRoot := s.sandboxGitCacheRoot()
	if err := ensurePrivateDir(cacheRoot); err != nil {
		s.logger.Warn("sandbox_git_cache_disabled", slog.String("repo_url", repoURL), slog.String("error", err.Error()))
		return "", ""
	}
	reporter.phase("repo_cache", "running", "refreshing shared repository cache")
	cacheDir := filepath.Join(cacheRoot, sandboxGitCacheKey(repoURL)+".git")
	var err error
	var stderr string
	if _, statErr := os.Stat(cacheDir); statErr == nil {
		_, stderr, err = commandRunner(ctx, "git", "-C", cacheDir, "fetch", "--prune", "origin")
	} else if errors.Is(statErr, os.ErrNotExist) {
		_, stderr, err = commandRunner(ctx, "git", "clone", "--mirror", repoURL, cacheDir)
		if err != nil {
			s.logger.Warn("sandbox_git_cache_clone_failed", slog.String("repo_url", repoURL), slog.String("error", strings.TrimSpace(stderr)))
			return "", ""
		}
		reporter.phase("repo_cache", "done", "shared repository cache refreshed")
		return path.Join(sandboxGitCacheMountRoot, filepath.Base(cacheDir)), fmt.Sprintf("%s:%s:ro", cacheRoot, sandboxGitCacheMountRoot)
	} else {
		s.logger.Warn("sandbox_git_cache_stat_failed", slog.String("repo_url", repoURL), slog.String("error", statErr.Error()))
		return "", ""
	}
	if err != nil {
		s.logger.Warn("sandbox_git_cache_fetch_failed", slog.String("repo_url", repoURL), slog.String("error", strings.TrimSpace(stderr)))
		return "", ""
	}
	reporter.phase("repo_cache", "done", "shared repository cache refreshed")
	return path.Join(sandboxGitCacheMountRoot, filepath.Base(cacheDir)), fmt.Sprintf("%s:%s:ro", cacheRoot, sandboxGitCacheMountRoot)
}

func (s *Server) lockSandboxGitCacheRepo(repoURL string) func() {
	key := sandboxGitCacheKey(repoURL)
	value, _ := s.gitCacheLocks.LoadOrStore(key, &sync.Mutex{})
	mu, _ := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *Server) containerRepoExists(ctx context.Context, workerID string, containerID string, repoTargetPath string) (bool, error) {
	resp, err := s.runContainerExec(ctx, workerID, containerID, ExecRequest{Cmd: []string{"sh", "-lc", fmt.Sprintf("test -d %s/.git", shellQuote(repoTargetPath))}})
	if err != nil {
		return false, err
	}
	return resp.ExitCode == 0, nil
}

func (s *Server) runSandboxGitPhase(ctx context.Context, workerID string, containerID string, operation string, phase string, cmd []string) error {
	startedAt := time.Now()
	resp, err := s.runContainerExec(ctx, workerID, containerID, ExecRequest{Cmd: cmd})
	result := "success"
	if err != nil {
		result = "error"
	}
	if err == nil && resp.ExitCode != 0 {
		result = "error"
	}
	s.metrics.recordSandboxRepoPhase(operation, phase, result, time.Since(startedAt))
	if err != nil {
		return err
	}
	if resp.ExitCode != 0 {
		stderr := strings.TrimSpace(resp.Stderr)
		if stderr == "" {
			stderr = strings.TrimSpace(resp.Stdout)
		}
		if stderr == "" {
			stderr = fmt.Sprintf("command exited with code %d", resp.ExitCode)
		}
		return errors.New(stderr)
	}
	return nil
}

func sandboxGitFetchCommand(repoTargetPath string, repoURL string, branch string, depth int, filter string) []string {
	var command strings.Builder
	command.WriteString("git -C ")
	command.WriteString(shellQuote(repoTargetPath))
	command.WriteString(" remote set-url origin ")
	command.WriteString(shellQuote(repoURL))
	command.WriteString(" && git -C ")
	command.WriteString(shellQuote(repoTargetPath))
	command.WriteString(" fetch --prune")
	if depth > 0 {
		command.WriteString(" --depth ")
		command.WriteString(strconv.Itoa(depth))
	}
	if strings.TrimSpace(filter) != "" {
		command.WriteString(" --filter=")
		command.WriteString(shellQuote(strings.TrimSpace(filter)))
	}
	command.WriteString(" origin")
	if strings.TrimSpace(branch) != "" {
		command.WriteByte(' ')
		command.WriteString(shellQuote(strings.TrimSpace(branch)))
	}
	return []string{"sh", "-lc", command.String()}
}

func sandboxGitResetCommand(repoTargetPath string, branch string, baseCommit string) []string {
	if strings.TrimSpace(baseCommit) != "" {
		baseCommit = strings.TrimSpace(baseCommit)
		return []string{"sh", "-lc", fmt.Sprintf("%s && git -C %s checkout --detach %s && git -C %s reset --hard %s", gitEnsureRevisionScript(repoTargetPath, baseCommit), shellQuote(repoTargetPath), shellQuote(baseCommit), shellQuote(repoTargetPath), shellQuote(baseCommit))}
	}
	if strings.TrimSpace(branch) != "" {
		return []string{"sh", "-lc", fmt.Sprintf("git -C %s checkout -B %s origin/%s && git -C %s reset --hard origin/%s", shellQuote(repoTargetPath), shellQuote(branch), shellQuote(branch), shellQuote(repoTargetPath), shellQuote(branch))}
	}
	return []string{"sh", "-lc", fmt.Sprintf("default_branch=$(git -C %s symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || true); default_branch=${default_branch#origin/}; if [ -n \"$default_branch\" ]; then git -C %s checkout -B \"$default_branch\" \"origin/$default_branch\" && git -C %s reset --hard \"origin/$default_branch\"; else git -C %s reset --hard FETCH_HEAD; fi", shellQuote(repoTargetPath), shellQuote(repoTargetPath), shellQuote(repoTargetPath), shellQuote(repoTargetPath))}
}

func gitEnsureRevisionScript(repoTargetPath string, revision string) string {
	revision = strings.TrimSpace(revision)
	return fmt.Sprintf("if ! git -C %s cat-file -e %s 2>/dev/null; then git -C %s fetch --no-tags origin %s; fi", shellQuote(repoTargetPath), shellQuote(revision+"^{commit}"), shellQuote(repoTargetPath), shellQuote(revision))
}

func sandboxGitTargetCleanupCommand(repoTargetPath string) []string {
	return []string{"sh", "-lc", fmt.Sprintf("rm -rf %s && mkdir -p %s", shellQuote(repoTargetPath), shellQuote(path.Dir(repoTargetPath)))}
}

func sandboxGitSwapRepoCommand(repoTargetPath string, tempRepoPath string) []string {
	backupPath := repoTargetPath + ".backup"
	return []string{"sh", "-lc", fmt.Sprintf(`set -e
target=%s
tmp=%s
backup=%s
rm -rf "$backup"
if [ -e "$target" ]; then mv "$target" "$backup"; fi
trap 'status=$?; if [ -e "$backup" ] && [ ! -e "$target" ]; then mv "$backup" "$target"; fi; exit $status' EXIT INT TERM
mv "$tmp" "$target"
rm -rf "$backup"
trap - EXIT INT TERM`, shellQuote(repoTargetPath), shellQuote(tempRepoPath), shellQuote(backupPath))}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func dockerCLIStatusState(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.HasPrefix(normalized, "up "):
		return "running"
	case strings.HasPrefix(normalized, "exited"):
		return "exited"
	case strings.HasPrefix(normalized, "created"):
		return "created"
	case strings.HasPrefix(normalized, "restarting"):
		return "restarting"
	case strings.HasPrefix(normalized, "paused"):
		return "paused"
	default:
		return normalized
	}
}

func parseDockerLabels(raw string) map[string]string {
	labels := map[string]string{}
	for _, entry := range strings.Split(strings.TrimSpace(raw), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			labels[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return labels
}

func containerWorkloadKind(labels map[string]string) string {
	if sandboxID := strings.TrimSpace(labels[labelOpenSandboxSandboxID]); sandboxID != "" {
		return managedKindSandbox
	}
	if managedID := strings.TrimSpace(labels[labelOpenSandboxManagedID]); managedID != "" {
		if kind := strings.TrimSpace(labels[labelOpenSandboxKind]); kind != "" {
			return kind
		}
	}
	if projectName := strings.TrimSpace(labels["com.docker.compose.project"]); projectName != "" {
		return "compose"
	}
	return "runtime"
}

func containerResettable(labels map[string]string) bool {
	return strings.TrimSpace(labels["com.docker.compose.project"]) != "" || (strings.TrimSpace(labels[labelOpenSandboxKind]) == managedKindDirect && strings.TrimSpace(labels[labelOpenSandboxManagedID]) != "")
}

func containerWorkloadID(containerID string, labels map[string]string, names []string) string {
	if sandboxID := strings.TrimSpace(labels[labelOpenSandboxSandboxID]); sandboxID != "" {
		return sandboxID
	}
	if managedID := strings.TrimSpace(labels[labelOpenSandboxManagedID]); managedID != "" {
		return managedID
	}
	projectName := strings.TrimSpace(labels["com.docker.compose.project"])
	if projectName == "" {
		return containerID
	}
	parts := []string{"compose", projectName}
	if serviceName := strings.TrimSpace(labels["com.docker.compose.service"]); serviceName != "" {
		parts = append(parts, serviceName)
	}
	if len(names) > 0 && strings.TrimSpace(names[0]) != "" {
		parts = append(parts, strings.TrimSpace(names[0]))
	}
	return strings.Join(parts, ":")
}

func parseDockerCLIPorts(raw string) []PortSummary {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	out := make([]PortSummary, 0)
	seen := map[string]struct{}{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if matches := dockerCLIPortMappingPattern.FindStringSubmatch(entry); len(matches) == 4 {
			publicPort, _ := strconv.Atoi(matches[1])
			privatePort, _ := strconv.Atoi(matches[2])
			key := fmt.Sprintf("%d-%d-%s", publicPort, privatePort, matches[3])
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, PortSummary{Private: privatePort, Public: publicPort, Type: matches[3]})
			continue
		}

		if matches := dockerCLIPrivatePortPattern.FindStringSubmatch(entry); len(matches) == 3 {
			privatePort, _ := strconv.Atoi(matches[1])
			key := fmt.Sprintf("0-%d-%s", privatePort, matches[2])
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, PortSummary{Private: privatePort, Type: matches[2]})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Private != out[j].Private {
			return out[i].Private < out[j].Private
		}
		return out[i].Public < out[j].Public
	})

	return out
}

var nowUnix = func() int64 {
	return timeNow().Unix()
}

var timeNow = func() time.Time {
	return time.Now().UTC()
}

func timeNowUnix() int64 {
	return nowUnix()
}
