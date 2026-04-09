package api

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
	traefikcfg "github.com/shaik-zeeshan/open-sandbox/internal/traefik"
)

type mockDocker struct {
	imageBuildFn           func(context.Context, io.Reader, build.ImageBuildOptions) (build.ImageBuildResponse, error)
	imageInspectFn         func(context.Context, string) (image.InspectResponse, error)
	imagePullFn            func(context.Context, string, image.PullOptions) (io.ReadCloser, error)
	imageListFn            func(context.Context, image.ListOptions) ([]image.Summary, error)
	imageRemoveFn          func(context.Context, string, image.RemoveOptions) ([]image.DeleteResponse, error)
	containerCreateFn      func(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error)
	containerStartFn       func(context.Context, string, container.StartOptions) error
	containerExecCreateFn  func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error)
	containerExecAttachFn  func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error)
	containerExecResizeFn  func(context.Context, string, container.ResizeOptions) error
	containerExecStartFn   func(context.Context, string, container.ExecStartOptions) error
	containerExecInspectFn func(context.Context, string) (container.ExecInspect, error)
	containerLogsFn        func(context.Context, string, container.LogsOptions) (io.ReadCloser, error)
	containerInspectFn     func(context.Context, string) (container.InspectResponse, error)
	containerListFn        func(context.Context, container.ListOptions) ([]container.Summary, error)
	containerStopFn        func(context.Context, string, container.StopOptions) error
	containerRemoveFn      func(context.Context, string, container.RemoveOptions) error
	copyFromContainerFn    func(context.Context, string, string) (io.ReadCloser, container.PathStat, error)
	copyToContainerFn      func(context.Context, string, string, io.Reader, container.CopyToContainerOptions) error
	volumeCreateFn         func(context.Context, volume.CreateOptions) (volume.Volume, error)
	volumeRemoveFn         func(context.Context, string, bool) error
}

type mockSandboxStore struct {
	createSandboxFn                  func(context.Context, store.Sandbox) error
	listSandboxesFn                  func(context.Context) ([]store.Sandbox, error)
	getSandboxFn                     func(context.Context, string) (store.Sandbox, error)
	updateSandboxRuntimeFn           func(context.Context, string, string, []string, []string, []string, string) error
	updateSandboxProxyConfigFn       func(context.Context, string, map[int]traefikcfg.ServiceProxyConfig) error
	updateSandboxStatusFn            func(context.Context, string, string) error
	updateSandboxStatusByContainerFn func(context.Context, string, string) error
	deleteSandboxFn                  func(context.Context, string) error
	deleteSandboxByContainerFn       func(context.Context, string) error
}

func (m *mockDocker) ImageBuild(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error) {
	if m.imageBuildFn == nil {
		return build.ImageBuildResponse{}, errors.New("not implemented")
	}
	return m.imageBuildFn(ctx, buildContext, options)
}

func (m *mockDocker) ImageInspect(ctx context.Context, imageID string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
	if m.imageInspectFn == nil {
		return image.InspectResponse{}, errors.New("not implemented")
	}
	return m.imageInspectFn(ctx, imageID)
}

func (m *mockDocker) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	if m.imagePullFn == nil {
		return nil, errors.New("not implemented")
	}
	return m.imagePullFn(ctx, refStr, options)
}

func (m *mockDocker) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	if m.imageListFn == nil {
		return nil, errors.New("not implemented")
	}
	return m.imageListFn(ctx, options)
}

func (m *mockDocker) ImageRemove(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error) {
	if m.imageRemoveFn == nil {
		return nil, errors.New("not implemented")
	}
	return m.imageRemoveFn(ctx, imageID, options)
}

func (m *mockDocker) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	if m.containerCreateFn == nil {
		return container.CreateResponse{}, errors.New("not implemented")
	}
	return m.containerCreateFn(ctx, config, hostConfig, networkingConfig, platform, containerName)
}

func (m *mockDocker) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	if m.containerStartFn == nil {
		return errors.New("not implemented")
	}
	return m.containerStartFn(ctx, containerID, options)
}

func (m *mockDocker) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error) {
	if m.containerExecCreateFn == nil {
		return container.ExecCreateResponse{}, errors.New("not implemented")
	}
	return m.containerExecCreateFn(ctx, containerID, options)
}

func (m *mockDocker) ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
	if m.containerExecAttachFn == nil {
		return dockertypes.HijackedResponse{}, errors.New("not implemented")
	}
	return m.containerExecAttachFn(ctx, execID, config)
}

func (m *mockDocker) ContainerExecResize(ctx context.Context, execID string, options container.ResizeOptions) error {
	if m.containerExecResizeFn == nil {
		return errors.New("not implemented")
	}
	return m.containerExecResizeFn(ctx, execID, options)
}

func (m *mockDocker) ContainerExecStart(ctx context.Context, execID string, config container.ExecStartOptions) error {
	if m.containerExecStartFn == nil {
		return errors.New("not implemented")
	}
	return m.containerExecStartFn(ctx, execID, config)
}

func (m *mockDocker) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	if m.containerExecInspectFn == nil {
		return container.ExecInspect{}, errors.New("not implemented")
	}
	return m.containerExecInspectFn(ctx, execID)
}

func (m *mockDocker) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	if m.containerLogsFn == nil {
		return nil, errors.New("not implemented")
	}
	return m.containerLogsFn(ctx, containerID, options)
}

func (m *mockDocker) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	if m.containerInspectFn == nil {
		return container.InspectResponse{}, errors.New("not implemented")
	}
	return m.containerInspectFn(ctx, containerID)
}

func (m *mockDocker) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	if m.containerListFn == nil {
		return nil, errors.New("not implemented")
	}
	return m.containerListFn(ctx, options)
}

func (m *mockDocker) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.containerStopFn == nil {
		return errors.New("not implemented")
	}
	return m.containerStopFn(ctx, containerID, options)
}

func (m *mockDocker) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	if m.containerRemoveFn == nil {
		return errors.New("not implemented")
	}
	return m.containerRemoveFn(ctx, containerID, options)
}

func (m *mockDocker) CopyFromContainer(ctx context.Context, containerID, srcPath string) (io.ReadCloser, container.PathStat, error) {
	if m.copyFromContainerFn == nil {
		return nil, container.PathStat{}, errors.New("not implemented")
	}
	return m.copyFromContainerFn(ctx, containerID, srcPath)
}

func (m *mockDocker) CopyToContainer(ctx context.Context, containerID, dstPath string, content io.Reader, options container.CopyToContainerOptions) error {
	if m.copyToContainerFn == nil {
		return errors.New("not implemented")
	}
	return m.copyToContainerFn(ctx, containerID, dstPath, content, options)
}

func (m *mockDocker) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	if m.volumeCreateFn == nil {
		return volume.Volume{}, errors.New("not implemented")
	}
	return m.volumeCreateFn(ctx, options)
}

func (m *mockDocker) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	if m.volumeRemoveFn == nil {
		return errors.New("not implemented")
	}
	return m.volumeRemoveFn(ctx, volumeID, force)
}

func (m *mockSandboxStore) CreateSandbox(ctx context.Context, sandbox store.Sandbox) error {
	if m.createSandboxFn == nil {
		return errors.New("not implemented")
	}
	return m.createSandboxFn(ctx, sandbox)
}

func (m *mockSandboxStore) ListSandboxes(ctx context.Context) ([]store.Sandbox, error) {
	if m.listSandboxesFn == nil {
		return nil, errors.New("not implemented")
	}
	return m.listSandboxesFn(ctx)
}

func (m *mockSandboxStore) GetSandbox(ctx context.Context, sandboxID string) (store.Sandbox, error) {
	if m.getSandboxFn == nil {
		return store.Sandbox{}, errors.New("not implemented")
	}
	return m.getSandboxFn(ctx, sandboxID)
}

func (m *mockSandboxStore) UpdateSandboxRuntime(ctx context.Context, sandboxID string, containerID string, env []string, secretEnv []string, secretEnvKeys []string, status string) error {
	if m.updateSandboxRuntimeFn == nil {
		return errors.New("not implemented")
	}
	return m.updateSandboxRuntimeFn(ctx, sandboxID, containerID, env, secretEnv, secretEnvKeys, status)
}

func (m *mockSandboxStore) UpdateSandboxStatus(ctx context.Context, sandboxID string, status string) error {
	if m.updateSandboxStatusFn == nil {
		return errors.New("not implemented")
	}
	return m.updateSandboxStatusFn(ctx, sandboxID, status)
}

func (m *mockSandboxStore) UpdateSandboxProxyConfig(ctx context.Context, sandboxID string, proxyConfig map[int]traefikcfg.ServiceProxyConfig) error {
	if m.updateSandboxProxyConfigFn == nil {
		return errors.New("not implemented")
	}
	return m.updateSandboxProxyConfigFn(ctx, sandboxID, proxyConfig)
}

func (m *mockSandboxStore) UpdateSandboxStatusByContainerID(ctx context.Context, containerID string, status string) error {
	if m.updateSandboxStatusByContainerFn == nil {
		return errors.New("not implemented")
	}
	return m.updateSandboxStatusByContainerFn(ctx, containerID, status)
}

func (m *mockSandboxStore) DeleteSandbox(ctx context.Context, sandboxID string) error {
	if m.deleteSandboxFn == nil {
		return errors.New("not implemented")
	}
	return m.deleteSandboxFn(ctx, sandboxID)
}

func (m *mockSandboxStore) DeleteSandboxByContainerID(ctx context.Context, containerID string) error {
	if m.deleteSandboxByContainerFn == nil {
		return errors.New("not implemented")
	}
	return m.deleteSandboxByContainerFn(ctx, containerID)
}

func newTestServer(d DockerAPI) *Server {
	gin.SetMode(gin.TestMode)
	return NewServer(d, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"})
}

func newTestServerWithStore(d DockerAPI, sandboxStore SandboxStore) *Server {
	gin.SetMode(gin.TestMode)
	return NewServerWithStore(d, AuthConfig{JWTSecret: []byte("jwt-secret"), TokenTTL: time.Minute, Issuer: "open-sandbox"}, sandboxStore)
}

func newSQLiteStoreForAPITest(t *testing.T) *store.SQLiteStore {
	t.Helper()
	sqliteStore, err := store.OpenSQLite(filepath.Join(t.TempDir(), "server-test.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	return sqliteStore
}

func setSandboxSecretsKey(t *testing.T, key string) {
	t.Helper()
	if key == "" {
		previous, hadPrevious := os.LookupEnv(sandboxSecretsKeyEnvVar)
		if err := os.Unsetenv(sandboxSecretsKeyEnvVar); err != nil {
			t.Fatalf("failed to unset %s: %v", sandboxSecretsKeyEnvVar, err)
		}
		t.Cleanup(func() {
			if !hadPrevious {
				_ = os.Unsetenv(sandboxSecretsKeyEnvVar)
				return
			}
			_ = os.Setenv(sandboxSecretsKeyEnvVar, previous)
		})
		return
	}
	t.Setenv(sandboxSecretsKeyEnvVar, key)
}

func signedTestToken(t *testing.T) string {
	return signedTokenFor(t, AuthIdentity{UserID: "admin-user", Username: "admin", Role: roleAdmin})
}

func signedTokenFor(t *testing.T, identity AuthIdentity) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, authClaims{
		UserID:   identity.UserID,
		Username: identity.Username,
		Role:     identity.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "open-sandbox",
			Subject:   identity.UserID,
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Minute)),
		},
	})

	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		t.Fatalf("failed to sign test jwt: %v", err)
	}

	return signed
}

func setAuthHeader(t *testing.T, req *http.Request) {
	t.Helper()
	signed := signedTestToken(t)
	req.Header.Set("Authorization", "Bearer "+signed)
}

func TestListWorkersIncludesLocalWorker(t *testing.T) {
	store := newSQLiteStoreForAPITest(t)
	s := newTestServerWithStore(&mockDocker{}, store)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/workers", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var workers []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &workers); err != nil {
		t.Fatalf("failed to decode workers response: %v", err)
	}
	if len(workers) == 0 {
		t.Fatal("expected at least one worker")
	}
	if workers[0]["id"] != localRuntimeWorkerID {
		t.Fatalf("expected first worker id %q, got %#v", localRuntimeWorkerID, workers[0]["id"])
	}
}

func TestGetTraefikRouteStateIncludesManagedWorkloads(t *testing.T) {
	originalCommandRunner := commandRunner
	defer func() { commandRunner = originalCommandRunner }()

	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		if name == "docker" && len(args) >= 3 && args[0] == "ps" && args[1] == "-a" {
			return strings.Join([]string{
				`{"ID":"sandbox-container","Image":"node:20","Names":"sandbox-one","Ports":"0.0.0.0:43000->3000/tcp","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=admin-user"}`,
				`{"ID":"direct-container","Image":"nginx:latest","Names":"direct-one","Ports":"0.0.0.0:48080->8080/tcp","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.kind=direct,open-sandbox.managed_id=ctr-1,open-sandbox.owner_id=admin-user"}`,
				`{"ID":"compose-web","Image":"nginx:latest","Names":"demo-web-1","Ports":"0.0.0.0:50080->80/tcp","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=admin-user,com.docker.compose.project=demo,com.docker.compose.service=web"}`,
			}, "\n"), "", nil
		}
		return "", "", errors.New("unexpected command")
	}

	t.Setenv("SANDBOX_TRAEFIK_DYNAMIC_CONFIG_DIR", filepath.Join(t.TempDir(), "traefik", "dynamic"))
	store := newSQLiteStoreForAPITest(t)
	s := newTestServerWithStore(&mockDocker{}, store)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/traefik/routes", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var payload traefikRouteStateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Enabled {
		t.Fatalf("expected enabled=true, got false")
	}
	if payload.DynamicConfigDir == "" {
		t.Fatal("expected dynamic config dir in response")
	}
	if len(payload.Sandboxes) != 1 || payload.Sandboxes[0].File != "sandbox-sandbox-1.yaml" {
		t.Fatalf("unexpected sandbox route state: %+v", payload.Sandboxes)
	}
	if len(payload.Containers) != 1 || payload.Containers[0].File != "container-ctr-1.yaml" {
		t.Fatalf("unexpected container route state: %+v", payload.Containers)
	}
	if len(payload.ComposeProjects) != 1 || payload.ComposeProjects[0].File != "compose-demo.yaml" {
		t.Fatalf("unexpected compose route state: %+v", payload.ComposeProjects)
	}
}

