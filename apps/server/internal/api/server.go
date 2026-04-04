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
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type DockerAPI interface {
	ImageBuild(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error)
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
	ListUsers(ctx context.Context) ([]store.User, error)
	UpdateUserPasswordHash(ctx context.Context, userID string, passwordHash string) error
	DeleteUser(ctx context.Context, userID string) error
}

type Server struct {
	docker          DockerAPI
	auth            AuthConfig
	router          *gin.Engine
	sandboxStore    SandboxStore
	userStore       UserStore
	workspaceRoot   string
	execWaitTimeout time.Duration
}

var commandRunner = runCommand
var commandRunnerInDir = runCommandInDir

type composeProjectContext struct {
	ProjectName string
	ProjectDir  string
	ComposeFile string
}

type ErrorResponse struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
	Stderr string `json:"stderr,omitempty"`
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
	Services any    `json:"services"`
	Raw      string `json:"raw"`
}

type GitCloneRequest struct {
	ContainerID string `json:"container_id" binding:"required"`
	RepoURL     string `json:"repo_url" binding:"required"`
	TargetPath  string `json:"target_path" binding:"required"`
	Branch      string `json:"branch"`
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
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(requestLoggerMiddleware(logger))
	origins := loadAllowedOrigins()
	r.Use(cors.New(cors.Config{
		AllowOriginFunc:  buildAllowOriginFunc(origins),
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "Accept", "X-Request-ID", "Traceparent", "Tracestate", "Baggage", "B3", "X-B3-TraceId", "X-B3-SpanId", "X-B3-ParentSpanId", "X-B3-Sampled", "X-B3-Flags", "Sentry-Trace"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	var userStore UserStore
	if sandboxStore != nil {
		if configuredUserStore, ok := sandboxStore.(UserStore); ok {
			userStore = configuredUserStore
		}
	}

	s := &Server{
		docker:          dockerClient,
		auth:            authConfig,
		router:          r,
		sandboxStore:    sandboxStore,
		userStore:       userStore,
		workspaceRoot:   resolveWorkspaceRoot(),
		execWaitTimeout: loadExecWaitTimeout(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) Router() *gin.Engine {
	return s.router
}

func (s *Server) registerRoutes() {
	s.router.GET("/health", s.health)
	s.router.GET("/auth/setup", s.setupStatus)
	s.router.POST("/auth/bootstrap", s.bootstrap)
	s.router.POST("/auth/login", s.login)
	s.router.GET("/auth/session", s.session)
	s.router.POST("/auth/logout", s.logout)
	secured := s.router.Group("/")
	secured.Use(s.auth.Middleware())

	api := secured.Group("/api")
	{
		api.GET("/users", s.listUsers)
		api.POST("/users", s.createUser)
		api.POST("/users/:id/password", s.updateUserPassword)
		api.DELETE("/users/:id", s.deleteUser)

		api.POST("/images/build/stream", s.buildImageStream)
		api.POST("/images/build", s.buildImage)
		api.POST("/images/pull", s.pullImage)
		api.GET("/images/search", s.searchImages)
		api.GET("/images", s.listImages)
		api.DELETE("/images/:id", s.removeImage)

		api.POST("/compose/up", s.composeUp)
		api.POST("/compose/down", s.composeDown)
		api.POST("/compose/status", s.composeStatus)

		api.POST("/git/clone", s.gitClone)

		api.GET("/containers", s.listContainers)
		api.POST("/containers/create", s.createContainer)
		api.POST("/containers/:id/restart", s.restartContainer)
		api.POST("/containers/:id/stop", s.stopContainer)
		api.DELETE("/containers/:id", s.removeContainer)
		api.POST("/containers/:id/exec", s.execInContainer)
		api.GET("/containers/:id/terminal/ws", s.streamContainerTerminal)
		api.GET("/containers/:id/logs", s.streamLogs)
		api.GET("/containers/:id/files", s.readContainerFile)
		api.PUT("/containers/:id/files", s.writeContainerFile)

		api.POST("/sandboxes", s.createSandbox)
		api.GET("/sandboxes", s.listSandboxes)
		api.GET("/sandboxes/:id", s.getSandbox)
		api.POST("/sandboxes/:id/restart", s.restartSandbox)
		api.POST("/sandboxes/:id/reset", s.resetSandbox)
		api.POST("/sandboxes/:id/stop", s.stopSandbox)
		api.DELETE("/sandboxes/:id", s.deleteSandbox)
		api.POST("/sandboxes/:id/exec", s.execInSandbox)
		api.GET("/sandboxes/:id/terminal/ws", s.streamSandboxTerminal)
		api.GET("/sandboxes/:id/logs", s.streamSandboxLogs)
		api.GET("/sandboxes/:id/files", s.readSandboxFile)
		api.PUT("/sandboxes/:id/files", s.writeSandboxFile)
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

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

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
// @Failure 500 {object} ErrorResponse
// @Router /api/images/{id} [delete]
func (s *Server) removeImage(c *gin.Context) {
	force, _ := strconv.ParseBool(c.DefaultQuery("force", "false"))

	deleted, err := s.docker.ImageRemove(c.Request.Context(), c.Param("id"), image.RemoveOptions{Force: force, PruneChildren: true})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
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

	project, err := s.prepareComposeProject(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
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

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

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

	project, err := s.prepareComposeProject(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	args := buildComposeArgs(project, req, "down")
	stdout, stderr, err := commandRunnerInDir(c.Request.Context(), project.ProjectDir, "docker", args...)
	if err != nil {
		writeErrorWithDetails(c, http.StatusInternalServerError, "compose down failed", "command_failed", strings.TrimSpace(stderr))
		return
	}

	c.JSON(http.StatusOK, ComposeResponse{Stdout: stdout, Stderr: stderr})
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

	project, err := s.prepareComposeProject(req)
	if err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	args := buildComposeArgs(project, req, "ps", "--format", "json")
	stdout, stderr, err := commandRunnerInDir(c.Request.Context(), project.ProjectDir, "docker", args...)
	if err != nil {
		writeErrorWithDetails(c, http.StatusInternalServerError, "compose status failed", "command_failed", strings.TrimSpace(stderr))
		return
	}

	var parsed any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		c.JSON(http.StatusOK, ComposeStatusResponse{Raw: stdout, Services: []any{}})
		return
	}

	c.JSON(http.StatusOK, ComposeStatusResponse{Raw: stdout, Services: parsed})
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

	cmd := []string{"git", "clone"}
	if req.Branch != "" {
		cmd = append(cmd, "--branch", req.Branch)
	}
	cmd = append(cmd, req.RepoURL, req.TargetPath)

	execResp, err := s.runContainerExec(c.Request.Context(), req.ContainerID, ExecRequest{Cmd: cmd})
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

	containerConfig := &container.Config{
		Image:      req.Image,
		Cmd:        req.Cmd,
		Env:        req.Env,
		WorkingDir: req.Workdir,
		Tty:        req.TTY,
		User:       req.User,
		Labels: map[string]string{
			"open-sandbox.managed":        "true",
			"open-sandbox.owner_id":       identity.UserID,
			"open-sandbox.owner_username": identity.Username,
		},
	}
	hostConfig := &container.HostConfig{
		Binds:      req.Binds,
		AutoRemove: req.AutoRemove,
	}

	if len(req.Ports) > 0 {
		exposedPorts, portBindings, err := nat.ParsePortSpecs(req.Ports)
		if err != nil {
			writeError(c, http.StatusBadRequest, fmt.Errorf("parse ports: %w", err))
			return
		}
		containerConfig.ExposedPorts = exposedPorts
		hostConfig.PortBindings = portBindings
	}

	created, err := s.createContainerWithAutoPull(c.Request.Context(), req.Image, containerConfig, hostConfig, req.Name)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	started := false
	if req.Start {
		if err := s.docker.ContainerStart(c.Request.Context(), created.ID, container.StartOptions{}); err != nil {
			writeError(c, http.StatusInternalServerError, fmt.Errorf("start container: %w", err))
			return
		}
		started = true
	}

	c.JSON(http.StatusOK, CreateContainerResponse{ContainerID: created.ID, Warnings: created.Warnings, Started: started})
}

func isMissingImageError(err error) bool {
	if errdefs.IsNotFound(err) {
		return true
	}

	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "no such image") || strings.Contains(errText, "not found")
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

	resp, err := s.runContainerExec(c.Request.Context(), target.ID, req)
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
	s.streamLogsForContainer(c, target.ID, follow, tail)
}

func (s *Server) streamContainerTerminal(c *gin.Context) {
	target, ok := s.loadAuthorizedContainer(c)
	if !ok {
		return
	}

	workdir := strings.TrimSpace(c.Query("workdir"))
	s.streamTerminalForContainer(c, target.ID, workdir)
}

func (s *Server) streamTerminalForContainer(c *gin.Context, containerID string, workdir string) {
	cols := parseTerminalDimension(c.Query("cols"), 120)
	rows := parseTerminalDimension(c.Query("rows"), 32)

	allowOrigin := buildAllowOriginFunc(loadAllowedOrigins())
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				return true
			}
			return allowOrigin(origin)
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)

	execID, attached, err := s.startInteractiveExec(c.Request.Context(), containerID, workdir, cols, rows)
	if err != nil {
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
			if err := s.docker.ContainerExecResize(c.Request.Context(), execID, container.ResizeOptions{Width: message.Cols, Height: message.Rows}); err != nil {
				cleanup.Do(cleanupSession)
				return
			}
		}
	}
}

func (s *Server) startInteractiveExec(
	ctx context.Context,
	containerID string,
	workdir string,
	cols uint,
	rows uint,
) (string, dockertypes.HijackedResponse, error) {
	consoleSize := &[2]uint{rows, cols}
	execConfig := container.ExecOptions{
		Cmd:          defaultTerminalShellCommand(),
		Env:          []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
		WorkingDir:   workdir,
		Tty:          true,
		ConsoleSize:  consoleSize,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	created, err := s.docker.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", dockertypes.HijackedResponse{}, fmt.Errorf("create interactive exec: %w", err)
	}

	attached, err := s.docker.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true, ConsoleSize: consoleSize})
	if err != nil {
		return "", dockertypes.HijackedResponse{}, fmt.Errorf("attach interactive exec: %w", err)
	}

	return created.ID, attached, nil
}

func defaultTerminalShellCommand() []string {
	return []string{"sh", "-lc", "export TERM=${TERM:-xterm-256color}; if command -v bash >/dev/null 2>&1; then exec bash -i; fi; exec sh -i"}
}

func parseTerminalDimension(raw string, fallback uint) uint {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || value == 0 {
		return fallback
	}

	return uint(value)
}

func (s *Server) runContainerExec(ctx context.Context, containerID string, req ExecRequest) (ExecResponse, error) {
	execConfig := container.ExecOptions{
		Cmd:          req.Cmd,
		User:         req.User,
		WorkingDir:   req.Workdir,
		Env:          req.Env,
		Tty:          req.TTY,
		AttachStdout: !req.Detach,
		AttachStderr: !req.Detach,
	}

	created, err := s.docker.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return ExecResponse{}, fmt.Errorf("create exec: %w", err)
	}

	if req.Detach {
		if err := s.docker.ContainerExecStart(ctx, created.ID, container.ExecStartOptions{Detach: true, Tty: req.TTY}); err != nil {
			return ExecResponse{}, fmt.Errorf("start detached exec: %w", err)
		}

		return ExecResponse{ExecID: created.ID, Detached: true}, nil
	}

	attached, err := s.docker.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: req.TTY})
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

	inspect, err := s.waitForExec(waitCtx, created.ID)
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

func (s *Server) waitForExec(ctx context.Context, execID string) (container.ExecInspect, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		inspect, err := s.docker.ContainerExecInspect(ctx, execID)
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
