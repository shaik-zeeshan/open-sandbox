package api

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
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
)

type ContainerSummary struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Created int64             `json:"created"`
	Labels  map[string]string `json:"labels"`
	Ports   []PortSummary     `json:"ports,omitempty"`
}

type PortSummary struct {
	Private int    `json:"private"`
	Public  int    `json:"public,omitempty"`
	Type    string `json:"type"`
	IP      string `json:"ip,omitempty"`
}

type SandboxResponse struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Image         string        `json:"image"`
	ContainerID   string        `json:"container_id"`
	WorkspaceDir  string        `json:"workspace_dir"`
	RepoURL       string        `json:"repo_url,omitempty"`
	Status        string        `json:"status"`
	OwnerUsername string        `json:"owner_username,omitempty"`
	Ports         []PortSummary `json:"ports,omitempty"`
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
}

type CreateSandboxRequest struct {
	Name               string   `json:"name" binding:"required"`
	Image              string   `json:"image" binding:"required"`
	RepoURL            string   `json:"repo_url"`
	Branch             string   `json:"branch"`
	RepoTargetPath     string   `json:"repo_target_path"`
	UseImageDefaultCmd bool     `json:"use_image_default_cmd"`
	Env                []string `json:"env"`
	Cmd                []string `json:"cmd"`
	Workdir            string   `json:"workdir"`
	TTY                bool     `json:"tty"`
	User               string   `json:"user"`
	Ports              []string `json:"ports"`
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
	Status string `json:"Status"`
	Labels string `json:"Labels"`
}

func (s *Server) listContainers(c *gin.Context) {
	containers, err := s.listRuntimeContainers(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, containers)
}

func (s *Server) restartContainer(c *gin.Context) {
	containerID := strings.TrimSpace(c.Param("id"))
	if containerID == "" {
		writeError(c, http.StatusBadRequest, errors.New("container id is required"))
		return
	}

	inspect, err := s.docker.ContainerInspect(c.Request.Context(), containerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if inspect.State != nil && inspect.State.Running {
		if err := s.docker.ContainerStop(c.Request.Context(), containerID, container.StopOptions{}); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := s.docker.ContainerStart(c.Request.Context(), containerID, container.StartOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if s.sandboxStore != nil {
		_ = s.sandboxStore.UpdateSandboxStatusByContainerID(c.Request.Context(), containerID, "running")
	}

	c.JSON(http.StatusOK, gin.H{"container_id": containerID, "restarted": true})
}

func (s *Server) stopContainer(c *gin.Context) {
	containerID := strings.TrimSpace(c.Param("id"))
	if containerID == "" {
		writeError(c, http.StatusBadRequest, errors.New("container id is required"))
		return
	}

	if err := s.docker.ContainerStop(c.Request.Context(), containerID, container.StopOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if s.sandboxStore != nil {
		_ = s.sandboxStore.UpdateSandboxStatusByContainerID(c.Request.Context(), containerID, "stopped")
	}

	c.JSON(http.StatusOK, gin.H{"container_id": containerID, "stopped": true})
}

func (s *Server) removeContainer(c *gin.Context) {
	containerID := strings.TrimSpace(c.Param("id"))
	if containerID == "" {
		writeError(c, http.StatusBadRequest, errors.New("container id is required"))
		return
	}

	force, _ := strconv.ParseBool(c.DefaultQuery("force", "true"))
	if err := s.docker.ContainerRemove(c.Request.Context(), containerID, container.RemoveOptions{Force: force, RemoveVolumes: true}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if s.sandboxStore != nil {
		_ = s.sandboxStore.DeleteSandboxByContainerID(c.Request.Context(), containerID)
	}

	c.JSON(http.StatusOK, gin.H{"container_id": containerID, "removed": true})
}

func (s *Server) readContainerFile(c *gin.Context) {
	containerID := strings.TrimSpace(c.Param("id"))
	filePath := strings.TrimSpace(c.Query("path"))
	if filePath == "" {
		writeError(c, http.StatusBadRequest, errors.New("query parameter path is required"))
		return
	}

	response, err := s.readContainerFileByID(c.Request.Context(), containerID, filePath)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) writeContainerFile(c *gin.Context) {
	containerID := strings.TrimSpace(c.Param("id"))
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		var req SaveFileRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, err)
			return
		}
		if err := s.writeContainerFileByID(c.Request.Context(), containerID, req.TargetPath, path.Base(req.TargetPath), strings.NewReader(req.Content)); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"container_id": containerID, "path": req.TargetPath, "saved": true})
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

	if err := s.writeContainerFileByID(c.Request.Context(), containerID, targetPath, header.Filename, file); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"container_id": containerID, "path": targetPath, "uploaded": true})
}