func TestGetTraefikRouteStateRequiresAdmin(t *testing.T) {
	s := newTestServer(&mockDocker{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/traefik/routes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "member", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSandboxesReconcilesTraefikRouteFiles(t *testing.T) {
	originalCommandRunner := commandRunner
	defer func() { commandRunner = originalCommandRunner }()
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		if name == "docker" && len(args) >= 3 && args[0] == "ps" && args[1] == "-a" {
			return `{"ID":"sandbox-container","Image":"nginx:latest","Names":"sandbox-one","Ports":"0.0.0.0:43000->80/tcp","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
		}
		return "", "", errors.New("unexpected command")
	}

	dynamicDir := filepath.Join(t.TempDir(), "traefik", "dynamic")
	t.Setenv("SANDBOX_TRAEFIK_DYNAMIC_CONFIG_DIR", dynamicDir)
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"}}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	routeFile := filepath.Join(dynamicDir, "sandbox-sandbox-1.yaml")
	if err := os.Remove(routeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove seeded route file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(routeFile); err != nil {
		t.Fatalf("expected sandbox route file to be reconciled during list, err=%v", err)
	}
}

func TestListComposeProjectsReconcilesTraefikRouteFiles(t *testing.T) {
	originalCommandRunner := commandRunner
	defer func() { commandRunner = originalCommandRunner }()
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		if name == "docker" && len(args) >= 3 && args[0] == "ps" && args[1] == "-a" {
			return `{"ID":"compose-web","Image":"nginx:latest","Names":"demo-web-1","Ports":"0.0.0.0:50080->80/tcp","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=web"}` + "\n", "", nil
		}
		return "", "", errors.New("unexpected command")
	}

	dynamicDir := filepath.Join(t.TempDir(), "traefik", "dynamic")
	t.Setenv("SANDBOX_TRAEFIK_DYNAMIC_CONFIG_DIR", dynamicDir)
	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("create compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "member-1", Username: "alice"}); err != nil {
		t.Fatalf("write compose owner metadata: %v", err)
	}

	routeFile := filepath.Join(dynamicDir, "compose-demo.yaml")
	if err := os.Remove(routeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove seeded compose route file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/compose/projects", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(routeFile); err != nil {
		t.Fatalf("expected compose route file to be reconciled during list, err=%v", err)
	}
}

func TestSyncTraefikRoutesUsesContextWithoutCancel(t *testing.T) {
	originalCommandRunner := commandRunner
	defer func() { commandRunner = originalCommandRunner }()
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		if name == "docker" && len(args) >= 3 && args[0] == "ps" && args[1] == "-a" {
			return `{"ID":"compose-web","Image":"nginx:latest","Names":"demo-web-1","Ports":"0.0.0.0:50080->80/tcp","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=web"}` + "\n", "", nil
		}
		return "", "", errors.New("unexpected command")
	}

	dynamicDir := filepath.Join(t.TempDir(), "traefik", "dynamic")
	t.Setenv("SANDBOX_TRAEFIK_DYNAMIC_CONFIG_DIR", dynamicDir)
	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("create compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	routeFile := filepath.Join(dynamicDir, "compose-demo.yaml")
	if err := os.Remove(routeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove seeded compose route file: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	s.syncTraefikRoutes(canceled)

	if _, err := os.Stat(routeFile); err != nil {
		t.Fatalf("expected compose route file to be reconciled with canceled context, err=%v", err)
	}
}

func TestWorkerRegisterAndHeartbeat(t *testing.T) {
	t.Setenv("SANDBOX_WORKER_SHARED_SECRET", "worker-secret")
	store := newSQLiteStoreForAPITest(t)
	s := newTestServerWithStore(&mockDocker{}, store)

	registerBody := `{"worker_id":"worker-2","name":"worker-2","advertise_address":"http://10.0.0.2:8080","execution_mode":"docker","version":"v1","heartbeat_ttl_seconds":20}`
	registerReq := httptest.NewRequest(http.MethodPost, "/control/workers/register", strings.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set(workerAuthHeader, "worker-secret")
	registerW := httptest.NewRecorder()

	s.Router().ServeHTTP(registerW, registerReq)

	if registerW.Code != http.StatusOK {
		t.Fatalf("expected register 200, got %d: %s", registerW.Code, registerW.Body.String())
	}

	heartbeatBody := `{"status":"active","advertise_address":"http://10.0.0.2:9090","version":"v2"}`
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/control/workers/worker-2/heartbeat", strings.NewReader(heartbeatBody))
	heartbeatReq.Header.Set("Content-Type", "application/json")
	heartbeatReq.Header.Set(workerAuthHeader, "worker-secret")
	heartbeatW := httptest.NewRecorder()

	s.Router().ServeHTTP(heartbeatW, heartbeatReq)

	if heartbeatW.Code != http.StatusOK {
		t.Fatalf("expected heartbeat 200, got %d: %s", heartbeatW.Code, heartbeatW.Body.String())
	}

	worker, err := store.GetRuntimeWorker(t.Context(), "worker-2")
	if err != nil {
		t.Fatalf("failed to get registered worker: %v", err)
	}
	if worker.AdvertiseAddress != "http://10.0.0.2:9090" {
		t.Fatalf("expected heartbeat to update advertise address, got %q", worker.AdvertiseAddress)
	}
	if worker.Version != "v2" {
		t.Fatalf("expected heartbeat to update version, got %q", worker.Version)
	}
}

func TestHealthEndpointIsPublic(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHealthEndpointReportsInvalidSecretEnvConfig(t *testing.T) {
	setSandboxSecretsKey(t, "not-a-valid-key")
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), sandboxSecretsKeyEnvVar) {
		t.Fatalf("expected config error in health response, got %s", w.Body.String())
	}
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestListImagesEndpoint(t *testing.T) {
	m := &mockDocker{
		imageListFn: func(context.Context, image.ListOptions) ([]image.Summary, error) {
			return []image.Summary{{ID: "sha256:abc", RepoTags: []string{"alpine:latest"}, Created: 1, Size: 1024}}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body []ImageSummary
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body) != 1 || body[0].ID != "sha256:abc" {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
}

func TestRemoveImageEndpoint(t *testing.T) {
	var capturedID string
	var capturedForce bool

	m := &mockDocker{
		imageRemoveFn: func(_ context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error) {
			capturedID = imageID
			capturedForce = options.Force
			return []image.DeleteResponse{{Deleted: "sha256:abc"}}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:abc?force=true", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if capturedID != "sha256:abc" {
		t.Fatalf("expected image id sha256:abc, got %q", capturedID)
	}
	if !capturedForce {
		t.Fatal("expected force=true to be passed to docker")
	}
}

func TestRemoveImageEndpointReturnsNotFound(t *testing.T) {
	m := &mockDocker{
		imageRemoveFn: func(context.Context, string, image.RemoveOptions) ([]image.DeleteResponse, error) {
			return nil, errdefs.NotFound(errors.New("no such image"))
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:missing", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestRemoveImageEndpointReturnsConflict(t *testing.T) {
	m := &mockDocker{
		imageRemoveFn: func(context.Context, string, image.RemoveOptions) ([]image.DeleteResponse, error) {
			return nil, errdefs.Conflict(errors.New("image is being used by running container"))
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:abc", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestRemoveImageEndpointRejectsInvalidForceQuery(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodDelete, "/api/images/sha256:abc?force=definitely", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestBuildImageFromDockerfileContent(t *testing.T) {
	var capturedDockerfile string
	var capturedTag string
	var capturedFiles map[string]string

	m := &mockDocker{
		imageBuildFn: func(_ context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error) {
			capturedDockerfile = options.Dockerfile
			if len(options.Tags) > 0 {
				capturedTag = options.Tags[0]
			}

			contextBytes, err := io.ReadAll(buildContext)
			if err != nil {
				return build.ImageBuildResponse{}, err
			}
			capturedFiles = untarTextFiles(t, contextBytes)

			return build.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(`{"stream":"ok"}`))}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/images/build", bytes.NewBufferString(`{
		"tag":"sandbox-inline:latest",
		"dockerfile":"Dockerfile",
		"dockerfile_content":"FROM alpine:3.20\nRUN echo hello\n",
		"context_files":{"app.txt":"hello from app"}
	}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedDockerfile != "Dockerfile" {
		t.Fatalf("expected dockerfile path Dockerfile, got %q", capturedDockerfile)
	}
	if capturedTag != "sandbox-inline:latest" {
		t.Fatalf("expected tag sandbox-inline:latest, got %q", capturedTag)
	}
	if capturedFiles["Dockerfile"] == "" || !strings.Contains(capturedFiles["Dockerfile"], "FROM alpine") {
		t.Fatalf("expected Dockerfile content in build context: %+v", capturedFiles)
	}
	if capturedFiles["app.txt"] != "hello from app" {
		t.Fatalf("expected context file app.txt in build context: %+v", capturedFiles)
	}
}

func TestBuildImageStreamEndpoint(t *testing.T) {
	m := &mockDocker{
		imageBuildFn: func(_ context.Context, _ io.Reader, _ build.ImageBuildOptions) (build.ImageBuildResponse, error) {
			body := strings.Join([]string{
				`{"stream":"Step 1/2 : FROM alpine:3.20\n"}`,
				`{"status":"Downloading","progress":"[=====>]"}`,
				`{"stream":"Successfully built\n"}`,
			}, "\n")
			return build.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(body))}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/images/build/stream", bytes.NewBufferString(`{
		"tag":"sandbox-inline:latest",
		"dockerfile":"Dockerfile",
		"dockerfile_content":"FROM alpine:3.20\nRUN echo hello\n"
	}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: stdout") || !strings.Contains(body, "Successfully built") || !strings.Contains(body, "event: done") {
		t.Fatalf("expected build stream events in response: %s", body)
	}
}

func TestBuildImageRequiresContextOrDockerfileContent(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodPost, "/api/images/build", bytes.NewBufferString(`{"tag":"sandbox-inline:latest"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestBuildImageRejectsContextPathOutsideWorkspace(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodPost, "/api/images/build", bytes.NewBufferString(`{"tag":"sandbox-inline:latest","context_path":"../"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSearchImagesEndpoint(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()

	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return strings.Join([]string{
			`{"Name":"ubuntu","Description":"Ubuntu base image","StarCount":"12345","IsOfficial":"[OK]","IsAutomated":""}`,
			`{"Name":"bitnami/redis","Description":"Redis image","StarCount":"321","IsOfficial":"","IsAutomated":"[OK]"}`,
		}, "\n"), "", nil
	}

	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/api/images/search?q=ubuntu&limit=10", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var body []ImageSearchResult
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body) != 2 {
		t.Fatalf("expected 2 results, got %d", len(body))
	}
	if body[0].Name != "ubuntu" || !body[0].Official || body[0].Stars != 12345 {
		t.Fatalf("unexpected first result: %+v", body[0])
	}
	if body[1].Name != "bitnami/redis" || !body[1].Automated {
		t.Fatalf("unexpected second result: %+v", body[1])
	}
}

func TestSearchImagesEndpointRequiresQuery(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/api/images/search", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestComposeStatusEndpoint(t *testing.T) {
	original := commandRunnerInDir
	defer func() { commandRunnerInDir = original }()

	workspaceRoot := t.TempDir()
	var capturedDir string
	var capturedArgs []string
	commandRunnerInDir = func(_ context.Context, dir string, _ string, args ...string) (string, string, error) {
		capturedDir = dir
		capturedArgs = append([]string(nil), args...)
		return `[{"Name":"app","State":"running"}]`, "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = workspaceRoot
	req := httptest.NewRequest(http.MethodPost, "/api/compose/status", bytes.NewBufferString(`{
		"content":"services:\n  app:\n    image: alpine:3.20\n",
		"project_name":"sandbox"
	}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("running")) {
		t.Fatalf("expected compose status in response: %s", w.Body.String())
	}

	expectedProjectDir := filepath.Join(workspaceRoot, ".open-sandbox", "compose", "sandbox")
	expectedComposeFile := filepath.Join(expectedProjectDir, "docker-compose.yml")
	if capturedDir != expectedProjectDir {
		t.Fatalf("expected compose command dir %q, got %q", expectedProjectDir, capturedDir)
	}
	argsJoined := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsJoined, "--project-directory "+expectedProjectDir) {
		t.Fatalf("expected project directory in args, got %v", capturedArgs)
	}
	if !strings.Contains(argsJoined, "-f "+expectedComposeFile) {
		t.Fatalf("expected compose file in args, got %v", capturedArgs)
	}
	content, err := os.ReadFile(expectedComposeFile)
	if err != nil {
		t.Fatalf("expected managed compose file: %v", err)
	}
	if !strings.Contains(string(content), "image: alpine:3.20") {
		t.Fatalf("unexpected compose file content: %q", string(content))
	}
}

func TestComposeStatusRequiresContent(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodPost, "/api/compose/status", bytes.NewBufferString(`{"project_name":"sandbox"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestComposeProjectPreviewEndpointsIncludePublishedPortsOnly(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return strings.Join([]string{
			`{"ID":"demo-web-1","Image":"nginx:latest","Names":"demo-web-1","Ports":"0.0.0.0:8080->80/tcp,3000/tcp","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=web"}`,
			`{"ID":"demo-db-1","Image":"postgres:16","Names":"demo-db-1","Ports":"5432/tcp","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=db"}`,
			`{"ID":"hidden-web-1","Image":"nginx:latest","Names":"hidden-web-1","Ports":"0.0.0.0:9000->80/tcp","Status":"Up 1 minute","Labels":"com.docker.compose.project=hidden,com.docker.compose.service=web"}`,
		}, "\n"), "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	demoProjectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(demoProjectDir); err != nil {
		t.Fatalf("expected demo compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(demoProjectDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("expected demo compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(demoProjectDir, AuthIdentity{UserID: "member-1", Username: "alice"}); err != nil {
		t.Fatalf("expected demo owner metadata: %v", err)
	}

	hiddenProjectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "hidden")
	if err := ensurePrivateDir(hiddenProjectDir); err != nil {
		t.Fatalf("expected hidden compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hiddenProjectDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("expected hidden compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(hiddenProjectDir, AuthIdentity{UserID: "member-2", Username: "bob"}); err != nil {
		t.Fatalf("expected hidden owner metadata: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/compose/projects", nil)
	listReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	listW := httptest.NewRecorder()
	s.Router().ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}

	var projects []ComposeProjectPreviewResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode compose projects response: %v", err)
	}
	if len(projects) != 1 || projects[0].ProjectName != "demo" {
		t.Fatalf("expected only demo project, got %+v", projects)
	}
	if len(projects[0].Services) != 2 {
		t.Fatalf("expected 2 demo services, got %+v", projects[0].Services)
	}
	for _, service := range projects[0].Services {
		switch service.ServiceName {
		case "web":
			if len(service.Ports) != 1 {
				t.Fatalf("expected web to expose only published ports, got %+v", service.Ports)
			}
			if service.Ports[0].PrivatePort != 80 || service.Ports[0].PublicPort != 8080 {
				t.Fatalf("unexpected web published port mapping: %+v", service.Ports[0])
			}
			if service.Ports[0].PreviewURL != "/auth/preview/launch/compose/demo/web/80" {
				t.Fatalf("unexpected web preview url: %q", service.Ports[0].PreviewURL)
			}
		case "db":
			if len(service.Ports) != 0 {
				t.Fatalf("expected db to have no preview ports for internal-only port, got %+v", service.Ports)
			}
		default:
			t.Fatalf("unexpected service in compose project response: %+v", service)
		}
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/compose/projects/demo", nil)
	getReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	getW := httptest.NewRecorder()
	s.Router().ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}
	var project ComposeProjectPreviewResponse
	if err := json.Unmarshal(getW.Body.Bytes(), &project); err != nil {
		t.Fatalf("decode compose project response: %v", err)
	}
	if project.ProjectName != "demo" {
		t.Fatalf("expected demo project response, got %+v", project)
	}
}

func TestComposeUpReservesOwnerBeforeCommandSucceeds(t *testing.T) {
	dockerBinDir := t.TempDir()
	dockerScript := filepath.Join(dockerBinDir, "docker")
	if err := os.WriteFile(dockerScript, []byte("#!/bin/sh\nprintf 'compose failed\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("expected fake docker script: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dockerBinDir+string(os.PathListSeparator)+oldPath)

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	req := httptest.NewRequest(http.MethodPost, "/api/compose/up", bytes.NewBufferString(`{
		"content":"services:\n  app:\n    image: alpine:3.20\n",
		"project_name":"demo"
	}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	owner, hasOwner, err := s.readComposeProjectOwnerMetadata(projectDir)
	if err != nil {
		t.Fatalf("expected compose owner metadata after failed run: %v", err)
	}
	if !hasOwner || owner.UserID != "member-1" {
		t.Fatalf("expected failed first compose run to reserve ownership, got %+v", owner)
	}
	shouldWriteOwner, err := s.authorizeComposeProjectAccess(AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}, s.composeProjectContextForName("demo"))
	if err != nil {
		t.Fatalf("expected same owner to be allowed to retry after failed run: %v", err)
	}
	if shouldWriteOwner {
		t.Fatal("expected retry access to reuse existing owner metadata")
	}
}

func TestComposeDownIncludesOptionalFlags(t *testing.T) {
	original := commandRunnerInDir
	defer func() { commandRunnerInDir = original }()

	workspaceRoot := t.TempDir()
	var capturedDir string
	var capturedArgs []string
	commandRunnerInDir = func(_ context.Context, dir string, _ string, args ...string) (string, string, error) {
		capturedDir = dir
		capturedArgs = append([]string(nil), args...)
		return "ok", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = workspaceRoot
	req := httptest.NewRequest(http.MethodPost, "/api/compose/down", bytes.NewBufferString(`{
		"content":"services:\n  app:\n    image: alpine:3.20\n",
		"project_name":"sandbox",
		"volumes":true,
		"remove_orphans":true
	}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	argsJoined := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsJoined, "down") || !strings.Contains(argsJoined, "--volumes") || !strings.Contains(argsJoined, "--remove-orphans") {
		t.Fatalf("expected down command to include optional flags, args: %v", capturedArgs)
	}
	expectedProjectDir := filepath.Join(workspaceRoot, ".open-sandbox", "compose", "sandbox")
	if capturedDir != expectedProjectDir {
		t.Fatalf("expected compose command dir %q, got %q", expectedProjectDir, capturedDir)
	}
	if _, err := os.Stat(expectedProjectDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected compose project artifacts to be removed, stat err=%v", err)
	}
}

func TestPrepareComposeProjectSanitizesProjectName(t *testing.T) {
	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()

	project, err := s.prepareComposeProject(ComposeRequest{
		ProjectName: " My Demo/Project ",
		Content:     "services:\n  app:\n    image: alpine:3.20\n",
	})
	if err != nil {
		t.Fatalf("expected compose project to be prepared: %v", err)
	}
	if project.ProjectName != "my-demo-project" {
		t.Fatalf("expected sanitized project name, got %q", project.ProjectName)
	}
	if !strings.HasPrefix(project.ProjectDir, filepath.Join(s.workspaceRoot, ".open-sandbox", "compose")) {
		t.Fatalf("expected project dir under managed compose root, got %q", project.ProjectDir)
	}
}

func TestComposeProjectNameFallsBackToContentHash(t *testing.T) {
	first := composeProjectName("", "services:\n  app:\n    image: alpine:3.20\n")
	second := composeProjectName("", "services:\n  app:\n    image: alpine:3.20\n")
	if first != second {
		t.Fatalf("expected deterministic fallback compose name, got %q and %q", first, second)
	}
	if !strings.HasPrefix(first, "compose-") {
		t.Fatalf("expected fallback compose name prefix, got %q", first)
	}
}

func TestResolveWorkspaceRootUsesHomeDirByDefault(t *testing.T) {
	t.Setenv("SANDBOX_WORKSPACE_DIR", "")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("expected home directory to be available: %v", err)
	}

	if got := resolveWorkspaceRoot(); got != filepath.Clean(homeDir) {
		t.Fatalf("expected workspace root %q, got %q", filepath.Clean(homeDir), got)
	}
}

func TestResolveWorkspaceRootUsesConfiguredPath(t *testing.T) {
	configured := t.TempDir()
	t.Setenv("SANDBOX_WORKSPACE_DIR", configured)

	if got := resolveWorkspaceRoot(); got != filepath.Clean(configured) {
		t.Fatalf("expected workspace root %q, got %q", filepath.Clean(configured), got)
	}
}

func TestCreateContainerEndpoint(t *testing.T) {
	var capturedName string
	var capturedImage string
	m := &mockDocker{
		containerCreateFn: func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
			capturedName = name
			capturedImage = config.Image
			return container.CreateResponse{ID: "new-container-id", Warnings: []string{}}, nil
		},
		containerStartFn: func(context.Context, string, container.StartOptions) error {
			return nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/create", bytes.NewBufferString(`{"image":"alpine:latest","name":"sandbox-1","cmd":["sleep","10"],"start":false}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if capturedName != "sandbox-1" || capturedImage != "alpine:latest" {
		t.Fatalf("unexpected create call values: name=%q image=%q", capturedName, capturedImage)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("new-container-id")) {
		t.Fatalf("expected container id in response: %s", w.Body.String())
	}
}

func TestCreateContainerEndpointStartsContainerWhenRequested(t *testing.T) {
	started := false
	m := &mockDocker{
		containerCreateFn: func(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "new-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, id string, _ container.StartOptions) error {
			started = id == "new-container-id"
			return nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/create", bytes.NewBufferString(`{"image":"alpine:latest","start":true}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !started {
		t.Fatal("expected container start to be called")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte(`"started":true`)) {
		t.Fatalf("expected started=true in response: %s", w.Body.String())
	}
}

func TestRestartContainerEndpoint(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"cid-123","Image":"ubuntu:24.04","Names":"sandbox-one","Status":"Up 5 minutes","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=admin-user"}` + "\n", "", nil
	}

	stopped := false
	started := false
	m := &mockDocker{
		containerInspectFn: func(context.Context, string) (container.InspectResponse, error) {
			return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}}}, nil
		},
		containerStopFn: func(_ context.Context, id string, _ container.StopOptions) error {
			stopped = id == "cid-123"
			return nil
		},
		containerStartFn: func(_ context.Context, id string, _ container.StartOptions) error {
			started = id == "cid-123"
			return nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/cid-123/restart", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !stopped || !started {
		t.Fatalf("expected container to be stopped then started, stopped=%v started=%v", stopped, started)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"restarted":true`)) {
		t.Fatalf("expected restarted=true in response: %s", w.Body.String())
	}
}

func TestCreateContainerEndpointParsesPortBindings(t *testing.T) {
	var capturedBindings nat.PortMap
	var capturedPorts nat.PortSet
	m := &mockDocker{
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			capturedBindings = hostConfig.PortBindings
			capturedPorts = config.ExposedPorts
			return container.CreateResponse{ID: "new-container-id"}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/create", bytes.NewBufferString(`{"image":"nginx:latest","ports":["8080:80"]}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(capturedBindings) == 0 || len(capturedPorts) == 0 {
		t.Fatalf("expected port bindings to be set, got bindings=%v ports=%v", capturedBindings, capturedPorts)
	}
}

func TestCreateContainerEndpointAppliesRuntimeLimits(t *testing.T) {
	t.Setenv("SANDBOX_RUNTIME_MEMORY_LIMIT", "512m")
	t.Setenv("SANDBOX_RUNTIME_CPU_LIMIT", "1.5")
	t.Setenv("SANDBOX_RUNTIME_PIDS_LIMIT", "256")

	m := &mockDocker{
		containerCreateFn: func(_ context.Context, _ *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if hostConfig.Resources.Memory != 512_000_000 {
				t.Fatalf("expected memory limit 512000000, got %d", hostConfig.Resources.Memory)
			}
			if hostConfig.Resources.NanoCPUs != 1_500_000_000 {
				t.Fatalf("expected cpu limit 1500000000, got %d", hostConfig.Resources.NanoCPUs)
			}
			if hostConfig.Resources.PidsLimit == nil || *hostConfig.Resources.PidsLimit != 256 {
				t.Fatalf("expected pids limit 256, got %+v", hostConfig.Resources.PidsLimit)
			}
			return container.CreateResponse{ID: "limited-container"}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/create", bytes.NewBufferString(`{"image":"alpine:latest","start":false}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestCreateContainerEndpointAutoPullsMissingImage(t *testing.T) {
	createCalls := 0
	pullCalled := false
	m := &mockDocker{
		containerCreateFn: func(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *ocispec.Platform, string) (container.CreateResponse, error) {
			createCalls++
			if createCalls == 1 {
				return container.CreateResponse{}, errors.New("No such image: alpine:latest")
			}
			return container.CreateResponse{ID: "created-after-pull"}, nil
		},
		imagePullFn: func(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
			pullCalled = true
			return io.NopCloser(bytes.NewReader([]byte("{}"))), nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/create", bytes.NewBufferString(`{"image":"alpine:latest","start":false}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !pullCalled {
		t.Fatal("expected image pull to be called when image is missing")
	}
	if createCalls != 2 {
		t.Fatalf("expected create to be retried, got %d calls", createCalls)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("created-after-pull")) {
		t.Fatalf("expected created container id in response: %s", w.Body.String())
	}
}

func TestListContainersEndpoint(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"cid-123","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"0.0.0.0:3000->3000/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=admin-user"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("sandbox-one")) {
		t.Fatalf("expected container response body: %s", w.Body.String())
	}
}

func TestListContainersIncludesServerGeneratedPreviewURLs(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return strings.Join([]string{
			`{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"3000/tcp,0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}`,
			`{"ID":"direct-container","Image":"nginx:latest","Names":"direct-one","Ports":"0.0.0.0:8080->80/tcp,3000/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=member-1"}`,
			`{"ID":"compose-container","Image":"nginx:latest","Names":"demo-web-1","Ports":"0.0.0.0:9000->8080/tcp,8081/tcp","Status":"Up 5 minutes","Labels":"com.docker.compose.project=demo,com.docker.compose.service=web"}`,
		}, "\n"), "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("expected compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("expected compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "member-1", Username: "alice"}); err != nil {
		t.Fatalf("expected compose owner metadata: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var containers []ContainerSummary
	if err := json.Unmarshal(w.Body.Bytes(), &containers); err != nil {
		t.Fatalf("decode containers response: %v", err)
	}
	if len(containers) != 3 {
		t.Fatalf("expected 3 visible containers, got %d", len(containers))
	}

	previewByID := map[string][]PreviewURL{}
	for _, item := range containers {
		previewByID[item.ContainerID] = item.PreviewURLs
	}
	if len(previewByID["sandbox-container"]) != 1 || previewByID["sandbox-container"][0].URL != "/auth/preview/launch/sandboxes/sandbox-1/80" {
		t.Fatalf("unexpected sandbox preview urls: %+v", previewByID["sandbox-container"])
	}
	if len(previewByID["direct-container"]) != 1 || previewByID["direct-container"][0].URL != "/auth/preview/launch/containers/ctr-123/80" {
		t.Fatalf("unexpected direct preview urls: %+v", previewByID["direct-container"])
	}
	if len(previewByID["compose-container"]) != 1 || previewByID["compose-container"][0].URL != "/auth/preview/launch/compose/demo/web/8080" {
		t.Fatalf("unexpected compose preview urls: %+v", previewByID["compose-container"])
	}
}

func TestListContainersIncludesPersistedDirectContainerPortSpecs(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"direct-container","Image":"nginx:latest","Names":"direct-one","Ports":"0.0.0.0:8080->80/tcp,3000/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	if err := s.writeDirectContainerSpec("ctr-123", CreateContainerRequest{Image: "nginx:latest", Ports: []string{"127.0.0.1:48080:80", "3000"}}); err != nil {
		t.Fatalf("write direct container spec: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var containers []ContainerSummary
	if err := json.Unmarshal(w.Body.Bytes(), &containers); err != nil {
		t.Fatalf("decode containers response: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 visible container, got %d", len(containers))
	}
	if len(containers[0].PortSpecs) != 2 || containers[0].PortSpecs[0] != "127.0.0.1:48080:80" || containers[0].PortSpecs[1] != "3000" {
		t.Fatalf("unexpected direct container port specs: %+v", containers[0].PortSpecs)
	}
}

func TestListContainersFiltersToCurrentUser(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return strings.Join([]string{
			`{"ID":"owned-sandbox","Image":"ubuntu:24.04","Names":"mine","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=member-1"}`,
			`{"ID":"direct-owned","Image":"alpine:3.20","Names":"direct","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=member-1"}`,
			`{"ID":"other-sandbox","Image":"ubuntu:24.04","Names":"other","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=member-2"}`,
			`{"ID":"compose-unowned","Image":"redis:7","Names":"compose","Status":"Up 1 minute","Labels":"com.docker.compose.project=shared"}`,
		}, "\n"), "", nil
	}

	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{
				{ID: "sandbox-1", ContainerID: "owned-sandbox", OwnerID: "member-1", OwnerUsername: "alice"},
				{ID: "sandbox-2", ContainerID: "other-sandbox", OwnerID: "member-2", OwnerUsername: "bob"},
			}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"owned-sandbox"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"direct-owned"`)) {
		t.Fatalf("expected owned containers in response: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"other-sandbox"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"compose-unowned"`)) {
		t.Fatalf("expected unowned containers to be filtered out: %s", w.Body.String())
	}
}

func TestListContainersFiltersAdminsToOwnedContainers(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return strings.Join([]string{
			`{"ID":"owned-sandbox","Image":"ubuntu:24.04","Names":"mine","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=admin-user"}`,
			`{"ID":"other-sandbox","Image":"ubuntu:24.04","Names":"other","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=member-2"}`,
		}, "\n"), "", nil
	}

	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{
				{ID: "sandbox-1", ContainerID: "owned-sandbox", OwnerID: "admin-user", OwnerUsername: "admin"},
				{ID: "sandbox-2", ContainerID: "other-sandbox", OwnerID: "member-2", OwnerUsername: "bob"},
			}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"owned-sandbox"`)) {
		t.Fatalf("expected owned admin container in response: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"other-sandbox"`)) {
		t.Fatalf("expected other user's container to be filtered out for admin: %s", w.Body.String())
	}
}

func TestContainerAccessRejectsOtherUsers(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"other-sandbox","Image":"ubuntu:24.04","Names":"other","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=member-2"}` + "\n", "", nil
	}

	stopped := false
	m := &mockDocker{
		containerStopFn: func(context.Context, string, container.StopOptions) error {
			stopped = true
			return nil
		},
	}
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{ID: "sandbox-2", ContainerID: "other-sandbox", OwnerID: "member-2", OwnerUsername: "bob"}}, nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/other-sandbox/stop", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if stopped {
		t.Fatal("expected unauthorized container stop to be blocked")
	}
}

func TestListContainersIncludesOwnedComposeProjects(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"compose-owned","Image":"redis:7","Names":"compose-owned","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=cache"}` + "\n", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("expected compose project dir: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "member-1", Username: "alice"}); err != nil {
		t.Fatalf("expected owner metadata to be written: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"compose-owned"`)) {
		t.Fatalf("expected owned compose container in response: %s", w.Body.String())
	}
}

func TestListContainersAdminFiltersToManagedWorkloads(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return strings.Join([]string{
			`{"ID":"sandbox-managed","Image":"ubuntu:24.04","Names":"sandbox-one","Status":"Up 1 minute","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=admin-user"}`,
			`{"ID":"direct-managed","Image":"alpine:3.20","Names":"direct-one","Status":"Up 1 minute","Labels":"open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=admin-user"}`,
			`{"ID":"compose-managed","Image":"redis:7","Names":"demo-cache-1","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=cache"}`,
			`{"ID":"compose-infra-client","Image":"open-sandbox-client","Names":"open-sandbox-client-1","Status":"Up 1 minute","Labels":"com.docker.compose.project=open-sandboxmigrate-k8s,com.docker.compose.service=client,com.docker.compose.project.config_files=/Users/shaikzeeshan/Code/open-sandbox.migrate-k8s/compose.yaml,com.docker.compose.project.working_dir=/Users/shaikzeeshan/Code/open-sandbox.migrate-k8s"}`,
			`{"ID":"compose-infra-server","Image":"open-sandbox-server","Names":"open-sandbox-server-1","Status":"Up 1 minute","Labels":"com.docker.compose.project=open-sandboxmigrate-k8s,com.docker.compose.service=server,com.docker.compose.project.config_files=/Users/shaikzeeshan/Code/open-sandbox.migrate-k8s/compose.yaml,com.docker.compose.project.working_dir=/Users/shaikzeeshan/Code/open-sandbox.migrate-k8s"}`,
			`{"ID":"compose-external","Image":"postgres:16","Names":"shared-db-1","Status":"Up 1 minute","Labels":"com.docker.compose.project=shared,com.docker.compose.service=db"}`,
			`{"ID":"runtime-external","Image":"busybox:1.36","Names":"scratch-box","Status":"Up 1 minute","Labels":""}`,
		}, "\n"), "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("expected compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  cache:\n    image: redis:7\n"), 0o600); err != nil {
		t.Fatalf("expected compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "admin-user", Username: "admin"}); err != nil {
		t.Fatalf("expected owner metadata to be written: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"sandbox-managed"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"direct-managed"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"compose-managed"`)) {
		t.Fatalf("expected managed workloads in response: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"compose-external"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"runtime-external"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"compose-infra-client"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"compose-infra-server"`)) {
		t.Fatalf("expected unmanaged workloads to be filtered out: %s", w.Body.String())
	}
}

func TestResetDirectContainerEndpoint(t *testing.T) {
	t.Setenv("SANDBOX_RUNTIME_MEMORY_LIMIT", "512m")
	t.Setenv("SANDBOX_RUNTIME_CPU_LIMIT", "1.5")
	t.Setenv("SANDBOX_RUNTIME_PIDS_LIMIT", "256")

	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"direct-1","Image":"alpine:3.20","Names":"direct-box","Status":"Up 1 minute","Labels":"open-sandbox.managed=true,open-sandbox.kind=direct,open-sandbox.managed_id=ctr-123,open-sandbox.owner_id=admin-user,open-sandbox.owner_username=admin"}` + "\n", "", nil
	}

	removed := false
	started := false
	createdName := ""
	m := &mockDocker{
		containerRemoveFn: func(_ context.Context, id string, _ container.RemoveOptions) error {
			removed = id == "direct-1"
			return nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
			createdName = name
			if config.Labels[labelOpenSandboxManagedID] != "ctr-123" {
				t.Fatalf("expected managed id label to be preserved, got %q", config.Labels[labelOpenSandboxManagedID])
			}
			if config.Labels[labelOpenSandboxKind] != managedKindDirect {
				t.Fatalf("expected direct kind label, got %q", config.Labels[labelOpenSandboxKind])
			}
			if hostConfig.AutoRemove {
				t.Fatal("expected auto remove to stay false")
			}
			if hostConfig.Resources.Memory != 512_000_000 {
				t.Fatalf("expected memory limit 512000000, got %d", hostConfig.Resources.Memory)
			}
			if hostConfig.Resources.NanoCPUs != 1_500_000_000 {
				t.Fatalf("expected cpu limit 1500000000, got %d", hostConfig.Resources.NanoCPUs)
			}
			if hostConfig.Resources.PidsLimit == nil || *hostConfig.Resources.PidsLimit != 256 {
				t.Fatalf("expected pids limit 256, got %+v", hostConfig.Resources.PidsLimit)
			}
			return container.CreateResponse{ID: "direct-2"}, nil
		},
		containerStartFn: func(_ context.Context, id string, _ container.StartOptions) error {
			started = id == "direct-2"
			return nil
		},
	}

	s := newTestServer(m)
	s.workspaceRoot = t.TempDir()
	if err := s.writeDirectContainerSpec("ctr-123", CreateContainerRequest{Image: "alpine:3.20", Name: "direct-box", Start: true}); err != nil {
		t.Fatalf("expected direct container spec to be written: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/containers/direct-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !removed || !started {
		t.Fatalf("expected direct container reset to recreate container, removed=%v started=%v", removed, started)
	}
	if createdName != "direct-box" {
		t.Fatalf("expected container name to be reused, got %q", createdName)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"container_id":"direct-2"`)) {
		t.Fatalf("expected replacement container id in response: %s", w.Body.String())
	}
}

func TestResetComposeContainerEndpoint(t *testing.T) {
	original := commandRunner
	originalInDir := commandRunnerInDir
	defer func() {
		commandRunner = original
		commandRunnerInDir = originalInDir
	}()

	callCount := 0
	commandRunner = func(_ context.Context, _ string, args ...string) (string, string, error) {
		if len(args) > 0 && args[0] == "stats" {
			return "", "", nil
		}
		callCount++
		if callCount == 1 {
			return `{"ID":"compose-old","Image":"redis:7","Names":"demo-cache-1","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=cache"}` + "\n", "", nil
		}
		return `{"ID":"compose-new","Image":"redis:7","Names":"demo-cache-1","Status":"Up 1 minute","Labels":"com.docker.compose.project=demo,com.docker.compose.service=cache"}` + "\n", "", nil
	}

	var capturedDir string
	var capturedArgs []string
	commandRunnerInDir = func(_ context.Context, dir string, _ string, args ...string) (string, string, error) {
		capturedDir = dir
		capturedArgs = append([]string(nil), args...)
		return "compose recreated", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("expected compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  cache:\n    image: redis:7\n"), 0o600); err != nil {
		t.Fatalf("expected compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "admin-user", Username: "admin"}); err != nil {
		t.Fatalf("expected owner metadata to be written: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/containers/compose-old/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if capturedDir != projectDir {
		t.Fatalf("expected compose command dir %q, got %q", projectDir, capturedDir)
	}
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "up -d --force-recreate cache") {
		t.Fatalf("expected compose reset args, got %v", capturedArgs)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"container_id":"compose-new"`)) {
		t.Fatalf("expected replacement compose container id in response: %s", w.Body.String())
	}
}

func TestWriteManagedComposeFileUsesRestrictedPermissions(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "compose-project")
	composeFile, err := writeManagedComposeFile(projectDir, "services:\n  app:\n    image: alpine:3.20\n")
	if err != nil {
		t.Fatalf("expected managed compose file to be written: %v", err)
	}

	projectInfo, err := os.Stat(projectDir)
	if err != nil {
		t.Fatalf("expected project dir to exist: %v", err)
	}
	if got := projectInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected project dir mode 0700, got %#o", got)
	}

	fileInfo, err := os.Stat(composeFile)
	if err != nil {
		t.Fatalf("expected compose file to exist: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected compose file mode 0600, got %#o", got)
	}
}

func TestPrepareComposeProjectLocksManagedDirectories(t *testing.T) {
	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()

	project, err := s.prepareComposeProject(ComposeRequest{
		ProjectName: "sandbox",
		Content:     "services:\n  app:\n    image: alpine:3.20\n",
	})
	if err != nil {
		t.Fatalf("expected compose project to be prepared: %v", err)
	}

	for _, dir := range []string{filepath.Join(s.workspaceRoot, ".open-sandbox"), filepath.Join(s.workspaceRoot, ".open-sandbox", "compose"), project.ProjectDir} {
		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Fatalf("expected managed dir %q: %v", dir, statErr)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("expected dir %q mode 0700, got %#o", dir, got)
		}
	}
}

func TestAuthorizeComposeProjectAccessRejectsForeignOwner(t *testing.T) {
	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("expected compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine:3.20\n"), 0o600); err != nil {
		t.Fatalf("expected compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "owner-1", Username: "alice"}); err != nil {
		t.Fatalf("expected owner metadata to be written: %v", err)
	}

	project := s.composeProjectContextForName("demo")
	shouldWriteOwner, err := s.authorizeComposeProjectAccess(AuthIdentity{UserID: "owner-2", Username: "bob", Role: roleMember}, project)
	if err == nil {
		t.Fatal("expected foreign owner access to be rejected")
	}
	if shouldWriteOwner {
		t.Fatal("expected foreign owner access to avoid owner writes")
	}

	owner, hasOwner, err := s.readComposeProjectOwnerMetadata(projectDir)
	if err != nil {
		t.Fatalf("expected owner metadata to remain readable: %v", err)
	}
	if !hasOwner || owner.UserID != "owner-1" {
		t.Fatalf("expected original owner metadata to remain intact: %+v", owner)
	}
}

func TestCreateSandboxEndpoint(t *testing.T) {
	setSandboxSecretsKey(t, "0123456789abcdef0123456789abcdef")
	createdSandbox := store.Sandbox{}
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image inspect for ubuntu:24.04, got %q", imageID)
			}
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/home/opencode"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			if !strings.HasPrefix(options.Name, "open-sandbox-") {
				t.Fatalf("expected volume name to be prefixed, got %q", options.Name)
			}
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			if !strings.Contains(containerName, "workspace") {
				t.Fatalf("expected generated container name, got %q", containerName)
			}
			if config.Image != "ubuntu:24.04" {
				t.Fatalf("expected ubuntu image, got %q", config.Image)
			}
			if config.WorkingDir != "/home/opencode" {
				t.Fatalf("expected image workdir /home/opencode, got %q", config.WorkingDir)
			}
			if len(hostConfig.Binds) != 1 || !strings.Contains(hostConfig.Binds[0], ":/home/opencode") {
				t.Fatalf("expected workspace bind mount, got %v", hostConfig.Binds)
			}
			if len(config.Env) != 3 || config.Env[2] != "SECRET_TOKEN=swordfish" {
				t.Fatalf("expected secret env in runtime config, got %+v", config.Env)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, containerID string, _ container.StartOptions) error {
			if containerID != "sandbox-container-id" {
				t.Fatalf("unexpected container id passed to start: %q", containerID)
			}
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(_ context.Context, sandbox store.Sandbox) error {
			createdSandbox = sandbox
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","env":["FOO=bar","HELLO=world"],"secret_env":["SECRET_TOKEN=swordfish"]}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	if createdSandbox.Name != "workspace" || createdSandbox.ContainerID != "sandbox-container-id" {
		t.Fatalf("unexpected sandbox persisted: %+v", createdSandbox)
	}
	if createdSandbox.WorkspaceDir != "/home/opencode" {
		t.Fatalf("expected persisted workspace dir /home/opencode, got %q", createdSandbox.WorkspaceDir)
	}
	if createdSandbox.OwnerID != "admin-user" || createdSandbox.OwnerUsername != "admin" {
		t.Fatalf("expected sandbox ownership to be set, got %+v", createdSandbox)
	}
	if len(createdSandbox.Env) != 2 || createdSandbox.Env[0] != "FOO=bar" || createdSandbox.Env[1] != "HELLO=world" {
		t.Fatalf("expected sandbox env to be persisted, got %+v", createdSandbox.Env)
	}
	if len(createdSandbox.SecretEnv) != 1 || createdSandbox.SecretEnv[0] == "SECRET_TOKEN=swordfish" || !strings.HasPrefix(createdSandbox.SecretEnv[0], "SECRET_TOKEN=") {
		t.Fatalf("expected encrypted sandbox secret env to be persisted, got %+v", createdSandbox.SecretEnv)
	}
	if len(createdSandbox.SecretEnvKeys) != 1 || createdSandbox.SecretEnvKeys[0] != "SECRET_TOKEN" {
		t.Fatalf("expected sandbox secret env keys to be persisted, got %+v", createdSandbox.SecretEnvKeys)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("sandbox-container-id")) {
		t.Fatalf("expected container id in response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"env":["FOO=bar","HELLO=world"]`)) {
		t.Fatalf("expected env in response: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"secret_env_keys":["SECRET_TOKEN"]`)) || bytes.Contains(w.Body.Bytes(), []byte("swordfish")) {
		t.Fatalf("expected secret env keys only in response: %s", w.Body.String())
	}
}

func TestCreateSandboxEndpointRollsBackWorkspaceVolumeWhenContainerCreateFails(t *testing.T) {
	createdVolume := ""
	removedVolume := ""
	removedVolumeForce := false
	containerRemoved := false

	m := &mockDocker{
		imageInspectFn: func(_ context.Context, _ string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			createdVolume = options.Name
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{}, errors.New("create boom")
		},
		containerRemoveFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			containerRemoved = true
			return nil
		},
		volumeRemoveFn: func(_ context.Context, volumeID string, force bool) error {
			removedVolume = volumeID
			removedVolumeForce = force
			return nil
		},
	}

	s := newTestServerWithStore(m, &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error {
		t.Fatal("expected sandbox not to be persisted")
		return nil
	}})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", w.Code, w.Body.String())
	}
	if createdVolume == "" {
		t.Fatal("expected workspace volume to be created")
	}
	if containerRemoved {
		t.Fatal("did not expect container rollback when create failed")
	}
	if removedVolume != createdVolume {
		t.Fatalf("expected rollback remove for workspace volume %q, got %q", createdVolume, removedVolume)
	}
	if !removedVolumeForce {
		t.Fatal("expected rollback volume remove with force=true")
	}
}

func TestCreateSandboxEndpointRollsBackContainerAndVolumeWhenStartFails(t *testing.T) {
	createdVolume := ""
	removedVolume := ""
	removedVolumeForce := false
	removedContainer := ""
	removeOptions := container.RemoveOptions{}

	m := &mockDocker{
		imageInspectFn: func(_ context.Context, _ string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			createdVolume = options.Name
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return errors.New("start boom")
		},
		containerRemoveFn: func(_ context.Context, containerID string, options container.RemoveOptions) error {
			removedContainer = containerID
			removeOptions = options
			return nil
		},
		volumeRemoveFn: func(_ context.Context, volumeID string, force bool) error {
			removedVolume = volumeID
			removedVolumeForce = force
			return nil
		},
	}

	s := newTestServerWithStore(m, &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error {
		t.Fatal("expected sandbox not to be persisted")
		return nil
	}})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", w.Code, w.Body.String())
	}
	if removedContainer != "sandbox-container-id" {
		t.Fatalf("expected rollback remove for sandbox-container-id, got %q", removedContainer)
	}
	if !removeOptions.Force || !removeOptions.RemoveVolumes {
		t.Fatalf("expected rollback remove options force+volumes, got %+v", removeOptions)
	}
	if createdVolume == "" {
		t.Fatal("expected workspace volume to be created")
	}
	if removedVolume != createdVolume {
		t.Fatalf("expected rollback remove for workspace volume %q, got %q", createdVolume, removedVolume)
	}
	if !removedVolumeForce {
		t.Fatal("expected rollback volume remove with force=true")
	}
}

func TestCreateSandboxEndpointLeavesWorkdirUnsetWhenImageHasNoWorkdir(t *testing.T) {
	createdSandbox := store.Sandbox{}
	volumeCreated := false
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image inspect for ubuntu:24.04, got %q", imageID)
			}
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			volumeCreated = true
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if config.WorkingDir != "" {
				t.Fatalf("expected workdir to be unset, got %q", config.WorkingDir)
			}
			if len(hostConfig.Binds) != 0 {
				t.Fatalf("expected no workspace bind mount, got %v", hostConfig.Binds)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(_ context.Context, sandbox store.Sandbox) error {
			createdSandbox = sandbox
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if volumeCreated {
		t.Fatal("expected sandbox volume creation to be skipped when workdir is unset")
	}
	if createdSandbox.WorkspaceDir != "" {
		t.Fatalf("expected persisted workspace dir to be empty, got %q", createdSandbox.WorkspaceDir)
	}
}

func TestCreateSandboxEndpointRejectsSecretEnvWithoutKey(t *testing.T) {
	setSandboxSecretsKey(t, "")
	s := newTestServerWithStore(&mockDocker{}, &mockSandboxStore{})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","secret_env":["SECRET_TOKEN=swordfish"]}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), sandboxSecretsKeyEnvVar) {
		t.Fatalf("expected missing key error, got %s", w.Body.String())
	}
}

func TestCreateSandboxEndpointAutoPullsImageBeforeResolvingWorkdir(t *testing.T) {
	inspectCalls := 0
	pullCalled := false
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image inspect for ubuntu:24.04, got %q", imageID)
			}
			inspectCalls++
			if inspectCalls == 1 {
				return image.InspectResponse{}, errors.New("No such image: ubuntu:24.04")
			}
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/home/opencode"}}}, nil
		},
		imagePullFn: func(_ context.Context, imageID string, _ image.PullOptions) (io.ReadCloser, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image pull for ubuntu:24.04, got %q", imageID)
			}
			pullCalled = true
			return io.NopCloser(bytes.NewReader([]byte("{}"))), nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if config.WorkingDir != "/home/opencode" {
				t.Fatalf("expected pulled image workdir /home/opencode, got %q", config.WorkingDir)
			}
			if len(hostConfig.Binds) != 1 || !strings.Contains(hostConfig.Binds[0], ":/home/opencode") {
				t.Fatalf("expected workspace bind mount, got %v", hostConfig.Binds)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(_ context.Context, _ store.Sandbox) error {
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !pullCalled {
		t.Fatal("expected image pull to be called when image inspect reports a missing image")
	}
	if inspectCalls != 2 {
		t.Fatalf("expected image inspect to be retried after pull, got %d calls", inspectCalls)
	}
}

func TestCreateSandboxEndpointNormalizesRequestedRootWorkdir(t *testing.T) {
	createdSandbox := store.Sandbox{}
	m := &mockDocker{
		imageInspectFn: func(context.Context, string) (image.InspectResponse, error) {
			t.Fatal("expected explicit workdir to skip image inspection")
			return image.InspectResponse{}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if config.WorkingDir != "/workspace" {
				t.Fatalf("expected normalized workdir /workspace, got %q", config.WorkingDir)
			}
			if len(hostConfig.Binds) != 1 || !strings.Contains(hostConfig.Binds[0], ":/workspace") {
				t.Fatalf("expected workspace bind mount, got %v", hostConfig.Binds)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(_ context.Context, sandbox store.Sandbox) error {
			createdSandbox = sandbox
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","workdir":"/"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if createdSandbox.WorkspaceDir != "/workspace" {
		t.Fatalf("expected persisted workspace dir /workspace, got %q", createdSandbox.WorkspaceDir)
	}
}

func TestCreateSandboxEndpointNormalizesImageRootWorkdir(t *testing.T) {
	createdSandbox := store.Sandbox{}
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image inspect for ubuntu:24.04, got %q", imageID)
			}
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if config.WorkingDir != "/workspace" {
				t.Fatalf("expected normalized workdir /workspace, got %q", config.WorkingDir)
			}
			if len(hostConfig.Binds) != 1 || !strings.Contains(hostConfig.Binds[0], ":/workspace") {
				t.Fatalf("expected workspace bind mount, got %v", hostConfig.Binds)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(_ context.Context, sandbox store.Sandbox) error {
			createdSandbox = sandbox
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if createdSandbox.WorkspaceDir != "/workspace" {
		t.Fatalf("expected persisted workspace dir /workspace, got %q", createdSandbox.WorkspaceDir)
	}
}

func TestCreateSandboxEndpointUsesImageDefaultCommandWhenRequested(t *testing.T) {
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, _ string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/home/opencode"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if config.Cmd != nil {
				t.Fatalf("expected sandbox to preserve image command, got %v", config.Cmd)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(_ context.Context, _ store.Sandbox) error {
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","use_image_default_cmd":true}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestListSandboxesFiltersToCurrentUser(t *testing.T) {
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{
				{ID: "sandbox-1", Name: "mine", ContainerID: "c1", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"},
				{ID: "sandbox-2", Name: "other", ContainerID: "c2", OwnerID: "member-2", OwnerUsername: "bob", Status: "running"},
			}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"sandbox-2"`)) {
		t.Fatalf("expected other user's sandbox to be filtered out: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"sandbox-1"`)) {
		t.Fatalf("expected own sandbox in response: %s", w.Body.String())
	}
}

func TestListSandboxesFiltersAdminsToOwnedSandboxes(t *testing.T) {
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{
				{ID: "sandbox-1", Name: "mine", ContainerID: "c1", OwnerID: "admin-user", OwnerUsername: "admin", Status: "running"},
				{ID: "sandbox-2", Name: "other", ContainerID: "c2", OwnerID: "member-2", OwnerUsername: "bob", Status: "running"},
			}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"sandbox-2"`)) {
		t.Fatalf("expected other user's sandbox to be filtered out for admin: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"sandbox-1"`)) {
		t.Fatalf("expected admin-owned sandbox in response: %s", w.Body.String())
	}
}

func TestListSandboxesIncludesServerGeneratedPreviewURLs(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"sandbox-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"3000/tcp,0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{ID: "sandbox-1", Name: "mine", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"}}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var sandboxes []SandboxResponse
	if err := json.Unmarshal(w.Body.Bytes(), &sandboxes); err != nil {
		t.Fatalf("decode sandboxes response: %v", err)
	}
	if len(sandboxes) != 1 {
		t.Fatalf("expected a single sandbox in response, got %d", len(sandboxes))
	}
	if len(sandboxes[0].PreviewURLs) != 1 || sandboxes[0].PreviewURLs[0].URL != "/auth/preview/launch/sandboxes/sandbox-1/80" {
		t.Fatalf("unexpected sandbox preview urls: %+v", sandboxes[0].PreviewURLs)
	}
}

func TestListSandboxesIncludesPersistedProxyConfig(t *testing.T) {
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{
				ID:            "sandbox-1",
				Name:          "mine",
				Image:         "ubuntu:24.04",
				ContainerID:   "sandbox-container",
				OwnerID:       "member-1",
				OwnerUsername: "alice",
				Status:        "running",
				ProxyConfig: map[int]traefikcfg.ServiceProxyConfig{
					3000: {
						RequestHeaders:  map[string]string{"X-Test": "one"},
						ResponseHeaders: map[string]string{"X-Frame-Options": "DENY"},
						PathPrefixStrip: "/api",
						SkipAuth:        true,
						CORS: &traefikcfg.CORSConfig{
							AllowOrigins: []string{"https://example.com"},
							MaxAge:       600,
						},
					},
				},
			}}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var sandboxes []SandboxResponse
	if err := json.Unmarshal(w.Body.Bytes(), &sandboxes); err != nil {
		t.Fatalf("decode sandboxes response: %v", err)
	}
	if len(sandboxes) != 1 {
		t.Fatalf("expected 1 sandbox, got %d", len(sandboxes))
	}
	if sandboxes[0].ProxyConfig["3000"] == nil {
		t.Fatalf("expected proxy config for port 3000, got %+v", sandboxes[0].ProxyConfig)
	}
	if sandboxes[0].ProxyConfig["3000"].RequestHeaders["X-Test"] != "one" {
		t.Fatalf("unexpected request headers: %+v", sandboxes[0].ProxyConfig["3000"])
	}
	if sandboxes[0].ProxyConfig["3000"].CORS == nil || sandboxes[0].ProxyConfig["3000"].CORS.MaxAge != 600 {
		t.Fatalf("unexpected cors config: %+v", sandboxes[0].ProxyConfig["3000"])
	}
	if !sandboxes[0].ProxyConfig["3000"].SkipAuth {
		t.Fatalf("expected skip_auth in response: %+v", sandboxes[0].ProxyConfig["3000"])
	}
	if sandboxes[0].ProxyConfig["3000"].PathPrefixStrip != "/api" {
		t.Fatalf("unexpected path_prefix_strip: %+v", sandboxes[0].ProxyConfig["3000"])
	}
}

func TestListSandboxesDeletesOwnedRecordsForMissingContainers(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"present-container","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"0.0.0.0:8080->80/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.sandbox_id=sandbox-1,open-sandbox.owner_id=member-1"}` + "\n", "", nil
	}

	deleted := make([]string, 0, 1)
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{
				{ID: "sandbox-1", Name: "present", ContainerID: "present-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"},
				{ID: "sandbox-2", Name: "missing", ContainerID: "missing-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"},
			}, nil
		},
		deleteSandboxFn: func(_ context.Context, sandboxID string) error {
			deleted = append(deleted, sandboxID)
			return nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if len(deleted) != 1 || deleted[0] != "sandbox-2" {
		t.Fatalf("expected missing sandbox record to be removed, got %+v", deleted)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"sandbox-2"`)) {
		t.Fatalf("expected missing sandbox to be removed from response: %s", w.Body.String())
	}
}

func TestListContainersReconcilesStaleDirectContainerSpecs(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	if err := ensurePrivateDir(filepath.Join(s.workspaceRoot, ".open-sandbox", "containers")); err != nil {
		t.Fatalf("prepare direct container spec root: %v", err)
	}
	staleSpecPath := filepath.Join(s.workspaceRoot, ".open-sandbox", "containers", "ctr-stale.json")
	if err := os.WriteFile(staleSpecPath, []byte(`{"image":"alpine:3.20"}`), 0o600); err != nil {
		t.Fatalf("write stale direct container spec: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(staleSpecPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale direct container spec to be removed, got err=%v", err)
	}
}

func TestComposeProjectEndpointsHideProjectsWithoutLiveServices(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	s := newTestServer(&mockDocker{})
	s.workspaceRoot = t.TempDir()
	projectDir := filepath.Join(s.workspaceRoot, ".open-sandbox", "compose", "demo")
	if err := ensurePrivateDir(projectDir); err != nil {
		t.Fatalf("prepare compose project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	if err := s.writeComposeProjectOwnerMetadata(projectDir, AuthIdentity{UserID: "member-1", Username: "alice"}); err != nil {
		t.Fatalf("write compose owner metadata: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/compose/projects", nil)
	listReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	listW := httptest.NewRecorder()
	s.Router().ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	var projects []ComposeProjectPreviewResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode compose projects response: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected no compose projects without live services, got %+v", projects)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/compose/projects/demo", nil)
	getReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	getW := httptest.NewRecorder()
	s.Router().ServeHTTP(getW, getReq)

	if getW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for compose project without live services, got %d (%s)", getW.Code, getW.Body.String())
	}
}

func TestSandboxAccessRejectsOtherUsers(t *testing.T) {
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "owner-1", OwnerUsername: "owner"}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes/sandbox-1", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-2", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateSandboxProxyConfigEndpointPersistsAndReturnsUpdatedSandbox(t *testing.T) {
	updatedAt := int64(200)
	updatedProxyConfig := map[int]traefikcfg.ServiceProxyConfig{}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(_ context.Context, sandboxID string) (store.Sandbox, error) {
			if sandboxID != "sandbox-1" {
				t.Fatalf("unexpected sandbox id %q", sandboxID)
			}
			if len(updatedProxyConfig) == 0 {
				return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running", UpdatedAt: 100}, nil
			}
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running", ProxyConfig: updatedProxyConfig, UpdatedAt: updatedAt}, nil
		},
		updateSandboxProxyConfigFn: func(_ context.Context, sandboxID string, proxyConfig map[int]traefikcfg.ServiceProxyConfig) error {
			if sandboxID != "sandbox-1" {
				t.Fatalf("unexpected sandbox id %q", sandboxID)
			}
			updatedProxyConfig = proxyConfig
			if len(proxyConfig) != 2 {
				t.Fatalf("expected 2 proxy config entries, got %d", len(proxyConfig))
			}
			if proxyConfig[3000].RequestHeaders["X-Test"] != "one" {
				t.Fatalf("unexpected parsed request headers: %+v", proxyConfig[3000])
			}
			if proxyConfig[3000].CORS == nil || proxyConfig[3000].CORS.MaxAge != 120 {
				t.Fatalf("unexpected parsed cors config: %+v", proxyConfig[3000])
			}
			if proxyConfig[8080].SkipAuth != true {
				t.Fatalf("expected skip auth for 8080, got %+v", proxyConfig[8080])
			}
			return nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/proxy-config", bytes.NewBufferString(`{"proxy_config":{"3000":{"request_headers":{"X-Test":"one"},"response_headers":{"X-Frame-Options":"DENY"},"cors":{"allow_origins":["https://example.com"],"max_age":120},"path_prefix_strip":"/api"},"8080":{"skip_auth":true}}}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var response SandboxResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode sandbox response: %v", err)
	}
	if response.ProxyConfig["3000"] == nil || response.ProxyConfig["3000"].RequestHeaders["X-Test"] != "one" {
		t.Fatalf("unexpected proxy config response: %+v", response.ProxyConfig)
	}
	if response.ProxyConfig["8080"] == nil || !response.ProxyConfig["8080"].SkipAuth {
		t.Fatalf("expected skip_auth on 8080 in response: %+v", response.ProxyConfig)
	}
	if response.UpdatedAt != updatedAt {
		t.Fatalf("expected updated_at %d, got %d", updatedAt, response.UpdatedAt)
	}
}

func TestUpdateSandboxProxyConfigEndpointAllowsClearingConfig(t *testing.T) {
	updatedCalled := false
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"}, nil
		},
		updateSandboxProxyConfigFn: func(_ context.Context, _ string, proxyConfig map[int]traefikcfg.ServiceProxyConfig) error {
			updatedCalled = true
			if proxyConfig != nil {
				t.Fatalf("expected nil proxy config when clearing, got %+v", proxyConfig)
			}
			return nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/proxy-config", bytes.NewBufferString(`{"proxy_config":{}}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !updatedCalled {
		t.Fatal("expected proxy config update to be called")
	}
}

func TestUpdateSandboxEnvEndpointRecreatesContainerAndPersistsRuntime(t *testing.T) {
	updatedSandbox := store.Sandbox{}
	inspectCalls := 0
	started := false
	var operations []string
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, containerID string) (container.InspectResponse, error) {
			inspectCalls++
			if inspectCalls > 1 {
				return container.InspectResponse{}, errors.New("not implemented")
			}
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected inspected container id %q", containerID)
			}
			return container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name: "/sandbox-workspace-abc123",
					HostConfig: &container.HostConfig{
						Binds: []string{"open-sandbox-volume:/workspace"},
						PortBindings: nat.PortMap{
							"3000/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "3000"}},
						},
					},
					State: &container.State{Running: true, Status: "running"},
				},
				Config: &container.Config{
					Image:      "alpine:3.20",
					Cmd:        []string{"sleep", "infinity"},
					WorkingDir: "/workspace",
					Labels: map[string]string{
						labelOpenSandboxManaged:       "true",
						labelOpenSandboxSandboxID:     "sandbox-1",
						labelOpenSandboxOwnerID:       "member-1",
						labelOpenSandboxOwnerUsername: "alice",
						labelOpenSandboxKind:          managedKindSandbox,
						labelOpenSandboxWorkerID:      localRuntimeWorkerID,
					},
					Env: []string{"OLD=value"},
				},
			}, nil
		},
		containerStopFn: func(_ context.Context, containerID string, _ container.StopOptions) error {
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected stopped container id %q", containerID)
			}
			operations = append(operations, "stop-old")
			return nil
		},
		containerRemoveFn: func(_ context.Context, containerID string, options container.RemoveOptions) error {
			if options.RemoveVolumes {
				t.Fatal("expected sandbox workspace volume to be preserved")
			}
			operations = append(operations, "remove-"+containerID)
			return nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, hostConfig *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
			if !strings.HasPrefix(name, "sandbox-workspace-abc123-replacement-") {
				t.Fatalf("expected replacement container name, got %q", name)
			}
			if len(config.Env) != 2 || config.Env[0] != "FOO=bar" || config.Env[1] != "BAR=baz" {
				t.Fatalf("expected updated env to be applied, got %+v", config.Env)
			}
			if config.Labels[labelOpenSandboxSandboxID] != "sandbox-1" {
				t.Fatalf("expected sandbox label to be preserved, got %+v", config.Labels)
			}
			if len(hostConfig.Binds) != 1 || hostConfig.Binds[0] != "open-sandbox-volume:/workspace" {
				t.Fatalf("expected workspace bind to be preserved, got %+v", hostConfig.Binds)
			}
			operations = append(operations, "create-new")
			return container.CreateResponse{ID: "sandbox-new"}, nil
		},
		containerStartFn: func(_ context.Context, containerID string, _ container.StartOptions) error {
			if containerID != "sandbox-new" {
				t.Fatalf("unexpected started container id %q", containerID)
			}
			started = true
			operations = append(operations, "start-new")
			return nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(_ context.Context, sandboxID string) (store.Sandbox, error) {
			if sandboxID != "sandbox-1" {
				t.Fatalf("unexpected sandbox id %q", sandboxID)
			}
			if updatedSandbox.ID != "" {
				return updatedSandbox, nil
			}
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-old", Image: "alpine:3.20", WorkspaceDir: "/workspace", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"}, nil
		},
		updateSandboxRuntimeFn: func(_ context.Context, sandboxID string, containerID string, env []string, secretEnv []string, secretEnvKeys []string, status string) error {
			if sandboxID != "sandbox-1" {
				t.Fatalf("unexpected sandbox id %q", sandboxID)
			}
			if containerID != "sandbox-new" {
				t.Fatalf("expected new container id to be stored, got %q", containerID)
			}
			if len(env) != 2 || env[0] != "FOO=bar" || env[1] != "BAR=baz" {
				t.Fatalf("expected env to be stored, got %+v", env)
			}
			if len(secretEnv) != 0 || len(secretEnvKeys) != 0 {
				t.Fatalf("expected no secret env to be stored, got %+v %+v", secretEnv, secretEnvKeys)
			}
			if status != "running" {
				t.Fatalf("expected running status, got %q", status)
			}
			operations = append(operations, "update-store")
			updatedSandbox = store.Sandbox{ID: "sandbox-1", ContainerID: containerID, Image: "alpine:3.20", WorkspaceDir: "/workspace", OwnerID: "member-1", OwnerUsername: "alice", Status: status, Env: append([]string(nil), env...), UpdatedAt: 200}
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/env", bytes.NewBufferString(`{"env":["FOO=bar","BAR=baz"]}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !started {
		t.Fatal("expected replacement sandbox container to start")
	}
	expectedOperations := []string{"stop-old", "create-new", "start-new", "update-store", "remove-sandbox-old"}
	if strings.Join(operations, ",") != strings.Join(expectedOperations, ",") {
		t.Fatalf("unexpected operation order: got %v want %v", operations, expectedOperations)
	}

	var response SandboxResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode sandbox response: %v", err)
	}
	if response.ContainerID != "sandbox-new" {
		t.Fatalf("expected updated container id, got %q", response.ContainerID)
	}
	if len(response.Env) != 2 || response.Env[0] != "FOO=bar" || response.Env[1] != "BAR=baz" {
		t.Fatalf("expected updated env response, got %+v", response.Env)
	}
}

