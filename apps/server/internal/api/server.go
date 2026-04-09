package api

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type DockerAPI interface {
	ImageBuild(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error)
	ImageInspect(ctx context.Context, imageID string, inspectOpts ...client.ImageInspectOption) (image.InspectResponse, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	ImageRemove(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error)
	ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (dockertypes.HijackedResponse, error)
	ContainerExecResize(ctx context.Context, execID string, options container.ResizeOptions) error
	ContainerExecStart(ctx context.Context, execID string, config container.ExecStartOptions) error
	ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	CopyFromContainer(ctx context.Context, containerID, srcPath string) (io.ReadCloser, container.PathStat, error)
	CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error
	VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
}

type SandboxStore interface {
	CreateSandbox(ctx context.Context, sandbox store.Sandbox) error
	ListSandboxes(ctx context.Context) ([]store.Sandbox, error)
	GetSandbox(ctx context.Context, sandboxID string) (store.Sandbox, error)
	UpdateSandboxRuntime(ctx context.Context, sandboxID string, containerID string, env []string, secretEnv []string, secretEnvKeys []string, status string) error
	UpdateSandboxProxyConfig(ctx context.Context, sandboxID string, proxyConfig map[int]traefikcfg.ServiceProxyConfig) error
	UpdateSandboxStatus(ctx context.Context, sandboxID string, status string) error
	UpdateSandboxStatusByContainerID(ctx context.Context, containerID string, status string) error
	DeleteSandbox(ctx context.Context, sandboxID string) error
	DeleteSandboxByContainerID(ctx context.Context, containerID string) error
}

type UserStore interface {
	HasUsers(ctx context.Context) (bool, error)
	CreateUser(ctx context.Context, user store.UserRecord) (store.User, error)
	EnsureUser(ctx context.Context, user store.UserRecord) (store.User, error)
	GetUserByUsername(ctx context.Context, username string) (store.UserRecord, error)
	GetUserByID(ctx context.Context, userID string) (store.UserRecord, error)
	CreateAPIKey(ctx context.Context, key store.APIKeyRecord) error
	GetAPIKeyByHash(ctx context.Context, keyHash string) (store.APIKeyRecord, error)
	ListAPIKeysByUser(ctx context.Context, userID string) ([]store.APIKeyRecord, error)
	RevokeAPIKey(ctx context.Context, keyID string, userID string, revokedAt int64) error
	ListUsers(ctx context.Context) ([]store.User, error)
	UpdateUserPasswordHash(ctx context.Context, userID string, passwordHash string) error
	DeleteUser(ctx context.Context, userID string) error
	CreateRefreshToken(ctx context.Context, token store.RefreshTokenRecord) error
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (store.RefreshTokenRecord, error)
	RotateRefreshToken(ctx context.Context, currentTokenID string, replacement store.RefreshTokenRecord, rotatedAt int64) error
	RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt int64) error
}

type WorkerStore interface {
	UpsertRuntimeWorker(ctx context.Context, worker store.RuntimeWorker) error
	GetRuntimeWorker(ctx context.Context, workerID string) (store.RuntimeWorker, error)
	ListRuntimeWorkers(ctx context.Context) ([]store.RuntimeWorker, error)
	TouchRuntimeWorkerHeartbeat(ctx context.Context, workerID string, observedAt int64, status string, advertiseAddress string, version string, labels map[string]string) error
}

type Server struct {
	docker           DockerAPI
	runtime          workloadRuntime
	auth             AuthConfig
	previewRouting   previewRoutingConfig
	router           *gin.Engine
	sandboxStore     SandboxStore
	userStore        UserStore
	workerStore      WorkerStore
	logger           *slog.Logger
	metrics          *operationalMetrics
	runtimeLimits    runtimeLimits
	workspaceRoot    string
	traefikWriter    *traefikcfg.ConfigWriter
	proxyAuthLimiter *proxyAuthRateLimiter
	execWaitTimeout  time.Duration
	gitCacheLocks    sync.Map
	secretEnvCodec   *sandboxSecretEnvCodec
	configErr        error
}

var commandRunner = runCommand
var commandRunnerInDir = runCommandInDir

type composeProjectContext struct {
	ProjectName string
	ProjectDir  string
	ComposeFile string
}

const (
	localRuntimeWorkerID          = "local"
	labelOpenSandboxManaged       = "open-sandbox.managed"
	labelOpenSandboxOwnerID       = "open-sandbox.owner_id"
	labelOpenSandboxOwnerUsername = "open-sandbox.owner_username"
	labelOpenSandboxKind          = "open-sandbox.kind"
	labelOpenSandboxManagedID     = "open-sandbox.managed_id"
	labelOpenSandboxSandboxID     = "open-sandbox.sandbox_id"
	labelOpenSandboxWorkerID      = "open-sandbox.worker_id"
	managedKindDirect             = "direct"
	managedKindSandbox            = "sandbox"
	composeOwnerMetadataFile      = ".owner.json"
)

type managedOwnerMetadata struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

type ErrorResponse struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Status int    `json:"status,omitempty"`
}

type BuildImageRequest struct {
	ContextPath       string            `json:"context_path"`
	Dockerfile        string            `json:"dockerfile"`
	DockerfileContent string            `json:"dockerfile_content"`
	ContextFiles      map[string]string `json:"context_files"`
	Tag               string            `json:"tag" binding:"required"`
	BuildArgs         map[string]string `json:"build_args"`
}

type PullImageRequest struct {
	Image string `json:"image" binding:"required"`
	Tag   string `json:"tag"`
}

type ImageSummary struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repo_tags"`
	Created  int64    `json:"created"`
	Size     int64    `json:"size"`
}

type ImageSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Official    bool   `json:"official"`
	Automated   bool   `json:"automated"`
}

type dockerSearchRecord struct {
	Name        string `json:"Name"`
	Description string `json:"Description"`
	StarCount   string `json:"StarCount"`
	IsOfficial  string `json:"IsOfficial"`
	IsAutomated string `json:"IsAutomated"`
}

type RemoveImageResponse struct {
	Deleted []image.DeleteResponse `json:"deleted"`
}

type ComposeRequest struct {
	Content       string   `json:"content" binding:"required"`
	ProjectName   string   `json:"project_name"`
	Services      []string `json:"services"`
	Volumes       bool     `json:"volumes"`
	RemoveOrphans bool     `json:"remove_orphans"`
}

type ComposeResponse struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

type ComposeStatusResponse struct {
	Services []ComposeStatusService `json:"services"`
	Raw      string                 `json:"raw"`
}

type ComposeStatusService struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	State   string `json:"state"`
}

type ComposeProjectPreviewResponse struct {
	ProjectName string                          `json:"project_name"`
	Services    []ComposeServicePreviewResponse `json:"services"`
}

type ComposeServicePreviewResponse struct {
	ServiceName string                      `json:"service_name"`
	Ports       []ComposePublishedPortEntry `json:"ports"`
}

type ComposePublishedPortEntry struct {
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port"`
	Type        string `json:"type"`
	IP          string `json:"ip,omitempty"`
	PreviewURL  string `json:"preview_url"`
}

type GitCloneRequest struct {
	ContainerID  string `json:"container_id" binding:"required"`
	RepoURL      string `json:"repo_url" binding:"required"`
	TargetPath   string `json:"target_path" binding:"required"`
	Branch       string `json:"branch"`
	SingleBranch bool   `json:"single_branch"`
	Depth        *int   `json:"depth"`
	Filter       string `json:"filter"`
}

func validateCloneDepth(depth *int) (int, error) {
	if depth == nil {
		return 0, nil
	}
	if *depth <= 0 {
		return 0, errors.New("depth must be a positive integer")
	}
	return *depth, nil
}

func gitCloneCommand(repoURL, targetPath, branch string, singleBranch bool, depth int, filter string, referencePath string) []string {
	cmd := []string{"git", "clone"}
	if branch != "" {
		cmd = append(cmd, "--branch", branch)
	}
	if singleBranch {
		cmd = append(cmd, "--single-branch")
	}
	if depth > 0 {
		cmd = append(cmd, "--depth", strconv.Itoa(depth))
	}
	if filter != "" {
		cmd = append(cmd, "--filter", filter)
	}
	if referencePath != "" {
		cmd = append(cmd, "--reference-if-able", referencePath)
	}
	return append(cmd, repoURL, targetPath)
}