func (s *Server) createSandbox(c *gin.Context) {
	if s.sandboxStore == nil {
		writeError(c, http.StatusInternalServerError, errors.New("sandbox store is not configured"))
		return
	}

	var req CreateSandboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, err)
		return
	}

	identity, ok := authIdentityFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, errors.New("missing auth identity"))
		return
	}

	sandboxID := newRequestID()
	workspaceDir := strings.TrimSpace(req.Workdir)
	if workspaceDir == "" {
		workspaceDir = "/workspace"
	}

	volumeName := "open-sandbox-" + sandboxID[:12]
	_, err := s.docker.VolumeCreate(c.Request.Context(), volume.CreateOptions{
		Name: volumeName,
		Labels: map[string]string{
			"open-sandbox.managed":    "true",
			"open-sandbox.sandbox_id": sandboxID,
		},
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, fmt.Errorf("create sandbox volume: %w", err))
		return
	}

	containerName := fmt.Sprintf("sandbox-%s-%s", sanitizeSandboxName(req.Name), sandboxID[:6])

	containerConfig := &container.Config{
		Image:      req.Image,
		Env:        req.Env,
		WorkingDir: workspaceDir,
		Tty:        req.TTY,
		User:       req.User,
		Labels: map[string]string{
			"open-sandbox.managed":     "true",
			"open-sandbox.sandbox_id":  sandboxID,
			"open-sandbox.name":        req.Name,
			"open-sandbox.repo_url":    strings.TrimSpace(req.RepoURL),
			"open-sandbox.repo_branch": strings.TrimSpace(req.Branch),
			"open-sandbox.repo_target_path": func() string {
				if strings.TrimSpace(req.RepoTargetPath) != "" {
					return strings.TrimSpace(req.RepoTargetPath)
				}
				if strings.TrimSpace(req.RepoURL) == "" {
					return ""
				}
				return path.Join(workspaceDir, "repo")
			}(),
		},
	}
	if len(req.Cmd) > 0 {
		containerConfig.Cmd = req.Cmd
	} else if !req.UseImageDefaultCmd {
		containerConfig.Cmd = []string{"sleep", "infinity"}
	}
	hostConfig := &container.HostConfig{
		Binds: []string{fmt.Sprintf("%s:%s", volumeName, workspaceDir)},
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

	created, err := s.createContainerWithAutoPull(c.Request.Context(), req.Image, containerConfig, hostConfig, containerName)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if err := s.docker.ContainerStart(c.Request.Context(), created.ID, container.StartOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, fmt.Errorf("start sandbox container: %w", err))
		return
	}

	if strings.TrimSpace(req.RepoURL) != "" {
		targetPath := strings.TrimSpace(req.RepoTargetPath)
		if targetPath == "" {
			targetPath = path.Join(workspaceDir, "repo")
		}

		cmd := []string{"git", "clone"}
		if strings.TrimSpace(req.Branch) != "" {
			cmd = append(cmd, "--branch", strings.TrimSpace(req.Branch))
		}
		cmd = append(cmd, strings.TrimSpace(req.RepoURL), targetPath)

		execResp, execErr := s.runContainerExec(c.Request.Context(), created.ID, ExecRequest{Cmd: cmd})
		if execErr != nil {
			writeError(c, http.StatusInternalServerError, fmt.Errorf("clone repository: %w", execErr))
			return
		}
		if execResp.ExitCode != 0 {
			writeErrorWithDetails(c, http.StatusBadRequest, "clone repository failed", "git_clone_failed", strings.TrimSpace(execResp.Stderr))
			return
		}
	}

	now := timeNowUnix()
	sandboxRecord := store.Sandbox{
		ID:            sandboxID,
		Name:          req.Name,
		Image:         req.Image,
		ContainerID:   created.ID,
		WorkspaceDir:  workspaceDir,
		RepoURL:       strings.TrimSpace(req.RepoURL),
		Status:        "running",
		OwnerID:       identity.UserID,
		OwnerUsername: identity.Username,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.sandboxStore.CreateSandbox(c.Request.Context(), sandboxRecord); err != nil {
		_ = s.docker.ContainerRemove(c.Request.Context(), created.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
		writeError(c, http.StatusInternalServerError, fmt.Errorf("persist sandbox: %w", err))
		return
	}

	c.JSON(http.StatusOK, sandboxToResponse(sandboxRecord))
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

	stateByContainer, statusByContainer, portsByContainer := s.liveContainerState(c.Request.Context())
	out := make([]SandboxResponse, 0, len(sandboxes))
	for _, sandbox := range sandboxes {
		if !identity.IsAdmin() && sandbox.OwnerID != identity.UserID {
			continue
		}
		response := sandboxToResponse(sandbox)
		if liveState, ok := stateByContainer[sandbox.ContainerID]; ok {
			response.Status = liveState
			if liveStatus, statusOK := statusByContainer[sandbox.ContainerID]; statusOK && strings.TrimSpace(liveStatus) != "" {
				response.Status = liveStatus
			}
		}
		if ports, ok := portsByContainer[sandbox.ContainerID]; ok {
			response.Ports = ports
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
		}
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) restartSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	inspect, err := s.docker.ContainerInspect(c.Request.Context(), sandbox.ContainerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if inspect.State != nil && inspect.State.Running {
		if err := s.docker.ContainerStop(c.Request.Context(), sandbox.ContainerID, container.StopOptions{}); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := s.docker.ContainerStart(c.Request.Context(), sandbox.ContainerID, container.StartOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	_ = s.sandboxStore.UpdateSandboxStatus(c.Request.Context(), sandbox.ID, "running")
	c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "restarted": true})
}

func (s *Server) resetSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	inspect, err := s.docker.ContainerInspect(c.Request.Context(), sandbox.ContainerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	if inspect.State == nil || !inspect.State.Running {
		if err := s.docker.ContainerStart(c.Request.Context(), sandbox.ContainerID, container.StartOptions{}); err != nil {
			writeError(c, http.StatusInternalServerError, err)
			return
		}
	}

	cleanupCmd := []string{"sh", "-lc", fmt.Sprintf("rm -rf %s/* %s/.[!.]* %s/..?* 2>/dev/null || true", shellQuote(sandbox.WorkspaceDir), shellQuote(sandbox.WorkspaceDir), shellQuote(sandbox.WorkspaceDir))}
	cleanupResp, err := s.runContainerExec(c.Request.Context(), sandbox.ContainerID, ExecRequest{Cmd: cleanupCmd})
	if err != nil {
		writeError(c, http.StatusInternalServerError, fmt.Errorf("reset workspace: %w", err))
		return
	}
	if cleanupResp.ExitCode != 0 {
		writeErrorWithDetails(c, http.StatusInternalServerError, "reset workspace failed", "workspace_reset_failed", strings.TrimSpace(cleanupResp.Stderr))
		return
	}

	repoURL := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_url"])
	repoTargetPath := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_target_path"])
	repoBranch := strings.TrimSpace(inspect.Config.Labels["open-sandbox.repo_branch"])
	if repoURL != "" {
		if repoTargetPath == "" {
			repoTargetPath = path.Join(sandbox.WorkspaceDir, "repo")
		}
		cloneCmd := []string{"git", "clone"}
		if repoBranch != "" {
			cloneCmd = append(cloneCmd, "--branch", repoBranch)
		}
		cloneCmd = append(cloneCmd, repoURL, repoTargetPath)
		cloneResp, cloneErr := s.runContainerExec(c.Request.Context(), sandbox.ContainerID, ExecRequest{Cmd: cloneCmd})
		if cloneErr != nil {
			writeError(c, http.StatusInternalServerError, fmt.Errorf("re-clone repository: %w", cloneErr))
			return
		}
		if cloneResp.ExitCode != 0 {
			writeErrorWithDetails(c, http.StatusInternalServerError, "re-clone repository failed", "git_clone_failed", strings.TrimSpace(cloneResp.Stderr))
			return
		}
	}

	_ = s.sandboxStore.UpdateSandboxStatus(c.Request.Context(), sandbox.ID, "running")
	c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "reset": true})
}