func TestUpdateSandboxEnvEndpointMergesSecretEnvAndRespondsWithKeysOnly(t *testing.T) {
	setSandboxSecretsKey(t, "0123456789abcdef0123456789abcdef")
	codec, err := newSandboxSecretEnvCodecFromEnv()
	if err != nil {
		t.Fatalf("load secret env codec: %v", err)
	}
	secretState, err := codec.encrypt(map[string]string{"KEEP": "value", "REPLACE": "old"})
	if err != nil {
		t.Fatalf("encrypt secret env: %v", err)
	}
	updatedSandbox := store.Sandbox{}
	var operations []string
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, containerID string) (container.InspectResponse, error) {
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected inspected container id %q", containerID)
			}
			return container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:       "/sandbox-workspace-abc123",
					HostConfig: &container.HostConfig{Binds: []string{"open-sandbox-volume:/workspace"}},
					State:      &container.State{Running: true, Status: "running"},
				},
				Config: &container.Config{
					Image: "alpine:3.20",
					Labels: map[string]string{
						labelOpenSandboxManaged:       "true",
						labelOpenSandboxSandboxID:     "sandbox-1",
						labelOpenSandboxOwnerID:       "member-1",
						labelOpenSandboxOwnerUsername: "alice",
						labelOpenSandboxKind:          managedKindSandbox,
						labelOpenSandboxWorkerID:      localRuntimeWorkerID,
					},
				},
			}, nil
		},
		containerStopFn: func(_ context.Context, containerID string, _ container.StopOptions) error {
			operations = append(operations, "stop-"+containerID)
			return nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			expectedEnv := []string{"VISIBLE=1", "KEEP=value", "NEW=fresh", "REPLACE=new"}
			if strings.Join(config.Env, ",") != strings.Join(expectedEnv, ",") {
				t.Fatalf("unexpected runtime env: got %+v want %+v", config.Env, expectedEnv)
			}
			operations = append(operations, "create-new")
			return container.CreateResponse{ID: "sandbox-new"}, nil
		},
		containerStartFn: func(_ context.Context, containerID string, _ container.StartOptions) error {
			operations = append(operations, "start-"+containerID)
			return nil
		},
		containerRemoveFn: func(_ context.Context, containerID string, _ container.RemoveOptions) error {
			operations = append(operations, "remove-"+containerID)
			return nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(_ context.Context, sandboxID string) (store.Sandbox, error) {
			if updatedSandbox.ID != "" {
				return updatedSandbox, nil
			}
			return store.Sandbox{ID: sandboxID, ContainerID: "sandbox-old", Image: "alpine:3.20", WorkspaceDir: "/workspace", OwnerID: "member-1", OwnerUsername: "alice", Status: "running", SecretEnv: append([]string(nil), secretState.EncryptedEnv...), SecretEnvKeys: append([]string(nil), secretState.Keys...)}, nil
		},
		updateSandboxRuntimeFn: func(_ context.Context, sandboxID string, containerID string, env []string, secretEnv []string, secretEnvKeys []string, status string) error {
			if strings.Join(env, ",") != "VISIBLE=1" {
				t.Fatalf("unexpected visible env: %+v", env)
			}
			if strings.Join(secretEnvKeys, ",") != "KEEP,NEW,REPLACE" {
				t.Fatalf("unexpected secret env keys: %+v", secretEnvKeys)
			}
			for _, entry := range secretEnv {
				if strings.Contains(entry, "value") || strings.Contains(entry, "fresh") || strings.Contains(entry, "new") {
					t.Fatalf("expected encrypted secret env, got %+v", secretEnv)
				}
			}
			updatedSandbox = store.Sandbox{ID: sandboxID, ContainerID: containerID, Image: "alpine:3.20", WorkspaceDir: "/workspace", OwnerID: "member-1", OwnerUsername: "alice", Status: status, Env: env, SecretEnv: secretEnv, SecretEnvKeys: secretEnvKeys}
			operations = append(operations, "update-store")
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/env", bytes.NewBufferString(`{"env":["VISIBLE=1"],"secret_env":["REPLACE=new","NEW=fresh"],"remove_secret_env_keys":["MISSING"]}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "value") || strings.Contains(w.Body.String(), "fresh") || strings.Contains(w.Body.String(), "REPLACE=new") {
		t.Fatalf("expected response not to leak secret values: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"secret_env_keys":["KEEP","NEW","REPLACE"]`) {
		t.Fatalf("expected secret env keys in response: %s", w.Body.String())
	}
	if strings.Join(operations, ",") != "stop-sandbox-old,create-new,start-sandbox-new,update-store,remove-sandbox-old" {
		t.Fatalf("unexpected operations: %+v", operations)
	}
}

func TestUpdateSandboxEnvEndpointRejectsWhenStoredSecretsNeedDecryptionWithoutKey(t *testing.T) {
	setSandboxSecretsKey(t, "")
	inspected := false
	m := &mockDocker{
		containerInspectFn: func(context.Context, string) (container.InspectResponse, error) {
			inspected = true
			return container.InspectResponse{}, nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(_ context.Context, sandboxID string) (store.Sandbox, error) {
			return store.Sandbox{ID: sandboxID, ContainerID: "sandbox-old", OwnerID: "member-1", OwnerUsername: "alice", Status: "running", SecretEnv: []string{"SECRET_TOKEN=encrypted"}, SecretEnvKeys: []string{"SECRET_TOKEN"}}, nil
		},
	}
	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/env", bytes.NewBufferString(`{"env":["VISIBLE=1"]}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if inspected {
		t.Fatal("expected request to fail before runtime mutation")
	}
	if !strings.Contains(w.Body.String(), sandboxSecretsKeyEnvVar) {
		t.Fatalf("expected missing key error, got %s", w.Body.String())
	}
}

func TestUpdateSandboxEnvEndpointRestartsOriginalWhenReplacementStartFails(t *testing.T) {
	inspectCalls := 0
	storeUpdated := false
	var operations []string
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, containerID string) (container.InspectResponse, error) {
			inspectCalls++
			if inspectCalls > 1 {
				return container.InspectResponse{}, errors.New("not implemented")
			}
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected inspected container id %q", containerID)
			}
			return container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:       "/sandbox-workspace-abc123",
					HostConfig: &container.HostConfig{Binds: []string{"open-sandbox-volume:/workspace"}},
					State:      &container.State{Running: true, Status: "running"},
				},
				Config: &container.Config{
					Image: "alpine:3.20",
					Labels: map[string]string{
						labelOpenSandboxManaged:       "true",
						labelOpenSandboxSandboxID:     "sandbox-1",
						labelOpenSandboxOwnerID:       "member-1",
						labelOpenSandboxOwnerUsername: "alice",
						labelOpenSandboxKind:          managedKindSandbox,
						labelOpenSandboxWorkerID:      localRuntimeWorkerID,
					},
				},
			}, nil
		},
		containerStopFn: func(_ context.Context, containerID string, _ container.StopOptions) error {
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected stopped container id %q", containerID)
			}
			operations = append(operations, "stop-old")
			return nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
			if !strings.HasPrefix(name, "sandbox-workspace-abc123-replacement-") {
				t.Fatalf("expected replacement container name, got %q", name)
			}
			operations = append(operations, "create-new")
			return container.CreateResponse{ID: "sandbox-new"}, nil
		},
		containerStartFn: func(_ context.Context, containerID string, _ container.StartOptions) error {
			operations = append(operations, "start-"+containerID)
			if containerID == "sandbox-new" {
				return errors.New("boom")
			}
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected started container id %q", containerID)
			}
			return nil
		},
		containerRemoveFn: func(_ context.Context, containerID string, options container.RemoveOptions) error {
			if options.RemoveVolumes {
				t.Fatal("expected sandbox workspace volume to be preserved")
			}
			operations = append(operations, "remove-"+containerID)
			return nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(_ context.Context, sandboxID string) (store.Sandbox, error) {
			if sandboxID != "sandbox-1" {
				t.Fatalf("unexpected sandbox id %q", sandboxID)
			}
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-old", Image: "alpine:3.20", WorkspaceDir: "/workspace", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"}, nil
		},
		updateSandboxRuntimeFn: func(context.Context, string, string, []string, []string, []string, string) error {
			storeUpdated = true
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/env", bytes.NewBufferString(`{"env":["FOO=bar"]}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", w.Code, w.Body.String())
	}
	if storeUpdated {
		t.Fatal("expected sandbox store runtime to remain unchanged")
	}
	expectedOperations := []string{"stop-old", "create-new", "start-sandbox-new", "remove-sandbox-new", "start-sandbox-old"}
	if strings.Join(operations, ",") != strings.Join(expectedOperations, ",") {
		t.Fatalf("unexpected operation order: got %v want %v", operations, expectedOperations)
	}
}

