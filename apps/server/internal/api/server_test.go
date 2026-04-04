package api

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

type mockDocker struct {
	imageBuildFn           func(context.Context, io.Reader, build.ImageBuildOptions) (build.ImageBuildResponse, error)
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
}

type mockSandboxStore struct {
	createSandboxFn                  func(context.Context, store.Sandbox) error
	listSandboxesFn                  func(context.Context) ([]store.Sandbox, error)
	getSandboxFn                     func(context.Context, string) (store.Sandbox, error)
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

func (m *mockSandboxStore) UpdateSandboxStatus(ctx context.Context, sandboxID string, status string) error {
	if m.updateSandboxStatusFn == nil {
		return errors.New("not implemented")
	}
	return m.updateSandboxStatusFn(ctx, sandboxID, status)
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

func TestHealthEndpointIsPublic(t *testing.T) {
	s := newTestServer(&mockDocker{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
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
	original := commandRunner
	defer func() { commandRunner = original }()

	commandRunner = func(context.Context, string, ...string) (string, string, error) {
		return `[{"Name":"app","State":"running"}]`, "", nil
	}

	s := newTestServer(&mockDocker{})
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

func TestComposeDownIncludesOptionalFlags(t *testing.T) {
	original := commandRunner
	defer func() { commandRunner = original }()

	var capturedArgs []string
	commandRunner = func(_ context.Context, _ string, args ...string) (string, string, error) {
		capturedArgs = append([]string(nil), args...)
		return "ok", "", nil
	}

	s := newTestServer(&mockDocker{})
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
		return `{"ID":"cid-123","Image":"ubuntu:24.04","Names":"sandbox-one","Ports":"0.0.0.0:3000->3000/tcp","Status":"Up 5 minutes","Labels":"open-sandbox.managed=true"}` + "\n", "", nil
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

func TestCreateSandboxEndpoint(t *testing.T) {
	createdSandbox := store.Sandbox{}
	m := &mockDocker{
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
			if len(hostConfig.Binds) != 1 || !strings.Contains(hostConfig.Binds[0], ":/workspace") {
				t.Fatalf("expected workspace bind mount, got %v", hostConfig.Binds)
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
	req := httptest.NewRequest(http.MethodPost, "/api/sandboxes", bytes.NewBufferString(`{"name":"workspace","image":"ubuntu:24.04"}`))
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
	if createdSandbox.OwnerID != "admin-user" || createdSandbox.OwnerUsername != "admin" {
		t.Fatalf("expected sandbox ownership to be set, got %+v", createdSandbox)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("sandbox-container-id")) {
		t.Fatalf("expected container id in response: %s", w.Body.String())
	}
}

func TestCreateSandboxEndpointUsesImageDefaultCommandWhenRequested(t *testing.T) {
	m := &mockDocker{
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
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container"}, nil
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
	go func() {
		_, _ = peerConn.Write([]byte("terminal ready\r\n"))
		buffer := make([]byte, 64)
		_ = peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		count, err := peerConn.Read(buffer)
		if err == nil && count > 0 {
			receivedInput <- string(buffer[:count])
		}
		_ = peerConn.Close()
	}()

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
			return store.Sandbox{ID: "sandbox-1", ContainerID: "sandbox-container", WorkspaceDir: "/workspace"}, nil
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
	req := httptest.NewRequest(http.MethodPost, "/api/git/clone", bytes.NewBufferString(`{"container_id":"cid","repo_url":"https://github.com/example/repo.git","target_path":"/workspace/repo","branch":"main"}`))
	setAuthHeader(t, req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	want := []string{"git", "clone", "--branch", "main", "https://github.com/example/repo.git", "/workspace/repo"}
	if len(capturedCmd) != len(want) {
		t.Fatalf("unexpected command length: got %v", capturedCmd)
	}
	for i := range want {
		if capturedCmd[i] != want[i] {
			t.Fatalf("unexpected command at %d: got %q, want %q", i, capturedCmd[i], want[i])
		}
	}
}

func TestLogStreamEndpoint(t *testing.T) {
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
