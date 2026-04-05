package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type workloadRuntime interface {
	ListWorkloads(ctx context.Context) ([]ContainerSummary, error)
	InspectWorkload(ctx context.Context, workloadID string) (container.InspectResponse, error)
	CreateWorkload(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, workloadName string) (container.CreateResponse, error)
	StartWorkload(ctx context.Context, workloadID string, options container.StartOptions) error
	StopWorkload(ctx context.Context, workloadID string, options container.StopOptions) error
	RemoveWorkload(ctx context.Context, workloadID string, options container.RemoveOptions) error
	CreateVolume(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	ExecCreate(ctx context.Context, workloadID string, options container.ExecOptions) (container.ExecCreateResponse, error)
	ExecAttach(ctx context.Context, execID string, options container.ExecAttachOptions) (dockertypes.HijackedResponse, error)
	ExecResize(ctx context.Context, execID string, options container.ResizeOptions) error
	ExecStart(ctx context.Context, execID string, options container.ExecStartOptions) error
	ExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
	Logs(ctx context.Context, workloadID string, options container.LogsOptions) (io.ReadCloser, error)
	CopyFrom(ctx context.Context, workloadID, srcPath string) (io.ReadCloser, container.PathStat, error)
	CopyTo(ctx context.Context, workloadID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error
	ProjectName(summary ContainerSummary) string
	ServiceName(summary ContainerSummary) string
	ResetWorkload(ctx context.Context, target ContainerSummary) (workloadResetResult, bool, error)
}

type workloadResetResult struct {
	WorkloadID string
	Stdout     string
	Stderr     string
}

type dockerRuntime struct {
	docker          DockerAPI
	workspaceRootFn func() string
}

func newDockerRuntime(dockerClient DockerAPI, workspaceRootFn func() string) workloadRuntime {
	return &dockerRuntime{docker: dockerClient, workspaceRootFn: workspaceRootFn}
}

func (r *dockerRuntime) ListWorkloads(ctx context.Context) ([]ContainerSummary, error) {
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

func (r *dockerRuntime) InspectWorkload(ctx context.Context, workloadID string) (container.InspectResponse, error) {
	return r.docker.ContainerInspect(ctx, workloadID)
}

func (r *dockerRuntime) CreateWorkload(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, workloadName string) (container.CreateResponse, error) {
	return r.docker.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, workloadName)
}

func (r *dockerRuntime) StartWorkload(ctx context.Context, workloadID string, options container.StartOptions) error {
	return r.docker.ContainerStart(ctx, workloadID, options)
}

func (r *dockerRuntime) StopWorkload(ctx context.Context, workloadID string, options container.StopOptions) error {
	return r.docker.ContainerStop(ctx, workloadID, options)
}

func (r *dockerRuntime) RemoveWorkload(ctx context.Context, workloadID string, options container.RemoveOptions) error {
	return r.docker.ContainerRemove(ctx, workloadID, options)
}

func (r *dockerRuntime) CreateVolume(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	return r.docker.VolumeCreate(ctx, options)
}

func (r *dockerRuntime) ExecCreate(ctx context.Context, workloadID string, options container.ExecOptions) (container.ExecCreateResponse, error) {
	return r.docker.ContainerExecCreate(ctx, workloadID, options)
}

func (r *dockerRuntime) ExecAttach(ctx context.Context, execID string, options container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
	return r.docker.ContainerExecAttach(ctx, execID, options)
}

func (r *dockerRuntime) ExecResize(ctx context.Context, execID string, options container.ResizeOptions) error {
	return r.docker.ContainerExecResize(ctx, execID, options)
}

func (r *dockerRuntime) ExecStart(ctx context.Context, execID string, options container.ExecStartOptions) error {
	return r.docker.ContainerExecStart(ctx, execID, options)
}

func (r *dockerRuntime) ExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	return r.docker.ContainerExecInspect(ctx, execID)
}

func (r *dockerRuntime) Logs(ctx context.Context, workloadID string, options container.LogsOptions) (io.ReadCloser, error) {
	return r.docker.ContainerLogs(ctx, workloadID, options)
}

func (r *dockerRuntime) CopyFrom(ctx context.Context, workloadID, srcPath string) (io.ReadCloser, container.PathStat, error) {
	return r.docker.CopyFromContainer(ctx, workloadID, srcPath)
}

func (r *dockerRuntime) CopyTo(ctx context.Context, workloadID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	return r.docker.CopyToContainer(ctx, workloadID, dstPath, content, options)
}

func (r *dockerRuntime) ProjectName(summary ContainerSummary) string {
	return strings.TrimSpace(summary.Labels["com.docker.compose.project"])
}

func (r *dockerRuntime) ServiceName(summary ContainerSummary) string {
	return strings.TrimSpace(summary.Labels["com.docker.compose.service"])
}

func (r *dockerRuntime) ResetWorkload(ctx context.Context, target ContainerSummary) (workloadResetResult, bool, error) {
	projectName := r.ProjectName(target)
	if projectName == "" {
		return workloadResetResult{}, false, nil
	}

	project, err := existingComposeProjectAt(r.workspaceRootFn(), projectName)
	if err != nil {
		return workloadResetResult{}, true, err
	}

	serviceName := r.ServiceName(target)
	req := ComposeRequest{ProjectName: project.ProjectName}
	if serviceName != "" {
		req.Services = []string{serviceName}
	}

	args := buildComposeArgs(project, req, "up", "-d", "--force-recreate")
	stdout, stderr, err := commandRunnerInDir(ctx, project.ProjectDir, "docker", args...)
	result := workloadResetResult{WorkloadID: target.ID, Stdout: stdout, Stderr: stderr}
	if err != nil {
		return result, true, err
	}

	containers, listErr := r.ListWorkloads(ctx)
	if listErr != nil {
		return result, true, nil
	}
	for _, item := range containers {
		if r.ProjectName(item) != project.ProjectName {
			continue
		}
		if serviceName != "" && r.ServiceName(item) != serviceName {
			continue
		}
		result.WorkloadID = item.ID
		break
	}

	return result, true, nil
}

func existingComposeProjectAt(workspaceRoot string, projectName string) (composeProjectContext, error) {
	sanitized := sanitizeComposeProjectName(projectName)
	if sanitized == "" {
		return composeProjectContext{}, fmt.Errorf("compose project name is required")
	}
	projectDir := filepath.Join(workspaceRoot, ".open-sandbox", "compose", sanitized)
	composeFile := filepath.Join(projectDir, "docker-compose.yml")
	if _, err := os.Stat(composeFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return composeProjectContext{}, fmt.Errorf("compose project %q not found", sanitized)
		}
		return composeProjectContext{}, fmt.Errorf("inspect compose project: %w", err)
	}

	return composeProjectContext{ProjectName: sanitized, ProjectDir: projectDir, ComposeFile: composeFile}, nil
}