func TestUpdateSandboxEnvEndpointRollsBackWhenStoreUpdateFails(t *testing.T) {
	inspectCalls := 0
	var operations []string
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, containerID string) (container.InspectResponse, error) {
			inspectCalls++
			if inspectCalls > 1 {
				return container.InspectResponse{}, errors.New("not implemented")
			}
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected inspected container id %q", containerID)
			}
			return container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:       "/sandbox-workspace-abc123",
					HostConfig: &container.HostConfig{Binds: []string{"open-sandbox-volume:/workspace"}},
					State:      &container.State{Running: true, Status: "running"},
				},
				Config: &container.Config{
					Image: "alpine:3.20",
					Labels: map[string]string{
						labelOpenSandboxManaged:       "true",
						labelOpenSandboxSandboxID:     "sandbox-1",
						labelOpenSandboxOwnerID:       "member-1",
						labelOpenSandboxOwnerUsername: "alice",
						labelOpenSandboxKind:          managedKindSandbox,
						labelOpenSandboxWorkerID:      localRuntimeWorkerID,
					},
				},
			}, nil
		},
		containerStopFn: func(_ context.Context, containerID string, _ container.StopOptions) error {
			if containerID != "sandbox-old" {
				t.Fatalf("unexpected stopped container id %q", containerID)
			}
			operations = append(operations, "stop-old")
			return nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
			if !strings.HasPrefix(name, "sandbox-workspace-abc123-replacement-") {
				t.Fatalf("expected replacement container name, got %q", name)
			}
			operations = append(operations, "create-new")
			return container.CreateResponse{ID: "sandbox-new"}, nil
		},
		containerStartFn: func(_ context.Context, containerID string, _ container.StartOptions) error {
			operations = append(operations, "start-"+containerID)
			if containerID != "sandbox-new" && containerID != "sandbox-old" {
				t.Fatalf("unexpected started container id %q", containerID)
			}
			return nil
		},
		containerRemoveFn: func(_ context.Context, containerID string, options container.RemoveOptions) error {
			if options.RemoveVolumes {
				t.Fatal("expected sandbox workspace volume to be preserved")
			}
			operations = append(operations, "remove-"+containerID)
			return nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(_ context.Context, sandboxID string) (store.Sandbox, error) {
			if sandboxID != "sandbox-1" {
				t.Fatalf("unexpected sandbox id %q", sandboxID)
			}
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-old", Image: "alpine:3.20", WorkspaceDir: "/workspace", OwnerID: "member-1", OwnerUsername: "alice", Status: "running"}, nil
		},
		updateSandboxRuntimeFn: func(context.Context, string, string, []string, []string, []string, string) error {
			operations = append(operations, "update-store")
			return errors.New("store boom")
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPatch, "/api/sandboxes/sandbox-1/env", bytes.NewBufferString(`{"env":["FOO=bar"]}`))
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", w.Code, w.Body.String())
	}
	expectedOperations := []string{"stop-old", "create-new", "start-sandbox-new", "update-store", "remove-sandbox-new", "start-sandbox-old"}
	if strings.Join(operations, ",") != strings.Join(expectedOperations, ",") {
		t.Fatalf("unexpected operation order: got %v want %v", operations, expectedOperations)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("rollback failed")) {
		t.Fatalf("expected rollback to succeed, got response %s", w.Body.String())
	}
}