type CreateContainerRequest struct {
	Image      string   `json:"image" binding:"required"`
	Name       string   `json:"name"`
	Cmd        []string `json:"cmd"`
	Env        []string `json:"env"`
	Workdir    string   `json:"workdir"`
	TTY        bool     `json:"tty"`
	User       string   `json:"user"`
	Binds      []string `json:"binds"`
	Ports      []string `json:"ports"`
	AutoRemove bool     `json:"auto_remove"`
	Start      bool     `json:"start"`
}

type CreateContainerResponse struct {
	ID          string   `json:"id"`
	ContainerID string   `json:"container_id"`
	Warnings    []string `json:"warnings"`
	Started     bool     `json:"started"`
}

type ExecRequest struct {
	Cmd     []string `json:"cmd" binding:"required"`
	Workdir string   `json:"workdir"`
	Env     []string `json:"env"`
	Detach  bool     `json:"detach"`
	TTY     bool     `json:"tty"`
	User    string   `json:"user"`
}

type ExecResponse struct {
	ExecID   string `json:"exec_id"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Detached bool   `json:"detached"`
}

type terminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint   `json:"cols,omitempty"`
	Rows uint   `json:"rows,omitempty"`
}

func NewServer(dockerClient DockerAPI, authConfig AuthConfig) *Server {
	return NewServerWithStore(dockerClient, authConfig, nil)
}

func NewServerWithStore(dockerClient DockerAPI, authConfig AuthConfig, sandboxStore SandboxStore) *Server {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	workspaceRoot := resolveWorkspaceRoot()
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(requestLoggerMiddleware(logger))
	origins := loadAllowedOrigins()
	r.Use(cors.New(cors.Config{
		AllowOriginWithContextFunc: buildAllowOriginWithContextFunc(origins),
		AllowMethods:               []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:               []string{"Authorization", "X-API-Key", "Content-Type", "Accept", "X-Request-ID", "Traceparent", "Tracestate", "Baggage", "B3", "X-B3-TraceId", "X-B3-SpanId", "X-B3-ParentSpanId", "X-B3-Sampled", "X-B3-Flags", "Sentry-Trace"},
		ExposeHeaders:              []string{"X-Request-ID"},
		AllowCredentials:           true,
		MaxAge:                     12 * time.Hour,
	}))

	var userStore UserStore
	var workerStore WorkerStore
	if sandboxStore != nil {
		if configuredUserStore, ok := sandboxStore.(UserStore); ok {
			userStore = configuredUserStore
		}
		if configuredWorkerStore, ok := sandboxStore.(WorkerStore); ok {
			workerStore = configuredWorkerStore
		}
	}
	authConfig.UserStore = userStore

	secretEnvCodec, configErr := newSandboxSecretEnvCodecFromEnv()

	s := &Server{
		docker:           dockerClient,
		auth:             authConfig,
		previewRouting:   loadPreviewRoutingConfig(),
		router:           r,
		sandboxStore:     sandboxStore,
		userStore:        userStore,
		workerStore:      workerStore,
		logger:           logger,
		metrics:          newOperationalMetrics(),
		runtimeLimits:    loadRuntimeLimitsFromEnv(),
		workspaceRoot:    workspaceRoot,
		traefikWriter:    nil,
		proxyAuthLimiter: newProxyAuthRateLimiter(loadProxyAuthRateLimitConfig()),
		execWaitTimeout:  loadExecWaitTimeout(),
		secretEnvCodec:   secretEnvCodec,
		configErr:        configErr,
	}
	if traefikDir := strings.TrimSpace(os.Getenv("SANDBOX_TRAEFIK_DYNAMIC_CONFIG_DIR")); traefikDir != "" {
		writer, writerErr := traefikcfg.NewConfigWriter(traefikDir, traefikcfg.ConfigWriterOptions{
			AppHost:             s.previewRouting.AppHost,
			PreviewBaseDomain:   s.previewRouting.PreviewBaseDomain,
			PreviewCallbackPath: s.previewRouting.CallbackPath,
		})
		if writerErr != nil {
			logger.Warn("traefik_route_writer_disabled", slog.String("reason", writerErr.Error()))
		} else {
			s.traefikWriter = writer
		}
	}
	s.runtime = newDelegatingRuntime(workerStore, localRuntimeWorkerID, newDockerRuntime(localRuntimeWorkerID, dockerClient, func() string { return s.workspaceRoot }))
	s.ensureLocalWorkerRegistration(context.Background())
	s.registerRoutes()
	s.syncTraefikRoutes(context.Background())
	return s
}

func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) registerRoutes() {
	s.router.GET("/health", s.health)
	s.router.GET("/metrics", s.metricsHandler)
	s.router.GET("/auth/setup", s.setupStatus)
	s.router.POST("/auth/bootstrap", s.bootstrap)
	s.router.POST("/auth/login", s.login)
	s.router.POST("/auth/refresh", s.refresh)
	s.router.GET("/auth/session", s.session)
	s.router.GET(s.previewRouting.LaunchPathPrefix+"/*target", s.previewLaunch)
	s.router.GET(s.previewRouting.CallbackPath, s.previewAuthCallback)
	s.router.GET("/auth/proxy/authorize", s.proxyAuthorize)
	s.router.POST("/auth/logout", s.logout)
	workerControl := s.router.Group("/control")
	workerControl.Use(s.workerAuthMiddleware())
	{
		workerControl.POST("/workers/register", s.registerWorker)
		workerControl.POST("/workers/:id/heartbeat", s.heartbeatWorker)
	}
	secured := s.router.Group("/")
	secured.Use(s.auth.Middleware())

	api := secured.Group("/api")
	{
		api.GET("/users", s.listUsers)
		api.POST("/users", s.createUser)
		api.POST("/users/:id/password", s.updateUserPassword)
		api.DELETE("/users/:id", s.deleteUser)

		api.GET("/api-keys", s.listAPIKeys)
		api.POST("/api-keys", s.createAPIKey)
		api.DELETE("/api-keys/:id", s.revokeAPIKey)

		api.POST("/images/build/stream", s.buildImageStream)
		api.POST("/images/build", s.buildImage)
		api.POST("/images/pull", s.pullImage)
		api.GET("/images/search", s.searchImages)
		api.GET("/images", s.listImages)
		api.DELETE("/images/:id", s.removeImage)

		api.POST("/compose/up", s.composeUp)
		api.POST("/compose/down", s.composeDown)
		api.POST("/compose/status", s.composeStatus)
		api.GET("/compose/projects", s.listComposeProjects)
		api.GET("/compose/projects/:projectName", s.getComposeProject)

		api.POST("/git/clone", s.gitClone)

		api.GET("/containers", s.listContainers)
		api.POST("/containers/create", s.createContainer)
		api.POST("/containers/:id/restart", s.restartContainer)
		api.POST("/containers/:id/reset", s.resetContainer)
		api.POST("/containers/:id/stop", s.stopContainer)
		api.DELETE("/containers/:id", s.removeContainer)
		api.POST("/containers/:id/exec", s.execInContainer)
		api.GET("/containers/:id/terminal/ws", s.streamContainerTerminal)
		api.GET("/containers/:id/logs", s.streamLogs)
		api.GET("/containers/:id/files", s.readContainerFile)
		api.PUT("/containers/:id/files", s.writeContainerFile)

		api.POST("/sandboxes", s.createSandbox)
		api.POST("/sandboxes/stream", s.createSandboxStream)
		api.GET("/sandboxes", s.listSandboxes)
		api.GET("/sandboxes/:id", s.getSandbox)
		api.PATCH("/sandboxes/:id/env", s.updateSandboxEnv)
		api.PATCH("/sandboxes/:id/proxy-config", s.updateSandboxProxyConfig)
		api.POST("/sandboxes/:id/restart", s.restartSandbox)
		api.POST("/sandboxes/:id/reset", s.resetSandbox)
		api.POST("/sandboxes/:id/reset/stream", s.resetSandboxStream)
		api.POST("/sandboxes/:id/stop", s.stopSandbox)
		api.DELETE("/sandboxes/:id", s.deleteSandbox)
		api.POST("/sandboxes/:id/exec", s.execInSandbox)
		api.GET("/sandboxes/:id/terminal/ws", s.streamSandboxTerminal)
		api.GET("/sandboxes/:id/logs", s.streamSandboxLogs)
		api.GET("/sandboxes/:id/files", s.readSandboxFile)
		api.PUT("/sandboxes/:id/files", s.writeSandboxFile)

		api.GET("/admin/workers", s.listWorkers)
		api.GET("/admin/traefik/routes", s.getTraefikRouteState)
		api.POST("/admin/maintenance/cleanup", s.runMaintenanceCleanup)
		api.POST("/admin/maintenance/reconcile", s.runMaintenanceReconcile)
	}

	secured.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

// searchImages godoc
// @Summary Search Docker Hub images
// @Description Searches Docker Hub using docker search
// @Tags images
// @Produce json
// @Param q query string true "Search query"
// @Param limit query int false "Maximum results"
// @Success 200 {array} ImageSearchResult
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/images/search [get]
func (s *Server) searchImages(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		writeError(c, http.StatusBadRequest, errors.New("query parameter q is required"))
		return
	}

	limit, err := parseSearchLimit(c.DefaultQuery("limit", "25"))
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	stdout, stderr, err := commandRunner(c.Request.Context(), "docker", "search", query, "--limit", strconv.Itoa(limit), "--format", "{{json .}}")
	if err != nil {
		writeErrorWithDetails(c, http.StatusInternalServerError, "image search failed", "command_failed", strings.TrimSpace(stderr))
		return
	}

	results, err := parseDockerSearchOutput(stdout)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, results)
}

// health godoc
// @Summary Health check
// @Description Returns service status
// @Tags system
// @Success 200 {object} map[string]string
// @Router /health [get]
func (s *Server) health(c *gin.Context) {
	if s.configErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "error": s.configErr.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// buildImage godoc
// @Summary Build Docker image
// @Description Builds an image from a local context path and Dockerfile
// @Tags images
// @Accept json
// @Produce json
// @Param payload body BuildImageRequest true "Build request"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/images/build [post]
func (s *Server) buildImage(c *gin.Context) {
	var req BuildImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	contextReader, buildOptions, err := s.prepareBuildRequest(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	defer contextReader.Close()

	resp, err := s.docker.ImageBuild(c.Request.Context(), contextReader, buildOptions)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	defer resp.Body.Close()

	output, err := collectBuildOutput(resp.Body)
	if err != nil {
		writeError(c, http.StatusInternalServerError, fmt.Errorf("docker build failed: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"output": output, "image": req.Tag})
}

func (s *Server) buildImageStream(c *gin.Context) {
	var req BuildImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	contextReader, buildOptions, err := s.prepareBuildRequest(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	defer contextReader.Close()

	resp, err := s.docker.ImageBuild(c.Request.Context(), contextReader, buildOptions)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	defer resp.Body.Close()

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	setSSEHeaders(c)

	mu := &sync.Mutex{}
	if err := streamDockerBuildOutput(c, mu, resp.Body); err != nil {
		emitSSE(c, mu, "error", err.Error())
		flusher.Flush()
		return
	}

	emitSSE(c, mu, "done", req.Tag)
	flusher.Flush()
}

// pullImage godoc
// @Summary Pull Docker image
// @Description Pulls an image from registry
// @Tags images
// @Accept json
// @Produce json
// @Param payload body PullImageRequest true "Pull request"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/images/pull [post]
func (s *Server) pullImage(c *gin.Context) {
	var req PullImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	ref := req.Image
	if req.Tag != "" {
		ref = fmt.Sprintf("%s:%s", req.Image, req.Tag)
	}

	resp, err := s.docker.ImagePull(c.Request.Context(), ref, image.PullOptions{})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	defer resp.Close()

	output, err := io.ReadAll(resp)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"output": string(output), "image": ref})
}

// listImages godoc
// @Summary List local Docker images
// @Description Returns all local images
// @Tags images
// @Produce json
// @Success 200 {array} ImageSummary
// @Failure 500 {object} ErrorResponse
// @Router /api/images [get]
func (s *Server) listImages(c *gin.Context) {
	images, err := s.docker.ImageList(c.Request.Context(), image.ListOptions{})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	out := make([]ImageSummary, 0, len(images))
	for _, img := range images {
		out = append(out, ImageSummary{
			ID:       img.ID,
			RepoTags: img.RepoTags,
			Created:  img.Created,
			Size:     img.Size,
		})
	}

	c.JSON(http.StatusOK, out)
}

// removeImage godoc
// @Summary Remove Docker image
// @Description Removes image by ID or tag
// @Tags images
// @Produce json
// @Param id path string true "Image ID or tag"
// @Param force query bool false "Force remove"
// @Success 200 {object} RemoveImageResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/images/{id} [delete]
func (s *Server) removeImage(c *gin.Context) {
	force := false
	forceQuery := strings.TrimSpace(c.Query("force"))
	if forceQuery != "" {
		parsed, err := strconv.ParseBool(forceQuery)
		if err != nil {
			writeError(c, http.StatusBadRequest, fmt.Errorf("invalid force query value %q", forceQuery))
			return
		}
		force = parsed
	}

	deleted, err := s.docker.ImageRemove(c.Request.Context(), c.Param("id"), image.RemoveOptions{Force: force, PruneChildren: true})
	if err != nil {
		switch {
		case errdefs.IsNotFound(err) || isMissingImageError(err):
			writeError(c, http.StatusNotFound, err)
		case errdefs.IsConflict(err):
			writeError(c, http.StatusConflict, err)
		case errdefs.IsInvalidParameter(err):
			writeError(c, http.StatusBadRequest, err)
		default:
			writeError(c, http.StatusInternalServerError, err)
		}
		return
	}

	c.JSON(http.StatusOK, RemoveImageResponse{Deleted: deleted})
}

// composeUp godoc
// @Summary Docker Compose up
// @Description Runs docker compose up -d
// @Tags compose
// @Accept json
// @Produce text/event-stream
// @Param payload body ComposeRequest true "Compose request"
// @Success 200 {string} string "SSE stream"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/compose/up [post]
func (s *Server) composeUp(c *gin.Context) {
	var req ComposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}

	existingProject := s.composeProjectContextForName(composeProjectName(req.ProjectName, req.Content))
	shouldWriteOwner, err := s.authorizeComposeProjectAccess(identity, existingProject)
	if err != nil {
		writeError(c, http.StatusNotFound, err)
		return
	}

	project, err := s.prepareComposeProject(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	if shouldWriteOwner {
		if err := s.writeComposeProjectOwnerMetadata(project.ProjectDir, identity); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}

	args := buildComposeArgs(project, req, "up", "-d")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	cmd := exec.CommandContext(c.Request.Context(), "docker", args...)
	cmd.Dir = project.ProjectDir
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if err := cmd.Start(); err != nil {
		writeErrorWithDetails(c, http.StatusInternalServerError, "compose up failed", "command_start_failed", "")
		return
	}

	setSSEHeaders(c)

	mu := &sync.Mutex{}
	stdoutWriter := &sseChunkWriter{ctx: c, stream: "stdout", mu: mu}
	stderrWriter := &sseChunkWriter{ctx: c, stream: "stderr", mu: mu}

	copyErrs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(stdoutWriter, stdoutPipe); copyErr != nil {
			copyErrs <- copyErr
		}
		stdoutWriter.FlushRemainder()
	}()

	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(stderrWriter, stderrPipe); copyErr != nil {
			copyErrs <- copyErr
		}
		stderrWriter.FlushRemainder()
	}()

	wg.Wait()
	close(copyErrs)
	for copyErr := range copyErrs {
		emitSSE(c, mu, "error", fmt.Sprintf("stream copy failed: %v", copyErr))
	}

	if err := cmd.Wait(); err != nil {
		emitSSE(c, mu, "error", "compose up failed")
		emitSSE(c, mu, "error", err.Error())
		flusher.Flush()
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	emitSSE(c, mu, "done", "compose up completed")
	flusher.Flush()
}

// composeDown godoc
// @Summary Docker Compose down
// @Description Runs docker compose down
// @Tags compose
// @Accept json
// @Produce json
// @Param payload body ComposeRequest true "Compose request"
// @Success 200 {object} ComposeResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/compose/down [post]
func (s *Server) composeDown(c *gin.Context) {
	var req ComposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}

	existingProject := s.composeProjectContextForName(composeProjectName(req.ProjectName, req.Content))
	shouldWriteOwner, err := s.authorizeComposeProjectAccess(identity, existingProject)
	if err != nil {
		writeError(c, http.StatusNotFound, err)
		return
	}

	project, err := s.prepareComposeProject(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	if shouldWriteOwner {
		if err := s.writeComposeProjectOwnerMetadata(project.ProjectDir, identity); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}

	args := buildComposeArgs(project, req, "down")
	stdout, stderr, err := commandRunnerInDir(c.Request.Context(), project.ProjectDir, "docker", args...)
	if err != nil {
		writeErrorWithDetails(c, http.StatusInternalServerError, "compose down failed", "command_failed", strings.TrimSpace(stderr))
		return
	}
	if err := s.removeComposeProjectArtifacts(project); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	c.JSON(http.StatusOK, ComposeResponse{Stdout: stdout, Stderr: stderr})
}

func (s *Server) removeComposeProjectArtifacts(project composeProjectContext) error {
	composeRoot := filepath.Clean(filepath.Join(s.workspaceRoot, ".open-sandbox", "compose"))
	projectDir := filepath.Clean(project.ProjectDir)
	if projectDir == composeRoot {
		return errors.New("refusing to remove compose root")
	}
	rel, err := filepath.Rel(composeRoot, projectDir)
	if err != nil {
		return fmt.Errorf("resolve compose project path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("compose project path escapes managed root")
	}
	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("remove compose project artifacts: %w", err)
	}
	return nil
}

// composeStatus godoc
// @Summary Docker Compose status
// @Description Runs docker compose ps --format json
// @Tags compose
// @Accept json
// @Produce json
// @Param payload body ComposeRequest true "Compose request"
// @Success 200 {object} ComposeStatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/compose/status [post]
func (s *Server) composeStatus(c *gin.Context) {
	var req ComposeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}

	existingProject := s.composeProjectContextForName(composeProjectName(req.ProjectName, req.Content))
	shouldWriteOwner, err := s.authorizeComposeProjectAccess(identity, existingProject)
	if err != nil {
		writeError(c, http.StatusNotFound, err)
		return
	}

	project, err := s.prepareComposeProject(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}
	if shouldWriteOwner {
		if err := s.writeComposeProjectOwnerMetadata(project.ProjectDir, identity); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}

	args := buildComposeArgs(project, req, "ps", "--format", "json")
	stdout, stderr, err := commandRunnerInDir(c.Request.Context(), project.ProjectDir, "docker", args...)
	if err != nil {
		writeErrorWithDetails(c, http.StatusInternalServerError, "compose status failed", "command_failed", strings.TrimSpace(stderr))
		return
	}

	services := parseComposeStatusServices(stdout)
	c.JSON(http.StatusOK, ComposeStatusResponse{Raw: stdout, Services: services})
}

func (s *Server) listComposeProjects(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	containers, err := s.visibleManagedContainers(c.Request.Context(), identity)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	projectNames := make(map[string]struct{})
	for _, item := range containers {
		projectName := strings.TrimSpace(item.ProjectName)
		if projectName == "" {
			projectName = strings.TrimSpace(item.Labels["com.docker.compose.project"])
		}
		if projectName == "" {
			continue
		}
		projectNames[projectName] = struct{}{}
	}

	projects := make([]ComposeProjectPreviewResponse, 0, len(projectNames))
	for projectName := range projectNames {
		projects = append(projects, s.buildComposeProjectPreview(projectName, containers))
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].ProjectName < projects[j].ProjectName
	})

	c.JSON(http.StatusOK, projects)
}

func (s *Server) getComposeProject(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	s.syncTraefikRoutes(c.Request.Context())

	projectName := sanitizeComposeProjectName(strings.TrimSpace(c.Param("projectName")))
	if projectName == "" {
		writeError(c, http.StatusBadRequest, errors.New("compose project name is required"))
		return
	}

	project, err := s.existingComposeProject(projectName)
	if err != nil {
		writeError(c, http.StatusNotFound, errors.New("compose project not found"))
		return
	}
	if _, err := s.authorizeComposeProjectAccess(identity, project); err != nil {
		writeError(c, http.StatusNotFound, errors.New("compose project not found"))
		return
	}

	containers, err := s.visibleManagedContainers(c.Request.Context(), identity)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	preview := s.buildComposeProjectPreview(project.ProjectName, containers)
	if len(preview.Services) == 0 {
		writeError(c, http.StatusNotFound, errors.New("compose project not found"))
		return
	}

	c.JSON(http.StatusOK, preview)
}

func (s *Server) visibleManagedContainers(ctx context.Context, identity AuthIdentity) ([]ContainerSummary, error) {
	containers, err := s.runtime.ListWorkloads(ctx)
	if err != nil {
		return nil, err
	}
	managedComposeProjects, err := s.managedComposeProjects()
	if err != nil {
		return nil, err
	}
	containers = s.filterManagedRuntimeContainers(containers, managedComposeProjects)
	ownedContainerIDs, err := s.ownedRuntimeContainerIDs(ctx, identity.UserID)
	if err != nil {
		return nil, err
	}
	ownedComposeProjects, err := s.ownedComposeProjects(identity.UserID)
	if err != nil {
		return nil, err
	}

	visible := make([]ContainerSummary, 0, len(containers))
	for _, item := range containers {
		if s.runtimeContainerVisibleToIdentity(item, identity, ownedContainerIDs, ownedComposeProjects) {
			item.PreviewURLs = s.previewURLsForContainer(item)
			visible = append(visible, item)
		}
	}

	return visible, nil
}

func (s *Server) buildComposeProjectPreview(projectName string, containers []ContainerSummary) ComposeProjectPreviewResponse {
	servicesByName := map[string]*ComposeServicePreviewResponse{}
	for _, item := range containers {
		itemProjectName := strings.TrimSpace(item.ProjectName)
		if itemProjectName == "" {
			itemProjectName = strings.TrimSpace(item.Labels["com.docker.compose.project"])
		}
		if itemProjectName != projectName {
			continue
		}
		serviceName := strings.TrimSpace(item.ServiceName)
		if serviceName == "" {
			serviceName = strings.TrimSpace(item.Labels["com.docker.compose.service"])
		}
		if serviceName == "" {
			continue
		}

		service := servicesByName[serviceName]
		if service == nil {
			service = &ComposeServicePreviewResponse{ServiceName: serviceName, Ports: make([]ComposePublishedPortEntry, 0, len(item.Ports))}
			servicesByName[serviceName] = service
		}

		seen := make(map[string]struct{}, len(service.Ports))
		for _, port := range service.Ports {
			seen[fmt.Sprintf("%d/%d/%s", port.PrivatePort, port.PublicPort, port.Type)] = struct{}{}
		}
		for _, port := range item.Ports {
			if port.Private <= 0 || port.Public <= 0 {
				continue
			}
			key := fmt.Sprintf("%d/%d/%s", port.Private, port.Public, port.Type)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			service.Ports = append(service.Ports, ComposePublishedPortEntry{
				PrivatePort: port.Private,
				PublicPort:  port.Public,
				Type:        port.Type,
				IP:          port.IP,
				PreviewURL:  s.previewLaunchURLForComposeService(projectName, serviceName, port.Private),
			})
		}
	}

	services := make([]ComposeServicePreviewResponse, 0, len(servicesByName))
	for _, service := range servicesByName {
		sort.Slice(service.Ports, func(i, j int) bool {
			if service.Ports[i].PrivatePort != service.Ports[j].PrivatePort {
				return service.Ports[i].PrivatePort < service.Ports[j].PrivatePort
			}
			if service.Ports[i].PublicPort != service.Ports[j].PublicPort {
				return service.Ports[i].PublicPort < service.Ports[j].PublicPort
			}
			return service.Ports[i].Type < service.Ports[j].Type
		})
		services = append(services, *service)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].ServiceName < services[j].ServiceName
	})

	return ComposeProjectPreviewResponse{ProjectName: projectName, Services: services}
}

func parseComposeStatusServices(raw string) []ComposeStatusService {
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return []ComposeStatusService{}
	}

	services := make([]ComposeStatusService, 0, len(parsed))
	for _, item := range parsed {
		service := ComposeStatusService{}
		if value, ok := item["Name"].(string); ok {
			service.Name = strings.TrimSpace(value)
		}
		if value, ok := item["Service"].(string); ok {
			service.Service = strings.TrimSpace(value)
		}
		if value, ok := item["State"].(string); ok {
			service.State = strings.TrimSpace(value)
		}
		services = append(services, service)
	}

	return services
}

// gitClone godoc
// @Summary Clone git repository inside container
// @Description Executes git clone inside a running container
// @Tags git
// @Accept json
// @Produce json
// @Param payload body GitCloneRequest true "Clone request"
// @Success 200 {object} ExecResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/git/clone [post]
func (s *Server) gitClone(c *gin.Context) {
	var req GitCloneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	depth, err := validateCloneDepth(req.Depth)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	cmd := gitCloneCommand(req.RepoURL, req.TargetPath, strings.TrimSpace(req.Branch), req.SingleBranch, depth, strings.TrimSpace(req.Filter), "")

	execResp, err := s.runContainerExec(c.Request.Context(), localRuntimeWorkerID, req.ContainerID, ExecRequest{Cmd: cmd})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, execResp)
}

// createContainer godoc
// @Summary Create container directly
// @Description Creates a Docker container and optionally starts it. If the image is missing locally, it is pulled automatically and create is retried.
// @Tags containers
// @Accept json
// @Produce json
// @Param payload body CreateContainerRequest true "Create container request"
// @Success 200 {object} CreateContainerResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/containers/create [post]
func (s *Server) createContainer(c *gin.Context) {
	var req CreateContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}
	managedID := newManagedResourceID("ctr")
	if err := s.writeDirectContainerSpec(managedID, req); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	containerConfig, hostConfig, err := buildDirectContainerConfigs(req, identity.UserID, identity.Username, managedID, localRuntimeWorkerID)
	if err != nil {
		_ = os.Remove(s.directContainerSpecPath(managedID))
		writeError(c, http.StatusBadRequest, err)
		return
	}
	s.runtimeLimits.apply(hostConfig)

	created, err := s.createContainerWithAutoPull(c.Request.Context(), localRuntimeWorkerID, req.Image, containerConfig, hostConfig, req.Name)
	if err != nil {
		_ = os.Remove(s.directContainerSpecPath(managedID))
		s.logLifecycleFailure("create_container", err, slog.String("managed_id", managedID), slog.String("image", req.Image))
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	started := false
	if req.Start {
		if err := s.runtime.StartWorkload(c.Request.Context(), localRuntimeWorkerID, created.ID, container.StartOptions{}); err != nil {
			s.logLifecycleFailure("start_container", err, slog.String("container_id", created.ID), slog.String("managed_id", managedID))
			writeError(c, http.StatusInternalServerError, fmt.Errorf("start container: %w", err))
			return
		}
		started = true
	}
	s.syncTraefikRoutes(c.Request.Context())
	s.logLifecycleSuccess("create_container", slog.String("container_id", created.ID), slog.String("managed_id", managedID), slog.Bool("started", started))

	c.JSON(http.StatusOK, CreateContainerResponse{ID: managedID, ContainerID: created.ID, Warnings: created.Warnings, Started: started})
}

func isMissingImageError(err error) bool {
	if errdefs.IsNotFound(err) {
		return true
	}

	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "no such image") || strings.Contains(errText, "not found")
}

func isMissingWorkloadError(err error) bool {
	if errdefs.IsNotFound(err) {
		return true
	}

	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "no such container") || strings.Contains(errText, "container not found")
}

// execInContainer godoc
// @Summary Execute command in container
// @Description Runs a command inside a running container
// @Tags containers
// @Accept json
// @Produce json
// @Param id path string true "Container ID"
// @Param payload body ExecRequest true "Exec request"
// @Success 200 {object} ExecResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/containers/{id}/exec [post]
func (s *Server) execInContainer(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	var req ExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	resp, err := s.runContainerExec(c.Request.Context(), s.workerIDForContainerSummary(target), target.ContainerID, req)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// streamLogs godoc
// @Summary Stream container logs
// @Description Streams stdout/stderr logs over SSE
// @Tags containers
// @Produce text/event-stream
// @Param id path string true "Container ID"
// @Param follow query bool false "Follow logs" default(true)
// @Param tail query string false "Tail lines" default(100)
// @Success 200 {string} string "SSE stream"
// @Failure 500 {object} ErrorResponse
// @Router /api/containers/{id}/logs [get]
func (s *Server) streamLogs(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	follow, _ := strconv.ParseBool(c.DefaultQuery("follow", "true"))
	tail := c.DefaultQuery("tail", "100")
	s.streamLogsForContainer(c, s.workerIDForContainerSummary(target), target.ContainerID, follow, tail)
}

func (s *Server) streamContainerTerminal(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	workdir := strings.TrimSpace(c.Query("workdir"))
	s.streamTerminalForContainer(c, s.workerIDForContainerSummary(target), target.ContainerID, workdir)
}

func (s *Server) streamTerminalForContainer(c *gin.Context, workerID string, containerID string, workdir string) {
	cols := parseTerminalDimension(c.Query("cols"), 120)
	rows := parseTerminalDimension(c.Query("rows"), 32)

	allowOrigin := buildAllowOriginFunc(loadAllowedOrigins())
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				return true
			}
			return allowOrigin(origin) ||
				requestOriginMatchesForwardedHost(r, origin) ||
				requestOriginHostMatchesForwardedHost(r, origin)
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logStreamFailure("terminal_websocket_upgrade", err, slog.String("container_id", containerID))
		return
	}
	conn.SetReadLimit(1 << 20)

	execID, attached, err := s.startInteractiveExec(c.Request.Context(), workerID, containerID, workdir, cols, rows)
	if err != nil {
		s.logStreamFailure("terminal_exec", err, slog.String("container_id", containerID))
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "terminal setup failed"),
			time.Now().Add(time.Second),
		)
		_ = conn.Close()
		return
	}

	var cleanup sync.Once
	cleanupSession := func() {
		attached.Close()
		_ = conn.Close()
	}

	go func() {
		buffer := make([]byte, 4096)
		for {
			count, readErr := attached.Reader.Read(buffer)
			if count > 0 {
				chunk := make([]byte, count)
				copy(chunk, buffer[:count])
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, chunk); writeErr != nil {
					cleanup.Do(cleanupSession)
					return
				}
			}

			if readErr != nil {
				cleanup.Do(cleanupSession)
				return
			}
		}
	}()

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			cleanup.Do(cleanupSession)
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var message terminalClientMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}

		switch message.Type {
		case "input":
			if message.Data == "" {
				continue
			}
			if _, err := attached.Conn.Write([]byte(message.Data)); err != nil {
				cleanup.Do(cleanupSession)
				return
			}
		case "resize":
			if message.Cols == 0 || message.Rows == 0 {
				continue
			}
			if err := s.runtime.ExecResize(c.Request.Context(), workerID, execID, container.ResizeOptions{Width: message.Cols, Height: message.Rows}); err != nil {
				cleanup.Do(cleanupSession)
				return
			}
		}
	}
}

func (s *Server) startInteractiveExec(
	ctx context.Context,
	workerID string,
	containerID string,
	workdir string,
	cols uint,
	rows uint,
) (string, dockertypes.HijackedResponse, error) {
	consoleSize := &[2]uint{rows, cols}
	execConfig := container.ExecOptions{
		Cmd:          defaultTerminalShellCommand(workdir),
		Env:          []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
		WorkingDir:   workdir,
		Tty:          true,
		ConsoleSize:  consoleSize,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	created, err := s.runtime.ExecCreate(ctx, workerID, containerID, execConfig)
	if err != nil {
		return "", dockertypes.HijackedResponse{}, fmt.Errorf("create interactive exec: %w", err)
	}

	attached, err := s.runtime.ExecAttach(ctx, workerID, created.ID, container.ExecAttachOptions{Tty: true, ConsoleSize: consoleSize})
	if err != nil {
		return "", dockertypes.HijackedResponse{}, fmt.Errorf("attach interactive exec: %w", err)
	}

	return created.ID, attached, nil
}

func defaultTerminalShellCommand(workdir string) []string {
	command := "export TERM=${TERM:-xterm-256color}; if command -v bash >/dev/null 2>&1; then exec bash -i; fi; exec sh -i"
	if trimmed := strings.TrimSpace(workdir); trimmed != "" {
		command = fmt.Sprintf("cd %s 2>/dev/null || true; %s", shellQuote(trimmed), command)
	}
	return []string{"sh", "-lc", command}
}

func parseTerminalDimension(raw string, fallback uint) uint {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || value == 0 {
		return fallback
	}

	return uint(value)
}

func (s *Server) runContainerExec(ctx context.Context, workerID string, containerID string, req ExecRequest) (ExecResponse, error) {
	execConfig := container.ExecOptions{
		Cmd:          req.Cmd,
		User:         req.User,
		WorkingDir:   req.Workdir,
		Env:          req.Env,
		Tty:          req.TTY,
		AttachStdout: !req.Detach,
		AttachStderr: !req.Detach,
	}

	created, err := s.runtime.ExecCreate(ctx, workerID, containerID, execConfig)
	if err != nil {
		return ExecResponse{}, fmt.Errorf("create exec: %w", err)
	}

	if req.Detach {
		if err := s.runtime.ExecStart(ctx, workerID, created.ID, container.ExecStartOptions{Detach: true, Tty: req.TTY}); err != nil {
			return ExecResponse{}, fmt.Errorf("start detached exec: %w", err)
		}

		return ExecResponse{ExecID: created.ID, Detached: true}, nil
	}

	attached, err := s.runtime.ExecAttach(ctx, workerID, created.ID, container.ExecAttachOptions{Tty: req.TTY})
	if err != nil {
		return ExecResponse{}, fmt.Errorf("attach exec: %w", err)
	}
	defer attached.Close()

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	if req.TTY {
		if _, err := io.Copy(&stdoutBuf, attached.Reader); err != nil {
			return ExecResponse{}, fmt.Errorf("read tty output: %w", err)
		}
	} else {
		if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attached.Reader); err != nil {
			return ExecResponse{}, fmt.Errorf("read exec output: %w", err)
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, s.execWaitTimeout)
	defer cancel()

	inspect, err := s.waitForExec(waitCtx, workerID, created.ID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ExecResponse{}, fmt.Errorf("exec did not finish within %s", s.execWaitTimeout)
		}
		return ExecResponse{}, err
	}

	return ExecResponse{
		ExecID:   created.ID,
		ExitCode: inspect.ExitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Detached: false,
	}, nil
}

func (s *Server) waitForExec(ctx context.Context, workerID string, execID string) (container.ExecInspect, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		inspect, err := s.runtime.ExecInspect(ctx, workerID, execID)
		if err != nil {
			return container.ExecInspect{}, fmt.Errorf("inspect exec: %w", err)
		}

		if !inspect.Running {
			return inspect, nil
		}

		select {
		case <-ctx.Done():
			return container.ExecInspect{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

type sseChunkWriter struct {
	ctx    *gin.Context
	stream string
	mu     *sync.Mutex
	buf    string
}

func (w *sseChunkWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf += string(p)
	for {
		idx := strings.IndexByte(w.buf, '\n')
		if idx == -1 {
			break
		}

		line := strings.TrimRight(w.buf[:idx], "\r")
		w.buf = w.buf[idx+1:]
		if line == "" {
			continue
		}

		emitSSE(w.ctx, nil, w.stream, line)
	}

	return len(p), nil
}

func (w *sseChunkWriter) FlushRemainder() {
	w.mu.Lock()
	defer w.mu.Unlock()

	line := strings.TrimRight(w.buf, "\r")
	w.buf = ""
	if line == "" {
		return
	}

	emitSSE(w.ctx, nil, w.stream, line)
}

func emitSSE(c *gin.Context, mu *sync.Mutex, event string, data string) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		_, _ = fmt.Fprintf(c.Writer, "event: %s\n", event)
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", scanner.Text())
	}
	flusher.Flush()
}

func setSSEHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Accept-Encoding", "identity")
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Connection", "keep-alive")
	c.Header("Pragma", "no-cache")
	c.Header("X-Accel-Buffering", "no")
}

func buildComposeArgs(project composeProjectContext, req ComposeRequest, cmd string, extra ...string) []string {
	args := []string{"compose", "--project-name", project.ProjectName, "--project-directory", project.ProjectDir, "-f", project.ComposeFile}

	args = append(args, cmd)
	if cmd == "down" {
		if req.Volumes {
			args = append(args, "--volumes")
		}
		if req.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
	}
	args = append(args, extra...)
	if len(req.Services) > 0 && (cmd == "up" || cmd == "start" || cmd == "stop" || cmd == "restart") {
		args = append(args, req.Services...)
	}

	return args
}

func buildDirectContainerConfigs(req CreateContainerRequest, ownerID string, ownerUsername string, managedID string, workerID string) (*container.Config, *container.HostConfig, error) {
	containerConfig := &container.Config{
		Image:      req.Image,
		Cmd:        req.Cmd,
		Env:        req.Env,
		WorkingDir: req.Workdir,
		Tty:        req.TTY,
		User:       req.User,
		Labels: map[string]string{
			labelOpenSandboxManaged:       "true",
			labelOpenSandboxOwnerID:       ownerID,
			labelOpenSandboxOwnerUsername: ownerUsername,
			labelOpenSandboxKind:          managedKindDirect,
			labelOpenSandboxManagedID:     managedID,
			labelOpenSandboxWorkerID:      normalizeManagedWorkerID(workerID),
		},
	}
	hostConfig := &container.HostConfig{
		Binds:      req.Binds,
		AutoRemove: req.AutoRemove,
	}

	if len(req.Ports) > 0 {
		exposedPorts, portBindings, err := nat.ParsePortSpecs(req.Ports)
		if err != nil {
			return nil, nil, fmt.Errorf("parse ports: %w", err)
		}
		containerConfig.ExposedPorts = exposedPorts
		hostConfig.PortBindings = portBindings
	}

	return containerConfig, hostConfig, nil
}

func (s *Server) prepareComposeProject(req ComposeRequest) (composeProjectContext, error) {
	composeRoot := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose")
	if err := os.MkdirAll(composeRoot, 0o700); err != nil {
		return composeProjectContext{}, fmt.Errorf("create compose root: %w", err)
	}
	if err := os.Chmod(composeRoot, 0o700); err != nil {
		return composeProjectContext{}, fmt.Errorf("chmod compose root: %w", err)
	}
	hiddenRoot := filepath.Dir(composeRoot)
	if err := os.Chmod(hiddenRoot, 0o700); err != nil {
		return composeProjectContext{}, fmt.Errorf("chmod compose storage root: %w", err)
	}

	projectName := composeProjectName(req.ProjectName, req.Content)
	projectDir := filepath.Join(composeRoot, projectName)
	composeFile, err := writeManagedComposeFile(projectDir, req.Content)
	if err != nil {
		return composeProjectContext{}, err
	}

	return composeProjectContext{
		ProjectName: projectName,
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
	}, nil
}

func (s *Server) existingComposeProject(projectName string) (composeProjectContext, error) {
	return existingComposeProjectAt(s.workspaceRoot, projectName)
}

func (s *Server) composeProjectContextForName(projectName string) composeProjectContext {
	return composeProjectContext{
		ProjectName: projectName,
		ProjectDir:  filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", projectName),
		ComposeFile: filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", projectName, "docker-compose.yml"),
	}
}

func (s *Server) authorizeComposeProjectAccess(identity AuthIdentity, project composeProjectContext) (bool, error) {
	owner, hasOwner, err := s.readComposeProjectOwnerMetadata(project.ProjectDir)
	if err != nil {
		return false, err
	}
	if hasOwner {
		if identity.IsAdmin() || owner.UserID == identity.UserID {
			return false, nil
		}
		return false, errors.New("compose project not found")
	}
	if _, err := os.Stat(project.ComposeFile); err == nil {
		if identity.IsAdmin() {
			return true, nil
		}
		return false, errors.New("compose project not found")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect compose project: %w", err)
	}
	return true, nil
}

func composeProjectName(raw string, content string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		sanitized := sanitizeComposeProjectName(trimmed)
		if sanitized != "" {
			return sanitized
		}
	}

	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("compose-%x", sum[:6])
}

func sanitizeComposeProjectName(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	lastWasDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		case r == '-', r == '_':
			b.WriteRune(r)
			lastWasDash = false
		case !lastWasDash:
			b.WriteByte('-')
			lastWasDash = true
		}
	}

	return strings.Trim(b.String(), "-_")
}

func writeManagedComposeFile(projectDir string, content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", errors.New("compose content is required")
	}

	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return "", fmt.Errorf("create compose project dir: %w", err)
	}
	if err := os.Chmod(projectDir, 0o700); err != nil {
		return "", fmt.Errorf("chmod compose project dir: %w", err)
	}

	composeFile := filepath.Join(projectDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write compose file: %w", err)
	}

	return composeFile, nil
}

func (s *Server) writeComposeProjectOwnerMetadata(projectDir string, identity AuthIdentity) error {
	owner, hasOwner, err := s.readComposeProjectOwnerMetadata(projectDir)
	if err != nil {
		return err
	}
	if hasOwner {
		if owner.UserID != identity.UserID {
			return errors.New("compose project owner mismatch")
		}
		return nil
	}

	ownerFile := filepath.Join(projectDir, composeOwnerMetadataFile)
	payload, err := json.Marshal(managedOwnerMetadata{UserID: identity.UserID, Username: identity.Username})
	if err != nil {
		return fmt.Errorf("encode compose owner metadata: %w", err)
	}
	if err := os.WriteFile(ownerFile, payload, 0o600); err != nil {
		return fmt.Errorf("write compose owner metadata: %w", err)
	}
	return nil
}

func (s *Server) readComposeProjectOwnerMetadata(projectDir string) (managedOwnerMetadata, bool, error) {
	ownerFile := filepath.Join(projectDir, composeOwnerMetadataFile)
	payload, err := os.ReadFile(ownerFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return managedOwnerMetadata{}, false, nil
		}
		return managedOwnerMetadata{}, false, fmt.Errorf("read compose owner metadata: %w", err)
	}
	var owner managedOwnerMetadata
	if err := json.Unmarshal(payload, &owner); err != nil {
		return managedOwnerMetadata{}, false, fmt.Errorf("decode compose owner metadata: %w", err)
	}
	return owner, true, nil
}

func (s *Server) directContainerSpecRoot() string {
	return filepath.Join(s.workspaceRoot, ".open-sandbox", "containers")
}

func (s *Server) directContainerSpecPath(managedID string) string {
	return filepath.Join(s.directContainerSpecRoot(), managedID+".json")
}

func (s *Server) writeDirectContainerSpec(managedID string, req CreateContainerRequest) error {
	if err := ensurePrivateDir(filepath.Join(s.workspaceRoot, ".open-sandbox")); err != nil {
		return err
	}
	if err := ensurePrivateDir(s.directContainerSpecRoot()); err != nil {
		return err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode direct container spec: %w", err)
	}
	if err := os.WriteFile(s.directContainerSpecPath(managedID), payload, 0o600); err != nil {
		return fmt.Errorf("write direct container spec: %w", err)
	}
	return nil
}

func (s *Server) readDirectContainerSpec(managedID string) (CreateContainerRequest, error) {
	payload, err := os.ReadFile(s.directContainerSpecPath(managedID))
	if err != nil {
		return CreateContainerRequest{}, fmt.Errorf("read direct container spec: %w", err)
	}
	var req CreateContainerRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return CreateContainerRequest{}, fmt.Errorf("decode direct container spec: %w", err)
	}
	return req, nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private dir %q: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("chmod private dir %q: %w", path, err)
	}
	return nil
}

func newManagedResourceID(prefix string) string {
	requestID := newRequestID()
	if len(requestID) > 12 {
		requestID = requestID[:12]
	}
	if strings.TrimSpace(prefix) == "" {
		return requestID
	}
	return prefix + "-" + requestID
}

func runCommand(ctx context.Context, name string, args ...string) (string, string, error) {
	return runCommandInDir(ctx, "", name, args...)
}

func runCommandInDir(ctx context.Context, dir string, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func tarDirectory(dir string) (io.ReadCloser, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve context path: %w", err)
	}

	if info, err := os.Stat(absDir); err != nil {
		return nil, fmt.Errorf("stat context path: %w", err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("context path is not a directory: %s", absDir)
	}

	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		defer func() {
			_ = tw.Close()
			_ = pw.Close()
		}()

		walkErr := filepath.Walk(absDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			rel, err := filepath.Rel(absDir, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}

			if strings.HasPrefix(rel, ".git") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			header.Name = filepath.ToSlash(rel)
			if info.IsDir() {
				header.Name += "/"
			}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.IsDir() || !info.Mode().IsRegular() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}

			if _, err := io.Copy(tw, file); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}

			return nil
		})

		if walkErr != nil {
			_ = pw.CloseWithError(walkErr)
		}
	}()

	return pr, nil
}

func tarFromDockerfile(dockerfilePath string, dockerfileContent string, contextFiles map[string]string) (io.ReadCloser, error) {
	if strings.TrimSpace(dockerfileContent) == "" {
		return nil, errors.New("dockerfile_content cannot be empty")
	}

	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	normalizedDockerfilePath, err := sanitizeTarPath(dockerfilePath)
	if err != nil {
		return nil, fmt.Errorf("invalid dockerfile path: %w", err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	if err := addTarTextFile(tw, normalizedDockerfilePath, dockerfileContent); err != nil {
		_ = tw.Close()
		return nil, err
	}

	keys := make([]string, 0, len(contextFiles))
	for k := range contextFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		normalized, err := sanitizeTarPath(name)
		if err != nil {
			_ = tw.Close()
			return nil, fmt.Errorf("invalid context file path %q: %w", name, err)
		}
		if err := addTarTextFile(tw, normalized, contextFiles[name]); err != nil {
			_ = tw.Close()
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func addTarTextFile(tw *tar.Writer, name string, content string) error {
	contentBytes := []byte(content)
	header := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(contentBytes)),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	if _, err := tw.Write(contentBytes); err != nil {
		return err
	}

	return nil
}

func sanitizeTarPath(p string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if clean == "." || clean == "" {
		return "", errors.New("path cannot be empty")
	}
	if strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return "", errors.New("path must be relative and stay inside build context")
	}

	return clean, nil
}

type dockerBuildMessage struct {
	Stream      string `json:"stream"`
	Status      string `json:"status"`
	Progress    string `json:"progress"`
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}

func (s *Server) prepareBuildRequest(req BuildImageRequest) (io.ReadCloser, build.ImageBuildOptions, error) {
	if req.Dockerfile == "" {
		req.Dockerfile = "Dockerfile"
	}

	var (
		contextReader io.ReadCloser
		err           error
	)

	switch {
	case req.ContextPath != "":
		resolvedContextPath, resolveErr := s.resolvePathInWorkspace(req.ContextPath)
		if resolveErr != nil {
			return nil, build.ImageBuildOptions{}, resolveErr
		}
		contextReader, err = tarDirectory(resolvedContextPath)
	case req.DockerfileContent != "":
		contextReader, err = tarFromDockerfile(req.Dockerfile, req.DockerfileContent, req.ContextFiles)
	default:
		return nil, build.ImageBuildOptions{}, errors.New("provide either context_path or dockerfile_content")
	}

	if err != nil {
		return nil, build.ImageBuildOptions{}, err
	}

	buildArgs := make(map[string]*string, len(req.BuildArgs))
	for key, value := range req.BuildArgs {
		val := value
		buildArgs[key] = &val
	}

	return contextReader, build.ImageBuildOptions{
		Dockerfile: req.Dockerfile,
		Tags:       []string{req.Tag},
		Remove:     true,
		BuildArgs:  buildArgs,
	}, nil
}

func collectBuildOutput(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var output strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line)
		output.WriteByte('\n')

		var msg dockerBuildMessage
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			if msg.ErrorDetail.Message != "" {
				return strings.TrimSuffix(output.String(), "\n"), errors.New(msg.ErrorDetail.Message)
			}
			if msg.Error != "" {
				return strings.TrimSuffix(output.String(), "\n"), errors.New(msg.Error)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return strings.TrimSuffix(output.String(), "\n"), err
	}

	return strings.TrimSuffix(output.String(), "\n"), nil
}

func parseSearchLimit(raw string) (int, error) {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return 0, errors.New("limit must be a positive integer")
	}
	if limit > 100 {
		return 100, nil
	}
	return limit, nil
}

func parseDockerSearchOutput(stdout string) ([]ImageSearchResult, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	results := make([]ImageSearchResult, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		var record dockerSearchRecord
		if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
			return nil, fmt.Errorf("decode docker search output: %w", err)
		}

		stars := 0
		if value := strings.TrimSpace(record.StarCount); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parse docker search stars: %w", err)
			}
			stars = parsed
		}

		results = append(results, ImageSearchResult{
			Name:        strings.TrimSpace(record.Name),
			Description: strings.TrimSpace(record.Description),
			Stars:       stars,
			Official:    strings.TrimSpace(record.IsOfficial) != "",
			Automated:   strings.TrimSpace(record.IsAutomated) != "",
		})
	}

	return results, nil
}

func streamDockerBuildOutput(c *gin.Context, mu *sync.Mutex, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		var msg dockerBuildMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			emitSSE(c, mu, "stdout", line)
			continue
		}

		if msg.ErrorDetail.Message != "" {
			emitSSE(c, mu, "stderr", msg.ErrorDetail.Message)
			return errors.New(msg.ErrorDetail.Message)
		}
		if msg.Error != "" {
			emitSSE(c, mu, "stderr", msg.Error)
			return errors.New(msg.Error)
		}

		switch {
		case strings.TrimSpace(msg.Stream) != "":
			emitSSE(c, mu, "stdout", strings.TrimRight(msg.Stream, "\n"))
		case strings.TrimSpace(msg.Status) != "":
			statusLine := strings.TrimSpace(msg.Status)
			if strings.TrimSpace(msg.Progress) != "" {
				statusLine = statusLine + " " + strings.TrimSpace(msg.Progress)
			}
			emitSSE(c, mu, "stdout", statusLine)
		default:
			emitSSE(c, mu, "stdout", line)
		}
	}

	return scanner.Err()
}

func loadAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("SANDBOX_CORS_ORIGINS"))
	if raw == "" {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}

	if len(origins) == 0 {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	return origins
}

func buildAllowOriginFunc(allowedOrigins []string) func(string) bool {
	allowedSet := make(map[string]struct{}, len(allowedOrigins))
	allowAll := false
	for _, origin := range allowedOrigins {
		if origin == "*" {
			allowAll = true
			continue
		}
		allowedSet[origin] = struct{}{}
	}

	return func(origin string) bool {
		if allowAll {
			return true
		}
		if _, ok := allowedSet[origin]; ok {
			return true
		}

		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}

		host := parsed.Hostname()
		return host == "localhost" || host == "127.0.0.1"
	}
}

func buildAllowOriginWithContextFunc(allowedOrigins []string) func(*gin.Context, string) bool {
	allowOrigin := buildAllowOriginFunc(allowedOrigins)
	return func(c *gin.Context, origin string) bool {
		return allowOrigin(origin) ||
			requestOriginMatchesForwardedHost(c.Request, origin) ||
			requestOriginHostMatchesForwardedHost(c.Request, origin)
	}
}

func requestOriginMatchesForwardedHost(r *http.Request, origin string) bool {
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Host == "" || parsedOrigin.Hostname() == "" {
		return false
	}

	requestScheme := forwardedRequestProto(r)
	if requestScheme == "" {
		if r.TLS != nil {
			requestScheme = "https"
		} else {
			requestScheme = "http"
		}
	}
	if !strings.EqualFold(parsedOrigin.Scheme, requestScheme) {
		return false
	}

	requestHost := forwardedRequestHost(r)
	if requestHost == "" {
		return false
	}

	parsedRequestHost, err := url.Parse("//" + requestHost)
	if err != nil || parsedRequestHost.Hostname() == "" {
		return false
	}

	requestHostIncludesPort := parsedRequestHost.Host != parsedRequestHost.Hostname()
	if requestHostIncludesPort {
		return strings.EqualFold(parsedOrigin.Host, parsedRequestHost.Host)
	}

	return strings.EqualFold(parsedOrigin.Hostname(), parsedRequestHost.Hostname())
}

func requestOriginHostMatchesForwardedHost(r *http.Request, origin string) bool {
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Host == "" || parsedOrigin.Hostname() == "" {
		return false
	}

	requestHost := forwardedRequestHost(r)
	if requestHost == "" {
		return false
	}

	parsedRequestHost, err := url.Parse("//" + requestHost)
	if err != nil || parsedRequestHost.Hostname() == "" {
		return false
	}

	requestHostIncludesPort := parsedRequestHost.Host != parsedRequestHost.Hostname()
	originIncludesPort := parsedOrigin.Host != parsedOrigin.Hostname()
	if requestHostIncludesPort || originIncludesPort {
		originPort := parsedOrigin.Port()
		requestPort := parsedRequestHost.Port()
		if originPort != "" && requestPort != "" {
			return strings.EqualFold(parsedOrigin.Host, parsedRequestHost.Host)
		}
	}

	return strings.EqualFold(parsedOrigin.Hostname(), parsedRequestHost.Hostname())
}

func forwardedRequestHost(r *http.Request) string {
	if r == nil {
		return ""
	}

	raw := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if raw == "" {
		raw = strings.TrimSpace(r.Host)
	}
	if raw == "" {
		return ""
	}

	if parts := strings.Split(raw, ","); len(parts) > 0 {
		raw = strings.TrimSpace(parts[0])
	}

	if host, port, err := net.SplitHostPort(raw); err == nil {
		return net.JoinHostPort(host, port)
	}

	return raw
}

func forwardedRequestProto(r *http.Request) string {
	if r == nil {
		return ""
	}

	raw := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if raw == "" {
		return ""
	}

	if parts := strings.Split(raw, ","); len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}

	return raw
}

func loadExecWaitTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SANDBOX_EXEC_WAIT_TIMEOUT"))
	if raw == "" {
		return 30 * time.Second
	}

	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return 30 * time.Second
	}

	return timeout
}

func resolveWorkspaceRoot() string {
	raw := strings.TrimSpace(os.Getenv("SANDBOX_WORKSPACE_DIR"))
	if raw == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			raw = homeDir
		} else {
			raw = "."
		}
	}

	resolved, err := filepath.Abs(raw)
	if err != nil {
		fallback, fallbackErr := filepath.Abs(".")
		if fallbackErr != nil {
			return "."
		}
		return filepath.Clean(fallback)
	}

	return filepath.Clean(resolved)
}

func (s *Server) resolvePathInWorkspace(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("path is required")
	}

	candidate := filepath.Clean(trimmed)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(s.workspaceRoot, candidate)
	}

	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	rel, err := filepath.Rel(s.workspaceRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside workspace root %q", path, s.workspaceRoot)
	}

	return resolved, nil
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}

		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func requestLoggerMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		requestID, _ := c.Get("request_id")
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		latencyMS := float64(time.Since(start).Microseconds()) / 1000
		logger.Info(
			"request_complete",
			slog.String("request_id", fmt.Sprint(requestID)),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Float64("latency_ms", latencyMS),
			slog.String("client_ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
		)
	}
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	return fmt.Sprintf("%x", b)
}

func writeError(c *gin.Context, status int, err error) {
	c.JSON(status, ErrorResponse{Error: err.Error()})
}

func writeErrorWithDetails(c *gin.Context, status int, message string, reason string, stderr string) {
	payload := ErrorResponse{Error: message}
	if strings.TrimSpace(reason) != "" {
		payload.Reason = strings.TrimSpace(reason)
	}
	if strings.TrimSpace(stderr) != "" {
		payload.Stderr = strings.TrimSpace(stderr)
	}
	c.JSON(status, payload)
}