func (s *Server) stopSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	if err := s.docker.ContainerStop(c.Request.Context(), sandbox.ContainerID, container.StopOptions{}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	_ = s.sandboxStore.UpdateSandboxStatus(c.Request.Context(), sandbox.ID, "stopped")

	updated, err := s.sandboxStore.GetSandbox(c.Request.Context(), sandbox.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, sandboxToResponse(updated))
}

func (s *Server) deleteSandbox(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	_ = s.docker.ContainerStop(c.Request.Context(), sandbox.ContainerID, container.StopOptions{})
	if err := s.docker.ContainerRemove(c.Request.Context(), sandbox.ContainerID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if err := s.sandboxStore.DeleteSandbox(c.Request.Context(), sandbox.ID); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

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

	response, err := s.runContainerExec(c.Request.Context(), sandbox.ContainerID, req)
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
	s.streamLogsForContainer(c, sandbox.ContainerID, follow, tail)
}

func (s *Server) streamSandboxTerminal(c *gin.Context) {
	sandbox, ok := s.loadSandbox(c)
	if !ok {
		return
	}

	s.streamTerminalForContainer(c, sandbox.ContainerID, sandbox.WorkspaceDir)
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

	response, err := s.readContainerFileByID(c.Request.Context(), sandbox.ContainerID, filePath)
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
		if err := s.writeContainerFileByID(c.Request.Context(), sandbox.ContainerID, req.TargetPath, path.Base(req.TargetPath), strings.NewReader(req.Content)); err != nil {
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

	if err := s.writeContainerFileByID(c.Request.Context(), sandbox.ContainerID, targetPath, header.Filename, file); err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": sandbox.ID, "path": targetPath, "uploaded": true})
}

func (s *Server) streamLogsForContainer(c *gin.Context, containerID string, follow bool, tail string) {
	reader, err := s.docker.ContainerLogs(c.Request.Context(), containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
	})
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, errors.New("streaming not supported"))
		return
	}

	mu := &sync.Mutex{}
	stdoutWriter := &sseChunkWriter{ctx: c, stream: "stdout", mu: mu}
	stderrWriter := &sseChunkWriter{ctx: c, stream: "stderr", mu: mu}

	inspect, err := s.docker.ContainerInspect(c.Request.Context(), containerID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err)
		return
	}

	if inspect.Config != nil && inspect.Config.Tty {
		if _, err := io.Copy(stdoutWriter, reader); err != nil {
			emitSSE(c, mu, "error", err.Error())
			flusher.Flush()
			return
		}
	} else if _, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, reader); err != nil {
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
	if !identity.IsAdmin() && sandbox.OwnerID != identity.UserID {
		writeError(c, http.StatusNotFound, store.ErrSandboxNotFound)
		return store.Sandbox{}, false
	}

	return sandbox, true
}