func TestSandboxResponseIncludesPortSpecs(t *testing.T) {
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{
				ID:            "sandbox-1",
				Name:          "mine",
				ContainerID:   "sandbox-container",
				OwnerID:       "member-1",
				OwnerUsername: "alice",
				Env:           []string{"FOO=bar", "HELLO=world"},
				Status:        "running",
				PortSpecs:     []string{"127.0.0.1:8080:80", "3000"},
			}}, nil
		},
	}

	s := newTestServerWithStore(&mockDocker{}, sandboxStore)
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var sandboxes []SandboxResponse
	if err := json.Unmarshal(w.Body.Bytes(), &sandboxes); err != nil {
		t.Fatalf("decode sandboxes response: %v", err)
	}
	if len(sandboxes) != 1 {
		t.Fatalf("expected 1 sandbox, got %d", len(sandboxes))
	}
	if len(sandboxes[0].PortSpecs) != 2 || sandboxes[0].PortSpecs[0] != "127.0.0.1:8080:80" || sandboxes[0].PortSpecs[1] != "3000" {
		t.Fatalf("unexpected sandbox port specs: %+v", sandboxes[0].PortSpecs)
	}
	if len(sandboxes[0].Env) != 2 || sandboxes[0].Env[0] != "FOO=bar" || sandboxes[0].Env[1] != "HELLO=world" {
		t.Fatalf("unexpected sandbox env: %+v", sandboxes[0].Env)
	}
}

func TestSandboxResponsesExposeSecretKeysWithoutValues(t *testing.T) {
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{
				ID:            "sandbox-1",
				Name:          "mine",
				ContainerID:   "sandbox-container",
				OwnerID:       "member-1",
				OwnerUsername: "alice",
				Env:           []string{"FOO=bar"},
				SecretEnv:     []string{"SECRET_TOKEN=encrypted"},
				SecretEnvKeys: []string{"SECRET_TOKEN"},
				Status:        "running",
			}}, nil
		},
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", Name: "mine", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice", Env: []string{"FOO=bar"}, SecretEnv: []string{"SECRET_TOKEN=encrypted"}, SecretEnvKeys: []string{"SECRET_TOKEN"}, Status: "running"}, nil
		},
	}
	s := newTestServerWithStore(&mockDocker{}, sandboxStore)

	listReq := httptest.NewRequest(http.MethodGet, "/api/sandboxes", nil)
	listReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	listResp := httptest.NewRecorder()
	s.Router().ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), `"secret_env_keys":["SECRET_TOKEN"]`) || strings.Contains(listResp.Body.String(), "encrypted") {
		t.Fatalf("expected list response to expose keys only: %s", listResp.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/sandboxes/sandbox-1", nil)
	getReq.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	getResp := httptest.NewRecorder()
	s.Router().ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), `"secret_env_keys":["SECRET_TOKEN"]`) || strings.Contains(getResp.Body.String(), "encrypted") {
		t.Fatalf("expected get response to expose keys only: %s", getResp.Body.String())
	}
}

func TestParseSandboxPortProxyConfigsRejectsMalformedPortKeys(t *testing.T) {
	got := parseSandboxPortProxyConfigs(map[string]*SandboxPortProxyConfig{
		"3000/tcp": {
			SkipAuth: true,
		},
		"3000abc": {
			RequestHeaders: map[string]string{"X-Test": "bad"},
		},
		" 8080 ": {
			PathPrefixStrip: " /api ",
		},
	})

	if len(got) != 1 {
		t.Fatalf("expected only strictly numeric ports to be accepted, got %+v", got)
	}
	if got[8080].PathPrefixStrip != "/api" {
		t.Fatalf("expected trimmed config for port 8080, got %+v", got[8080])
	}
}

func TestDeleteSandboxRemovesRecordWhenContainerAlreadyMissing(t *testing.T) {
	deletedSandboxID := ""
	m := &mockDocker{
		containerRemoveFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
			return errdefs.NotFound(errors.New("no such container"))
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "member-1", OwnerUsername: "alice"}, nil
		},
		deleteSandboxFn: func(_ context.Context, sandboxID string) error {
			deletedSandboxID = sandboxID
			return nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodDelete, "/api/sandboxes/sandbox-1", nil)
	req.Header.Set("Authorization", "Bearer "+signedTokenFor(t, AuthIdentity{UserID: "member-1", Username: "alice", Role: roleMember}))
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if deletedSandboxID != "sandbox-1" {
		t.Fatalf("expected sandbox record to be deleted, got %q", deletedSandboxID)
	}
}

func TestSandboxExecEndpointUsesSandboxContainer(t *testing.T) {
	hijacked := fakeHijackedResponse([]byte("sandbox output\n"), []byte(""))
	m := &mockDocker{
		containerExecCreateFn: func(_ context.Context, containerID string, _ container.ExecOptions) (container.ExecCreateResponse, error) {
			if containerID != "sandbox-container" {
				t.Fatalf("expected sandbox container id, got %q", containerID)
			}
			return container.ExecCreateResponse{ID: "exec-789"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return hijacked, nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/exec", bytes.NewBufferString(`{"cmd":["pwd"]}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("sandbox output")) {
		t.Fatalf("expected sandbox exec output in response: %s", w.Body.String())
	}
}

func TestExecEndpoint(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"c-1","Image":"ubuntu:24.04","Names":"sandbox-one","Status":"Up 5 minutes","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=admin-user"}` + "\n", "", nil
	}

	hijacked := fakeHijackedResponse([]byte("command output\n"), []byte(""))
	m := &mockDocker{
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{ID: "exec-123"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return hijacked, nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/containers/c-1/exec", bytes.NewBufferString(`{"cmd":["sh","-lc","echo hi"],"detach":false,"tty":false}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("command output")) {
		t.Fatalf("expected command output in response: %s", w.Body.String())
	}
}

func TestSandboxTerminalWebSocket(t *testing.T) {
	serverConn, peerConn := net.Pipe()
	t.Cleanup(func() { _ = peerConn.Close() })

	resizeCalls := make([]container.ResizeOptions, 0, 1)
	resizeReceived := make(chan container.ResizeOptions, 1)
	receivedInput := make(chan string, 1)
	keepSessionOpen := make(chan struct{})
	go func() {
		defer close(receivedInput)
		_, _ = peerConn.Write([]byte("terminal ready\r\n"))
		buffer := make([]byte, 64)
		_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		count, err := peerConn.Read(buffer)
		if err == nil && count > 0 {
			receivedInput <- string(buffer[:count])
		}
		<-keepSessionOpen
		_ = peerConn.Close()
	}()
	defer close(keepSessionOpen)

	m := &mockDocker{
		containerExecCreateFn: func(_ context.Context, containerID string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			if containerID != "sandbox-container" {
				t.Fatalf("expected sandbox container id, got %q", containerID)
			}
			if !options.AttachStdin || !options.AttachStdout || !options.Tty {
				t.Fatalf("expected interactive exec options, got %+v", options)
			}
			if options.WorkingDir != "/workspace" {
				t.Fatalf("expected workspace dir /workspace, got %q", options.WorkingDir)
			}
			if len(options.Cmd) != 3 || !strings.Contains(options.Cmd[2], "cd '/workspace'") {
				t.Fatalf("expected terminal shell command to cd into /workspace, got %v", options.Cmd)
			}
			if options.ConsoleSize == nil || options.ConsoleSize[0] != 32 || options.ConsoleSize[1] != 100 {
				t.Fatalf("expected initial console size [32 100], got %+v", options.ConsoleSize)
			}
			return container.ExecCreateResponse{ID: "exec-ws"}, nil
		},
		containerExecAttachFn: func(_ context.Context, execID string, options container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			if execID != "exec-ws" {
				t.Fatalf("expected exec id exec-ws, got %q", execID)
			}
			if !options.Tty || options.ConsoleSize == nil || options.ConsoleSize[0] != 32 || options.ConsoleSize[1] != 100 {
				t.Fatalf("expected tty attach with console size, got %+v", options)
			}
			return dockertypes.HijackedResponse{Conn: serverConn, Reader: bufio.NewReader(serverConn)}, nil
		},
		containerExecResizeFn: func(_ context.Context, execID string, options container.ResizeOptions) error {
			if execID != "exec-ws" {
				t.Fatalf("expected resize for exec-ws, got %q", execID)
			}
			resizeCalls = append(resizeCalls, options)
			select {
			case resizeReceived <- options:
			default:
			}
			return nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	httpServer := httptest.NewServer(s.Router())
	t.Cleanup(httpServer.Close)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/sandboxes/sandbox-1/terminal/ws?cols=100&rows=32"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Cookie": []string{sessionCookieName + "=" + signedTestToken(t)},
		"Origin": []string{"http://localhost:5173"},
	})
	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial failed: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer conn.Close()

	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read terminal output: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("expected binary terminal output, got message type %d", messageType)
	}
	if string(payload) != "terminal ready\r\n" {
		t.Fatalf("unexpected terminal output %q", string(payload))
	}

	if err := conn.WriteJSON(terminalClientMessage{Type: "input", Data: "pwd\n"}); err != nil {
		t.Fatalf("failed to send terminal input: %v", err)
	}
	if err := conn.WriteJSON(terminalClientMessage{Type: "resize", Cols: 140, Rows: 48}); err != nil {
		t.Fatalf("failed to send terminal resize: %v", err)
	}

	select {
	case input := <-receivedInput:
		if input != "pwd\n" {
			t.Fatalf("expected terminal input pwd\\n, got %q", input)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal input")
	}

	select {
	case <-resizeReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal resize")
	}
	if len(resizeCalls) != 1 {
		t.Fatalf("expected 1 resize call, got %d", len(resizeCalls))
	}
	if resizeCalls[0].Width != 140 || resizeCalls[0].Height != 48 {
		t.Fatalf("unexpected resize call: %+v", resizeCalls[0])
	}
}

func TestTerminalWebSocketAllowsSameHostOriginBehindProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes/sandbox-1/terminal/ws?cols=100&rows=32", nil)
	req.Host = "192.168.0.8"
	req.Header.Set("Origin", "http://192.168.0.8:8010")
	req.Header.Set("X-Forwarded-Host", "192.168.0.8")
	req.Header.Set("X-Forwarded-Proto", "http")

	allowOrigin := buildAllowOriginFunc(loadAllowedOrigins())
	allowed := allowOrigin("http://192.168.0.8:8010") ||
		requestOriginMatchesForwardedHost(req, "http://192.168.0.8:8010") ||
		requestOriginHostMatchesForwardedHost(req, "http://192.168.0.8:8010")
	if !allowed {
		t.Fatal("expected websocket origin to be allowed for proxied same-host request")
	}
}

func TestTerminalWebSocketAllowsHTTPSOriginWhenForwardedProtoIsHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/sandboxes/sandbox-1/terminal/ws?cols=100&rows=32", nil)
	req.Host = "sandbox.shaiks.space"
	req.Header.Set("Origin", "https://sandbox.shaiks.space")
	req.Header.Set("X-Forwarded-Host", "sandbox.shaiks.space")
	req.Header.Set("X-Forwarded-Proto", "http")

	allowOrigin := buildAllowOriginFunc(loadAllowedOrigins())
	allowed := allowOrigin("https://sandbox.shaiks.space") ||
		requestOriginMatchesForwardedHost(req, "https://sandbox.shaiks.space") ||
		requestOriginHostMatchesForwardedHost(req, "https://sandbox.shaiks.space")
	if !allowed {
		t.Fatal("expected websocket origin to be allowed when host matches behind TLS-terminating proxy")
	}
}

func TestGitCloneEndpointBuildsExpectedCommand(t *testing.T) {
	var capturedCmd []string
	hijacked := fakeHijackedResponse([]byte(""), []byte(""))
	m := &mockDocker{
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			capturedCmd = append([]string{}, options.Cmd...)
			return container.ExecCreateResponse{ID: "exec-456"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return hijacked, nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/git/clone", bytes.NewBufferString(`{"container_id":"cid","repo_url":"https://github.com/example/repo.git","target_path":"/workspace/repo","branch":"main","single_branch":true,"depth":1,"filter":"blob:none"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	want := []string{"git", "clone", "--branch", "main", "--single-branch", "--depth", "1", "--filter", "blob:none", "https://github.com/example/repo.git", "/workspace/repo"}
	if len(capturedCmd) != len(want) {
		t.Fatalf("unexpected command length: got %v", capturedCmd)
	}
	for i := range want {
		if capturedCmd[i] != want[i] {
			t.Fatalf("unexpected command at %d: got %q, want %q", i, capturedCmd[i], want[i])
		}
	}
}

func TestGitCloneEndpointRejectsNonPositiveDepth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth int
	}{
		{name: "zero", depth: 0},
		{name: "negative", depth: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execCalled := false
			m := &mockDocker{
				containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
					execCalled = true
					return container.ExecCreateResponse{}, nil
				},
			}

			s := newTestServer(m)
			req := httptest.NewRequest(http.MethodPost, "/api/git/clone", bytes.NewBufferString(fmt.Sprintf(`{"container_id":"cid","repo_url":"https://github.com/example/repo.git","target_path":"/workspace/repo","depth":%d}`, tc.depth)))
			setAuthHeader(t, req)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			s.Router().ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
			}
			if execCalled {
				t.Fatal("expected clone exec not to run")
			}
			if !strings.Contains(w.Body.String(), "depth must be a positive integer") {
				t.Fatalf("expected depth validation error, got %s", w.Body.String())
			}
		})
	}
}