func (s *Server) readContainerFileByID(ctx context.Context, containerID string, filePath string) (FileReadResponse, error) {
	reader, stat, err := s.docker.CopyFromContainer(ctx, containerID, filePath)
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

func (s *Server) writeContainerFileByID(ctx context.Context, containerID string, targetPath string, uploadFilename string, content io.Reader) error {
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

	if err := s.docker.CopyToContainer(ctx, containerID, targetDir, archiveReader, container.CopyToContainerOptions{AllowOverwriteDirWithFile: true}); err != nil {
		return fmt.Errorf("copy file to container: %w", err)
	}

	return nil
}

func (s *Server) liveContainerState(ctx context.Context) (map[string]string, map[string]string, map[string][]PortSummary) {
	stateByContainer := map[string]string{}
	statusByContainer := map[string]string{}
	portsByContainer := map[string][]PortSummary{}

	containers, err := s.listRuntimeContainers(ctx)
	if err != nil {
		return stateByContainer, statusByContainer, portsByContainer
	}

	for _, item := range containers {
		stateByContainer[item.ID] = strings.TrimSpace(item.State)
		statusByContainer[item.ID] = strings.TrimSpace(item.Status)
		portsByContainer[item.ID] = item.Ports
	}

	return stateByContainer, statusByContainer, portsByContainer
}

func (s *Server) runtimeContainersByID(ctx context.Context) (map[string]ContainerSummary, error) {
	containers, err := s.listRuntimeContainers(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ContainerSummary, len(containers))
	for _, item := range containers {
		byID[item.ID] = item
	}
	return byID, nil
}

func (s *Server) listRuntimeContainers(ctx context.Context) ([]ContainerSummary, error) {
	stdout, stderr, err := commandRunner(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w: %s", err, strings.TrimSpace(stderr))
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	out := make([]ContainerSummary, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var record dockerContainerCLIRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("decode docker ps line: %w", err)
		}

		names := make([]string, 0)
		for _, name := range strings.Split(record.Names, ",") {
			trimmed := strings.TrimSpace(name)
			if trimmed != "" {
				names = append(names, trimmed)
			}
		}
		sort.Strings(names)

		out = append(out, ContainerSummary{
			ID:      strings.TrimSpace(record.ID),
			Names:   names,
			Image:   strings.TrimSpace(record.Image),
			State:   dockerCLIStatusState(record.Status),
			Status:  strings.TrimSpace(record.Status),
			Created: 0,
			Labels:  parseDockerLabels(record.Labels),
			Ports:   parseDockerCLIPorts(record.Ports),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		left := out[i].Names
		right := out[j].Names
		if len(left) == 0 || len(right) == 0 {
			return out[i].ID < out[j].ID
		}
		return left[0] < right[0]
	})

	return out, nil
}

func (s *Server) createContainerWithAutoPull(
	ctx context.Context,
	imageRef string,
	containerConfig *container.Config,
	hostConfig *container.HostConfig,
	containerName string,
) (container.CreateResponse, error) {
	created, err := s.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
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

	created, err = s.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
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
		WorkspaceDir:  sandbox.WorkspaceDir,
		RepoURL:       sandbox.RepoURL,
		Status:        sandbox.Status,
		OwnerUsername: sandbox.OwnerUsername,
		Ports:         nil,
		CreatedAt:     sandbox.CreatedAt,
		UpdatedAt:     sandbox.UpdatedAt,
	}
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