func TestGitCloneEndpointRejectsInvalidBaseCommit(t *testing.T) {
	execCalled := false
	m := &mockDocker{
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			execCalled = true
			return container.ExecCreateResponse{}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/git/clone", bytes.NewBufferString(`{"container_id":"cid","repo_url":"https://github.com/example/repo.git","target_path":"/workspace/repo","base_commit":"abc123; touch /tmp/pwned"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if execCalled {
		t.Fatal("expected clone exec not to run")
	}
	if !strings.Contains(w.Body.String(), "base_commit must be a valid git revision") {
		t.Fatalf("expected base_commit validation error, got %s", w.Body.String())
	}
}

func TestGitCloneEndpointChecksOutBaseCommitWhenProvided(t *testing.T) {
	var capturedCmds [][]string
	hijacked := fakeHijackedResponse([]byte(""), []byte(""))
	m := &mockDocker{
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			capturedCmds = append(capturedCmds, append([]string{}, options.Cmd...))
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-%d", len(capturedCmds))}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return hijacked, nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodPost, "/api/git/clone", bytes.NewBufferString(`{"container_id":"cid","repo_url":"https://github.com/example/repo.git","target_path":"/workspace/repo","base_commit":"abc123"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if len(capturedCmds) != 2 {
		t.Fatalf("expected clone and checkout commands, got %d", len(capturedCmds))
	}
	if got := capturedCmds[0]; len(got) != 4 || got[0] != "git" || got[1] != "clone" || got[2] != "https://github.com/example/repo.git" || got[3] != "/workspace/repo" {
		t.Fatalf("expected clone command, got %v", got)
	}
	if got := capturedCmds[1]; len(got) < 3 || got[0] != "sh" || got[1] != "-lc" || !strings.Contains(got[2], "git -C '/workspace/repo' fetch --no-tags origin 'abc123'") || !strings.Contains(got[2], "git -C '/workspace/repo' checkout --detach 'abc123'") {
		t.Fatalf("expected checkout command, got %v", got)
	}
}

func TestCreateSandboxEndpointMaterializesBaseCommitForShallowSingleBranchClone(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	var capturedCmds [][]string
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image inspect for ubuntu:24.04, got %q", imageID)
			}
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			capturedCmds = append(capturedCmds, append([]string{}, options.Cmd...))
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-%d", len(capturedCmds))}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("")), nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error { return nil }}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git","branch":"main","single_branch":true,"depth":1,"base_commit":"abc123"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if len(capturedCmds) != 2 {
		t.Fatalf("expected clone and checkout commands, got %d", len(capturedCmds))
	}
	if got := capturedCmds[0]; len(got) < 8 || got[0] != "git" || got[1] != "clone" || got[2] != "--branch" || got[3] != "main" || got[4] != "--single-branch" || got[5] != "--depth" || got[6] != "1" {
		t.Fatalf("expected shallow single-branch clone command, got %v", got)
	}
	if got := capturedCmds[1]; len(got) < 3 || got[0] != "sh" || got[1] != "-lc" || !strings.Contains(got[2], "git -C '/workspace/repo' fetch --no-tags origin 'abc123'") || !strings.Contains(got[2], "git -C '/workspace/repo' checkout --detach 'abc123'") {
		t.Fatalf("expected base commit materialization before checkout, got %v", got)
	}
}

func TestCreateSandboxEndpointBuildsCloneCommandWithDepthAndFilter(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	var capturedCmd []string
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			if imageID != "ubuntu:24.04" {
				t.Fatalf("expected image inspect for ubuntu:24.04, got %q", imageID)
			}
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, config *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			if config.Labels["open-sandbox.repo_single_branch"] != "true" {
				t.Fatalf("expected repo single branch label, got %+v", config.Labels)
			}
			if config.Labels["open-sandbox.repo_depth"] != "1" {
				t.Fatalf("expected repo depth label, got %+v", config.Labels)
			}
			if config.Labels["open-sandbox.repo_filter"] != "blob:none" {
				t.Fatalf("expected repo filter label, got %+v", config.Labels)
			}
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error {
			return nil
		},
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			capturedCmd = append([]string{}, options.Cmd...)
			return container.ExecCreateResponse{ID: "exec-clone"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("")), nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		createSandboxFn: func(context.Context, store.Sandbox) error { return nil },
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git","branch":"main","single_branch":true,"depth":1,"filter":"blob:none"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	want := []string{"git", "clone", "--branch", "main", "--single-branch", "--depth", "1", "--filter", "blob:none", "--reference-if-able", path.Join(sandboxGitCacheMountRoot, sandboxGitCacheKey("https://github.com/example/repo.git")+".git"), "https://github.com/example/repo.git", "/workspace/repo"}
	if len(capturedCmd) != len(want) {
		t.Fatalf("unexpected command length: got %v", capturedCmd)
	}
	for i := range want {
		if capturedCmd[i] != want[i] {
			t.Fatalf("unexpected command at %d: got %q, want %q", i, capturedCmd[i], want[i])
		}
	}
}

func TestCreateSandboxEndpointRejectsNonPositiveDepth(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth int
	}{
		{name: "zero", depth: 0},
		{name: "negative", depth: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			containerCreated := false
			m := &mockDocker{
				containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					containerCreated = true
					return container.CreateResponse{}, nil
				},
			}

			s := newTestServerWithStore(m, &mockSandboxStore{})
			req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(fmt.Sprintf(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git","depth":%d}`, tc.depth)))
			setAuthHeader(t, req)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			s.Router().ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
			}
			if containerCreated {
				t.Fatal("expected sandbox container not to be created")
			}
			if !strings.Contains(w.Body.String(), "depth must be a positive integer") {
				t.Fatalf("expected depth validation error, got %s", w.Body.String())
			}
		})
	}
}

func TestCreateSandboxEndpointRejectsInvalidBaseCommit(t *testing.T) {
	containerCreated := false
	m := &mockDocker{
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			containerCreated = true
			return container.CreateResponse{}, nil
		},
	}

	s := newTestServerWithStore(m, &mockSandboxStore{})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git","base_commit":"abc123; touch /tmp/pwned"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if containerCreated {
		t.Fatal("expected sandbox container not to be created")
	}
	if !strings.Contains(w.Body.String(), "base_commit must be a valid git revision") {
		t.Fatalf("expected base_commit validation error, got %s", w.Body.String())
	}
}

func TestResetSandboxEndpointBuildsCloneCommandWithDepthAndFilter(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	var capturedCmds [][]string
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, containerID string) (container.InspectResponse, error) {
			if containerID != "sandbox-container" {
				t.Fatalf("expected sandbox container, got %q", containerID)
			}
			return container.InspectResponse{
				Config: &container.Config{Labels: map[string]string{
					"open-sandbox.repo_url":           "https://github.com/example/repo.git",
					"open-sandbox.repo_branch":        "main",
					"open-sandbox.repo_single_branch": "true",
					"open-sandbox.repo_depth":         "1",
					"open-sandbox.repo_filter":        "blob:none",
					"open-sandbox.repo_target_path":   "/workspace/repo",
				}},
				ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
			}, nil
		},
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			captured := append([]string{}, options.Cmd...)
			capturedCmds = append(capturedCmds, captured)
			return container.ExecCreateResponse{ID: "exec-reset"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("")), nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if len(capturedCmds) != 4 {
		t.Fatalf("expected repo check, fetch, reset, and clean commands, got %d", len(capturedCmds))
	}
	if got := capturedCmds[0]; len(got) < 3 || got[0] != "sh" || got[1] != "-lc" || !strings.Contains(got[2], "test -d '/workspace/repo'/.git") {
		t.Fatalf("expected repo existence check, got %v", got)
	}
	if got := capturedCmds[1]; len(got) < 3 || !strings.Contains(got[2], "git -C '/workspace/repo' fetch --prune --depth 1 --filter='blob:none' origin 'main'") {
		t.Fatalf("expected fetch command, got %v", got)
	}
	if got := capturedCmds[2]; len(got) < 3 || !strings.Contains(got[2], "git -C '/workspace/repo' checkout -B 'main' origin/'main' && git -C '/workspace/repo' reset --hard origin/'main'") {
		t.Fatalf("expected reset command, got %v", got)
	}
	if got := capturedCmds[3]; len(got) != 5 || got[0] != "git" || got[1] != "-C" || got[2] != "/workspace/repo" || got[3] != "clean" || got[4] != "-fdx" {
		t.Fatalf("expected clean command, got %v", got)
	}
}

func TestResetSandboxEndpointUsesBaseCommitWhenStored(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	var capturedCmds [][]string
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, containerID string) (container.InspectResponse, error) {
			if containerID != "sandbox-container" {
				t.Fatalf("expected sandbox container, got %q", containerID)
			}
			return container.InspectResponse{
				Config: &container.Config{Labels: map[string]string{
					"open-sandbox.repo_url":           "https://github.com/example/repo.git",
					"open-sandbox.repo_branch":        "main",
					"open-sandbox.repo_single_branch": "true",
					"open-sandbox.repo_depth":         "1",
					"open-sandbox.repo_base_commit":   "abc123",
					"open-sandbox.repo_target_path":   "/workspace/repo",
				}},
				ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
			}, nil
		},
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			captured := append([]string{}, options.Cmd...)
			capturedCmds = append(capturedCmds, captured)
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-reset-%d", len(capturedCmds))}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("")), nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if len(capturedCmds) != 4 {
		t.Fatalf("expected repo check, fetch, reset, and clean commands, got %d", len(capturedCmds))
	}
	if got := capturedCmds[2]; len(got) < 3 || !strings.Contains(got[2], "git -C '/workspace/repo' fetch --no-tags origin 'abc123'") || !strings.Contains(got[2], "git -C '/workspace/repo' checkout --detach 'abc123' && git -C '/workspace/repo' reset --hard 'abc123'") {
		t.Fatalf("expected base-commit reset command, got %v", got)
	}
}

func TestResetSandboxEndpointRejectsInvalidStoredBaseCommit(t *testing.T) {
	execCalled := false
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{
				Config: &container.Config{Labels: map[string]string{
					"open-sandbox.repo_url":         "https://github.com/example/repo.git",
					"open-sandbox.repo_target_path": "/workspace/repo",
					"open-sandbox.repo_base_commit": "abc123; touch /tmp/pwned",
				}},
				ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
			}, nil
		},
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			execCalled = true
			return container.ExecCreateResponse{}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", w.Code, w.Body.String())
	}
	if execCalled {
		t.Fatal("expected reset exec not to run")
	}
	if !strings.Contains(w.Body.String(), "invalid stored base_commit") {
		t.Fatalf("expected stored base_commit validation error, got %s", w.Body.String())
	}
}

func TestResetSandboxEndpointRecloneFallbackMaterializesBaseCommit(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	var capturedCmds [][]string
	execCount := 0
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{
				Config: &container.Config{Labels: map[string]string{
					"open-sandbox.repo_url":           "https://github.com/example/repo.git",
					"open-sandbox.repo_branch":        "main",
					"open-sandbox.repo_single_branch": "true",
					"open-sandbox.repo_depth":         "1",
					"open-sandbox.repo_base_commit":   "abc123",
					"open-sandbox.repo_target_path":   "/workspace/repo",
				}},
				ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
			}, nil
		},
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			execCount++
			capturedCmds = append(capturedCmds, append([]string{}, options.Cmd...))
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-reset-%d", execCount)}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("")), nil
		},
		containerExecInspectFn: func(_ context.Context, execID string) (container.ExecInspect, error) {
			if execID == "exec-reset-2" {
				return container.ExecInspect{ExitCode: 1, Running: false}, nil
			}
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	cloneSeen := false
	checkoutSeen := false
	swapSeen := false
	for _, got := range capturedCmds {
		if len(got) < 3 {
			continue
		}
		if got[0] == "git" && got[1] == "clone" {
			joined := strings.Join(got, " ")
			if !strings.Contains(joined, "--single-branch") || !strings.Contains(joined, "--depth 1") {
				t.Fatalf("expected shallow single-branch clone in fallback, got %v", got)
			}
			if got[len(got)-1] != "/workspace/repo.reclone" {
				t.Fatalf("expected fallback clone into temp path, got %v", got)
			}
			cloneSeen = true
		}
		if got[0] == "sh" && got[1] == "-lc" && strings.Contains(got[2], "checkout --detach 'abc123'") {
			if !strings.Contains(got[2], "git -C '/workspace/repo.reclone' fetch --no-tags origin 'abc123'") {
				t.Fatalf("expected base commit materialization in fallback checkout, got %v", got)
			}
			checkoutSeen = true
		}
		if got[0] == "sh" && got[1] == "-lc" && strings.Contains(got[2], "tmp='/workspace/repo.reclone'") {
			if !strings.Contains(got[2], "mv \"$tmp\" \"$target\"") {
				t.Fatalf("expected safe swap command in fallback, got %v", got)
			}
			swapSeen = true
		}
	}
	if !cloneSeen {
		t.Fatalf("expected fallback clone command, got %v", capturedCmds)
	}
	if !checkoutSeen {
		t.Fatalf("expected fallback checkout command, got %v", capturedCmds)
	}
	if !swapSeen {
		t.Fatalf("expected fallback swap command, got %v", capturedCmds)
	}
}

func TestResetSandboxEndpointCleansMissingRepoTargetBeforeClone(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	var capturedCmds [][]string
	execCount := 0
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{
				Config: &container.Config{Labels: map[string]string{
					"open-sandbox.repo_url":         "https://github.com/example/repo.git",
					"open-sandbox.repo_target_path": "/workspace/repo",
				}},
				ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}},
			}, nil
		},
		containerExecCreateFn: func(_ context.Context, _ string, options container.ExecOptions) (container.ExecCreateResponse, error) {
			execCount++
			capturedCmds = append(capturedCmds, append([]string{}, options.Cmd...))
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-reset-%d", execCount)}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("")), nil
		},
		containerExecInspectFn: func(_ context.Context, execID string) (container.ExecInspect, error) {
			if execID == "exec-reset-1" {
				return container.ExecInspect{ExitCode: 1, Running: false}, nil
			}
			return container.ExecInspect{ExitCode: 0, Running: false}, nil
		},
	}

	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}

	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	cleanupSeen := false
	cloneSeen := false
	for _, got := range capturedCmds {
		joined := strings.Join(got, " ")
		if strings.Contains(joined, "test -d '/workspace/repo'/.git") {
			continue
		}
		if strings.Contains(joined, "rm -rf '/workspace/repo' && mkdir -p '/workspace'") {
			cleanupSeen = true
		}
		if strings.Contains(joined, "git clone") {
			cloneSeen = true
		}
	}
	if !cleanupSeen {
		t.Fatalf("expected cleanup command before clone, got %v", capturedCmds)
	}
	if !cloneSeen {
		t.Fatalf("expected clone command, got %v", capturedCmds)
	}
}

func TestLogStreamEndpoint(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `{"ID":"c-2","Image":"ubuntu:24.04","Names":"sandbox-two","Status":"Up 5 minutes","Labels":"open-sandbox.managed=true,open-sandbox.owner_id=admin-user"}` + "\n", "", nil
	}

	stream := fakeMuxedStream([]byte("hello\n"), []byte("warn\n"))
	m := &mockDocker{
		containerInspectFn: func(context.Context, string) (container.InspectResponse, error) {
			return container.InspectResponse{Config: &container.Config{Tty: false}}, nil
		},
		containerLogsFn: func(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(stream)), nil
		},
	}

	s := newTestServer(m)
	req := httptest.NewRequest(http.MethodGet, "/api/containers/c-2/logs?follow=false&tail=10", nil)
	setAuthHeader(t, req)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("event: stdout")) || !bytes.Contains([]byte(body), []byte("event: stderr")) {
		t.Fatalf("expected stdout/stderr events in stream: %s", body)
	}
}

func TestMetricsEndpointReportsLifecycleCounters(t *testing.T) {
	s := newTestServer(&mockDocker{})
	s.metrics.recordLifecycle("create_sandbox", "success")
	s.metrics.recordCleanupRun("success")
	s.metrics.recordSandboxRepoPhase("reset", "fetch", "success", 250*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `open_sandbox_sandbox_lifecycle_total{operation="create_sandbox",result="success"} 1`) {
		t.Fatalf("expected lifecycle metric, got %s", body)
	}
	if !strings.Contains(body, `open_sandbox_cleanup_runs_total{result="success"} 1`) {
		t.Fatalf("expected cleanup metric, got %s", body)
	}
	if !strings.Contains(body, `open_sandbox_sandbox_repo_phase_total{operation="reset",phase="fetch",result="success"} 1`) {
		t.Fatalf("expected repo phase counter, got %s", body)
	}
	if !strings.Contains(body, `open_sandbox_sandbox_repo_phase_duration_seconds{operation="reset",phase="fetch",result="success"} 0.25`) {
		t.Fatalf("expected repo phase duration, got %s", body)
	}
}

func TestCreateSandboxStreamEndpointEmitsProgressEvents(t *testing.T) {
	m := &mockDocker{
		imageInspectFn: func(_ context.Context, imageID string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error { return nil },
	}
	s := newTestServerWithStore(m, &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error { return nil }})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/stream", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: progress") || !strings.Contains(body, "event: result") || !strings.Contains(body, "event: done") {
		t.Fatalf("expected progress, result, and done events, got %s", body)
	}
}

func TestCreateSandboxStreamEndpointEmitsErrorEvent(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	m := &mockDocker{
		imageInspectFn: func(_ context.Context, _ string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error { return nil },
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{ID: "exec-clone"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("fatal: no such repo\n")), nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 1, Running: false}, nil
		},
	}
	s := newTestServerWithStore(m, &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error { return nil }})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/stream", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: error") || !strings.Contains(body, `data: {"error":"clone repository failed","reason":"git_clone_failed","stderr":"fatal: no such repo","status":400}`) {
		t.Fatalf("expected structured error event, got %s", body)
	}
	if strings.Contains(body, "event: result") {
		t.Fatalf("did not expect result event on failure, got %s", body)
	}
}

func TestCreateSandboxEndpointReturnsJSONErrorOnCloneFailure(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	removedContainer := ""
	removeOptions := container.RemoveOptions{}
	removedVolume := ""
	removedVolumeForce := false

	m := &mockDocker{
		imageInspectFn: func(_ context.Context, _ string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error { return nil },
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			return container.ExecCreateResponse{ID: "exec-clone"}, nil
		},
		containerExecAttachFn: func(context.Context, string, container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			return fakeHijackedResponse([]byte(""), []byte("fatal: no such repo\n")), nil
		},
		containerExecInspectFn: func(context.Context, string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 1, Running: false}, nil
		},
		containerRemoveFn: func(_ context.Context, containerID string, options container.RemoveOptions) error {
			removedContainer = containerID
			removeOptions = options
			return nil
		},
		volumeRemoveFn: func(_ context.Context, volumeID string, force bool) error {
			removedVolume = volumeID
			removedVolumeForce = force
			return nil
		},
	}
	s := newTestServerWithStore(m, &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error { return nil }})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if removedContainer != "sandbox-container-id" {
		t.Fatalf("expected rollback remove for sandbox-container-id, got %q", removedContainer)
	}
	if !removeOptions.Force || !removeOptions.RemoveVolumes {
		t.Fatalf("expected rollback remove options force+volumes, got %+v", removeOptions)
	}
	if removedVolume == "" {
		t.Fatal("expected workspace volume rollback to run")
	}
	if !removedVolumeForce {
		t.Fatal("expected workspace volume rollback to force removal")
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"error":"clone repository failed","reason":"git_clone_failed","stderr":"fatal: no such repo"}` {
		t.Fatalf("expected structured JSON error, got %s", got)
	}
}

func TestCreateSandboxEndpointRollsBackOnBaseCommitCheckoutFailure(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	execCount := 0
	removedContainer := ""
	removeOptions := container.RemoveOptions{}
	removedVolume := ""
	removedVolumeForce := false
	createdVolume := ""

	m := &mockDocker{
		imageInspectFn: func(_ context.Context, _ string) (image.InspectResponse, error) {
			return image.InspectResponse{Config: &dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{WorkingDir: "/workspace"}}}, nil
		},
		volumeCreateFn: func(_ context.Context, options volume.CreateOptions) (volume.Volume, error) {
			createdVolume = options.Name
			return volume.Volume{Name: options.Name}, nil
		},
		containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "sandbox-container-id"}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error { return nil },
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			execCount++
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-%d", execCount)}, nil
		},
		containerExecAttachFn: func(_ context.Context, execID string, _ container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			if execID == "exec-1" {
				return fakeHijackedResponse([]byte(""), []byte("")), nil
			}
			return fakeHijackedResponse([]byte(""), []byte("fatal: bad revision\n")), nil
		},
		containerExecInspectFn: func(_ context.Context, execID string) (container.ExecInspect, error) {
			if execID == "exec-1" {
				return container.ExecInspect{ExitCode: 0, Running: false}, nil
			}
			return container.ExecInspect{ExitCode: 1, Running: false}, nil
		},
		containerRemoveFn: func(_ context.Context, containerID string, options container.RemoveOptions) error {
			removedContainer = containerID
			removeOptions = options
			return nil
		},
		volumeRemoveFn: func(_ context.Context, volumeID string, force bool) error {
			removedVolume = volumeID
			removedVolumeForce = force
			return nil
		},
	}

	s := newTestServerWithStore(m, &mockSandboxStore{createSandboxFn: func(context.Context, store.Sandbox) error { return nil }})
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04","repo_url":"https://github.com/example/repo.git","base_commit":"abc123"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if removedContainer != "sandbox-container-id" {
		t.Fatalf("expected rollback remove for sandbox-container-id, got %q", removedContainer)
	}
	if !removeOptions.Force || !removeOptions.RemoveVolumes {
		t.Fatalf("expected rollback remove options force+volumes, got %+v", removeOptions)
	}
	if removedVolume != createdVolume {
		t.Fatalf("expected rollback remove for workspace volume %q, got %q", createdVolume, removedVolume)
	}
	if !removedVolumeForce {
		t.Fatal("expected workspace volume rollback to force removal")
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"error":"checkout repository failed","reason":"git_checkout_failed","stderr":"fatal: bad revision"}` {
		t.Fatalf("expected checkout failure payload, got %s", got)
	}
}

func TestResetSandboxStreamEndpointEmitsErrorEvent(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	callCount := 0
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{Config: &container.Config{Labels: map[string]string{
				"open-sandbox.repo_url":         "https://github.com/example/repo.git",
				"open-sandbox.repo_target_path": "/workspace/repo",
			}}, ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}}}, nil
		},
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			callCount++
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-%d", callCount)}, nil
		},
		containerExecAttachFn: func(_ context.Context, execID string, _ container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			if execID == "exec-1" {
				return fakeHijackedResponse([]byte(""), []byte("fatal: corrupt repo\n")), nil
			}
			return fakeHijackedResponse([]byte(""), []byte("permission denied\n")), nil
		},
		containerExecInspectFn: func(_ context.Context, execID string) (container.ExecInspect, error) {
			if execID == "exec-1" {
				return container.ExecInspect{ExitCode: 1, Running: false}, nil
			}
			return container.ExecInspect{ExitCode: 1, Running: false}, nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}
	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset/stream", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: error") || !strings.Contains(body, `data: {"error":"reset repository failed","reason":"git_cleanup_failed","stderr":"permission denied","status":500}`) {
		t.Fatalf("expected structured error event, got %s", body)
	}
}

func TestResetSandboxEndpointReturnsJSONErrorOnRefreshFailure(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	callCount := 0
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{Config: &container.Config{Labels: map[string]string{
				"open-sandbox.repo_url":         "https://github.com/example/repo.git",
				"open-sandbox.repo_target_path": "/workspace/repo",
			}}, ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true}}}, nil
		},
		containerExecCreateFn: func(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error) {
			callCount++
			return container.ExecCreateResponse{ID: fmt.Sprintf("exec-%d", callCount)}, nil
		},
		containerExecAttachFn: func(_ context.Context, execID string, _ container.ExecAttachOptions) (dockertypes.HijackedResponse, error) {
			if execID == "exec-1" {
				return fakeHijackedResponse([]byte(""), []byte("fatal: corrupt repo\n")), nil
			}
			return fakeHijackedResponse([]byte(""), []byte("permission denied\n")), nil
		},
		containerExecInspectFn: func(_ context.Context, _ string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 1, Running: false}, nil
		},
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}
	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (%s)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"error":"reset repository failed","reason":"git_cleanup_failed","stderr":"permission denied"}` {
		t.Fatalf("expected structured JSON error, got %s", got)
	}
}

func TestPrepareSandboxGitReferenceLocksPerRepo(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()

	workspaceRoot := t.TempDir()
	s := newTestServerWithStore(&mockDocker{}, &mockSandboxStore{})
	s.workspaceRoot = workspaceRoot

	cacheDir := filepath.Join(s.sandboxGitCacheRoot(), sandboxGitCacheKey("https://github.com/example/repo.git")+".git")
	cloneStarted := make(chan struct{}, 1)
	cloneRelease := make(chan struct{})
	var mu sync.Mutex
	cloneCount := 0
	fetchCount := 0
	commandRunner = func(_ context.Context, name string, args ...string) (string, string, error) {
		mu.Lock()
		if name != "git" {
			mu.Unlock()
			return "", "", nil
		}
		if len(args) >= 1 && args[0] == "clone" {
			cloneCount++
			if err := os.MkdirAll(cacheDir, 0o700); err != nil {
				mu.Unlock()
				return "", err.Error(), err
			}
			cloneStarted <- struct{}{}
			mu.Unlock()
			<-cloneRelease
			return "", "", nil
		}
		if len(args) >= 3 && args[0] == "-C" && args[2] == "fetch" {
			fetchCount++
		}
		mu.Unlock()
		return "", "", nil
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.prepareSandboxGitReference(context.Background(), localRuntimeWorkerID, "https://github.com/example/repo.git", sandboxProgressReporter{})
	}()
	<-cloneStarted
	go func() {
		defer wg.Done()
		s.prepareSandboxGitReference(context.Background(), localRuntimeWorkerID, "https://github.com/example/repo.git", sandboxProgressReporter{})
	}()
	close(cloneRelease)
	wg.Wait()

	if cloneCount != 1 {
		t.Fatalf("expected one mirror clone, got %d", cloneCount)
	}
	if fetchCount != 1 {
		t.Fatalf("expected one mirror fetch after clone, got %d", fetchCount)
	}
}

func TestPrepareSandboxGitReferenceSkipsUnsafeRepoURLs(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()
	called := false
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		called = true
		return "", "", nil
	}

	s := newTestServerWithStore(&mockDocker{}, &mockSandboxStore{})
	if referencePath, bind := s.prepareSandboxGitReference(context.Background(), localRuntimeWorkerID, "git@localhost:example/repo.git", sandboxProgressReporter{}); referencePath != "" || bind != "" {
		t.Fatalf("expected unsafe remote to skip shared cache, got %q %q", referencePath, bind)
	}
	if called {
		t.Fatal("expected unsafe remote to avoid host-side git commands")
	}
}

func TestResetSandboxStreamEndpointEmitsProgressEvents(t *testing.T) {
	m := &mockDocker{
		containerInspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
			return container.InspectResponse{Config: &container.Config{Labels: map[string]string{}}, ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: false}}}, nil
		},
		containerStartFn: func(_ context.Context, _ string, _ container.StartOptions) error { return nil },
	}
	sandboxStore := &mockSandboxStore{
		getSandboxFn: func(context.Context, string) (store.Sandbox, error) {
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", OwnerID: "admin-user", OwnerUsername: "admin"}, nil
		},
		updateSandboxStatusFn: func(context.Context, string, string) error { return nil },
	}
	s := newTestServerWithStore(m, sandboxStore)
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes/sandbox-1/reset/stream", bytes.NewBufferString(`{}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: progress") || !strings.Contains(body, "event: result") || !strings.Contains(body, "event: done") {
		t.Fatalf("expected progress, result, and done events, got %s", body)
	}
}

func TestMaintenanceCleanupRemovesStaleArtifacts(t *testing.T) {
	workspaceRoot := t.TempDir()
	composeDir := filepath.Join(workspaceRoot, ".open-sandbox", "compose", "stale-project")
	if err := os.MkdirAll(composeDir, 0o700); err != nil {
		t.Fatalf("create compose dir: %v", err)
	}
	staleSpecDir := filepath.Join(workspaceRoot, ".open-sandbox", "containers")
	if err := os.MkdirAll(staleSpecDir, 0o700); err != nil {
		t.Fatalf("create direct spec dir: %v", err)
	}
	staleSpecPath := filepath.Join(staleSpecDir, "ctr-stale.json")
	if err := os.WriteFile(staleSpecPath, []byte(`{"image":"alpine:3.20"}`), 0o600); err != nil {
		t.Fatalf("write direct spec: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(composeDir, oldTime, oldTime); err != nil {
		t.Fatalf("touch compose dir: %v", err)
	}
	if err := os.Chtimes(staleSpecPath, oldTime, oldTime); err != nil {
		t.Fatalf("touch direct spec: %v", err)
	}

	deletedSandboxID := ""
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{ID: "missing-sandbox", ContainerID: "gone-container", UpdatedAt: time.Now().Add(-48 * time.Hour).Unix()}}, nil
		},
		deleteSandboxFn: func(_ context.Context, sandboxID string) error {
			deletedSandboxID = sandboxID
			return nil
		},
	}

	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	s := newTestServerWithStore(&mockDocker{containerListFn: func(context.Context, container.ListOptions) ([]container.Summary, error) {
		return nil, nil
	}}, sandboxStore)
	s.workspaceRoot = workspaceRoot

	req := httptest.NewRequest(http.MethodPost, "/api/admin/maintenance/cleanup", bytes.NewBufferString(`{"dry_run":false,"max_artifact_age":"24h","max_missing_sandbox_age":"24h"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if deletedSandboxID != "missing-sandbox" {
		t.Fatalf("expected missing sandbox record to be deleted, got %q", deletedSandboxID)
	}
	if _, err := os.Stat(staleSpecPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale direct spec to be removed, got err=%v", err)
	}
	if _, err := os.Stat(composeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale compose dir to be removed, got err=%v", err)
	}
	if !strings.Contains(w.Body.String(), `"direct_container_specs":1`) || !strings.Contains(w.Body.String(), `"compose_projects":1`) {
		t.Fatalf("expected cleanup response counts, got %s", w.Body.String())
	}
}

func TestMaintenanceReconcileRemovesMissingArtifactsImmediately(t *testing.T) {
	workspaceRoot := t.TempDir()
	composeDir := filepath.Join(workspaceRoot, ".open-sandbox", "compose", "missing-project")
	if err := os.MkdirAll(composeDir, 0o700); err != nil {
		t.Fatalf("create compose dir: %v", err)
	}
	specDir := filepath.Join(workspaceRoot, ".open-sandbox", "containers")
	if err := os.MkdirAll(specDir, 0o700); err != nil {
		t.Fatalf("create direct spec dir: %v", err)
	}
	specPath := filepath.Join(specDir, "ctr-missing.json")
	if err := os.WriteFile(specPath, []byte(`{"image":"alpine:3.20"}`), 0o600); err != nil {
		t.Fatalf("write direct spec: %v", err)
	}

	deletedSandboxID := ""
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{ID: "sandbox-missing", ContainerID: "gone-container", UpdatedAt: time.Now().Unix()}}, nil
		},
		deleteSandboxFn: func(_ context.Context, sandboxID string) error {
			deletedSandboxID = sandboxID
			return nil
		},
	}

	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	s := newTestServerWithStore(&mockDocker{containerListFn: func(context.Context, container.ListOptions) ([]container.Summary, error) {
		return nil, nil
	}}, sandboxStore)
	s.workspaceRoot = workspaceRoot

	req := httptest.NewRequest(http.MethodPost, "/api/admin/maintenance/reconcile", bytes.NewBufferString(`{"dry_run":false}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if deletedSandboxID != "sandbox-missing" {
		t.Fatalf("expected missing sandbox record to be deleted, got %q", deletedSandboxID)
	}
	if _, err := os.Stat(specPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected direct spec to be removed, got err=%v", err)
	}
	if _, err := os.Stat(composeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected compose project dir to be removed, got err=%v", err)
	}
}

func TestMaintenanceReconcileDryRunDoesNotRemoveArtifacts(t *testing.T) {
	workspaceRoot := t.TempDir()
	composeDir := filepath.Join(workspaceRoot, ".open-sandbox", "compose", "dryrun-project")
	if err := os.MkdirAll(composeDir, 0o700); err != nil {
		t.Fatalf("create compose dir: %v", err)
	}
	specDir := filepath.Join(workspaceRoot, ".open-sandbox", "containers")
	if err := os.MkdirAll(specDir, 0o700); err != nil {
		t.Fatalf("create direct spec dir: %v", err)
	}
	specPath := filepath.Join(specDir, "ctr-dryrun.json")
	if err := os.WriteFile(specPath, []byte(`{"image":"alpine:3.20"}`), 0o600); err != nil {
		t.Fatalf("write direct spec: %v", err)
	}

	deleteCalls := 0
	sandboxStore := &mockSandboxStore{
		listSandboxesFn: func(context.Context) ([]store.Sandbox, error) {
			return []store.Sandbox{{ID: "sandbox-dryrun", ContainerID: "gone-container", UpdatedAt: time.Now().Unix()}}, nil
		},
		deleteSandboxFn: func(_ context.Context, _ string) error {
			deleteCalls++
			return nil
		},
	}

	original := commandRunner
	defer func() { commandRunner = original }()
	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return "", "", nil
	}

	s := newTestServerWithStore(&mockDocker{containerListFn: func(context.Context, container.ListOptions) ([]container.Summary, error) {
		return nil, nil
	}}, sandboxStore)
	s.workspaceRoot = workspaceRoot

	req := httptest.NewRequest(http.MethodPost, "/api/admin/maintenance/reconcile", bytes.NewBufferString(`{"dry_run":true}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if deleteCalls != 0 {
		t.Fatalf("expected dry-run to avoid deleting sandbox records, calls=%d", deleteCalls)
	}
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("expected direct spec to remain during dry-run, err=%v", err)
	}
	if _, err := os.Stat(composeDir); err != nil {
		t.Fatalf("expected compose project dir to remain during dry-run, err=%v", err)
	}
}

func untarTextFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()

	files := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar: %v", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("failed to read file %s from tar: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(content)
	}

	return files
}

func fakeMuxedStream(stdout []byte, stderr []byte) []byte {
	var b bytes.Buffer
	if len(stdout) > 0 {
		_, _ = stdcopy.NewStdWriter(&b, stdcopy.Stdout).Write(stdout)
	}
	if len(stderr) > 0 {
		_, _ = stdcopy.NewStdWriter(&b, stdcopy.Stderr).Write(stderr)
	}
	return b.Bytes()
}

func fakeHijackedResponse(stdout []byte, stderr []byte) dockertypes.HijackedResponse {
	stream := fakeMuxedStream(stdout, stderr)
	conn, peer := net.Pipe()
	_ = peer.Close()

	return dockertypes.HijackedResponse{
		Conn:   conn,
		Reader: bufio.NewReader(bytes.NewReader(stream)),
	}
}
