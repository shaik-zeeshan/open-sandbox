package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestComposeSecretsKeyPassesThroughOnlyWhenSet(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("docker compose is not available")
	}

	repoRoot := repoRoot(t)
	for _, composeFile := range []string{"compose.yaml", "compose.dev.yaml", "compose.ghcr.yaml"} {
		t.Run(composeFile, func(t *testing.T) {
			environment := loadServerEnvironment(t, filepath.Join(repoRoot, composeFile))

			tmpDir := t.TempDir()
			composePath := filepath.Join(tmpDir, "compose.yaml")
			writeYAML(t, composePath, map[string]any{
				"services": map[string]any{
					"server": map[string]any{
						"image":       "alpine:3.22",
						"command":     []string{"sh", "-lc", "env | grep '^SANDBOX_SECRETS_KEY=' || true"},
						"environment": environment,
					},
				},
			})

			unsetOutput := runComposeServerEnv(t, composePath, map[string]string{
				"SANDBOX_JWT_SECRET":    "test-jwt-secret",
				"OPEN_SANDBOX_DATA_DIR": "/tmp/open-sandbox",
			})
			if strings.Contains(unsetOutput, "SANDBOX_SECRETS_KEY=") {
				t.Fatalf("expected SANDBOX_SECRETS_KEY to be omitted when unset, got output %q", unsetOutput)
			}

			setOutput := runComposeServerEnv(t, composePath, map[string]string{
				"SANDBOX_JWT_SECRET":    "test-jwt-secret",
				"OPEN_SANDBOX_DATA_DIR": "/tmp/open-sandbox",
				"SANDBOX_SECRETS_KEY":   "0123456789abcdef0123456789abcdef",
			})
			if !strings.Contains(setOutput, "SANDBOX_SECRETS_KEY=0123456789abcdef0123456789abcdef") {
				t.Fatalf("expected SANDBOX_SECRETS_KEY to be present when set, got output %q", setOutput)
			}
		})
	}
}

func TestInstallScriptSecretsKeyOnlyForwardsWhenConfigured(t *testing.T) {
	repoRoot := repoRoot(t)
	scriptPath := filepath.Join(repoRoot, "install.sh")

	for _, tc := range []struct {
		name             string
		secretsKey       string
		wantSecretsFlag  bool
		wantSecretsValue string
	}{
		{name: "unset", wantSecretsFlag: true, wantSecretsValue: "generated-jwt-secret"},
		{name: "set", secretsKey: "0123456789abcdef0123456789abcdef", wantSecretsFlag: true, wantSecretsValue: "0123456789abcdef0123456789abcdef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			mockBinDir := filepath.Join(tmpDir, "bin")
			if err := os.MkdirAll(mockBinDir, 0o755); err != nil {
				t.Fatalf("failed to create mock bin dir: %v", err)
			}

			dockerLogPath := filepath.Join(tmpDir, "docker.log")
			writeExecutable(t, filepath.Join(mockBinDir, "docker"), `#!/usr/bin/env bash
set -euo pipefail
log_path="${MOCK_DOCKER_LOG:?}"
quoted=""
for arg in "$@"; do
  quoted+="$(printf '%q ' "$arg")"
done
printf '%s\n' "${quoted% }" >> "$log_path"

cmd="${1:-}"
shift || true

case "$cmd" in
  pull|rm|logs)
    exit 0
    ;;
  network)
    if [[ "${1:-}" == "inspect" ]]; then
      exit 1
    fi
    exit 0
    ;;
  volume)
    if [[ "${1:-}" == "inspect" ]]; then
      exit 1
    fi
    exit 0
    ;;
  container)
    if [[ "${1:-}" == "inspect" ]]; then
      exit 1
    fi
    exit 0
    ;;
  inspect)
    printf 'healthy\n'
    exit 0
    ;;
  run)
    exit 0
    ;;
esac

exit 0
`)
			writeExecutable(t, filepath.Join(mockBinDir, "openssl"), `#!/usr/bin/env bash
set -euo pipefail
counter_file="${MOCK_OPENSSL_COUNTER_FILE:?}"
counter=0
if [[ -f "$counter_file" ]]; then
  counter=$(cat "$counter_file")
fi
counter=$((counter + 1))
printf '%s' "$counter" > "$counter_file"
case "$counter" in
  1) printf 'generated-jwt-secret\n' ;;
  2) printf 'generated-secrets-key-0123456789ab\n' ;;
  *) printf 'unexpected-openssl-call-%s\n' "$counter" ;;
esac
`)
			writeExecutable(t, filepath.Join(mockBinDir, "curl"), "#!/usr/bin/env bash\nexit 0\n")
			writeExecutable(t, filepath.Join(mockBinDir, "sudo"), "#!/usr/bin/env bash\nexec \"$@\"\n")
			writeExecutable(t, filepath.Join(mockBinDir, "id"), `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  -u)
    printf '1000\n'
    ;;
  -un)
    printf 'tester\n'
    ;;
  -gn)
    printf 'tester\n'
    ;;
  *)
    printf 'unsupported id args: %s\n' "$*" >&2
    exit 1
    ;;
esac
`)
			writeExecutable(t, filepath.Join(mockBinDir, "chown"), "#!/usr/bin/env bash\nexit 0\n")

			env := append(os.Environ(),
				"PATH="+mockBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"OPEN_SANDBOX_DATA_DIR="+filepath.Join(tmpDir, "data"),
				"MOCK_DOCKER_LOG="+dockerLogPath,
				"MOCK_OPENSSL_COUNTER_FILE="+filepath.Join(tmpDir, "openssl-counter"),
			)
			if tc.secretsKey != "" {
				env = append(env, "SANDBOX_SECRETS_KEY="+tc.secretsKey)
			}

			cmd := exec.Command("bash", scriptPath)
			cmd.Env = env
			cmd.Dir = repoRoot
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("install.sh failed: %v\n%s", err, output)
			}

			runLine := findDockerRunLine(t, dockerLogPath, "open-sandbox-server")
			hasSecretsFlag := strings.Contains(runLine, "SANDBOX_SECRETS_KEY=")
			if hasSecretsFlag != tc.wantSecretsFlag {
				t.Fatalf("unexpected SANDBOX_SECRETS_KEY forwarding state=%v line=%s", hasSecretsFlag, runLine)
			}
			if tc.wantSecretsFlag && !strings.Contains(runLine, tc.wantSecretsValue) {
				t.Fatalf("expected SANDBOX_SECRETS_KEY value in docker run line, got %s", runLine)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine caller path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func loadServerEnvironment(t *testing.T, composePath string) any {
	t.Helper()

	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", composePath, err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("failed to parse %s: %v", composePath, err)
	}

	services, ok := doc["services"].(map[string]any)
	if !ok {
		t.Fatalf("services missing from %s", composePath)
	}
	server, ok := services["server"].(map[string]any)
	if !ok {
		t.Fatalf("server missing from %s", composePath)
	}
	if _, ok := server["environment"]; !ok {
		t.Fatalf("server.environment missing from %s", composePath)
	}

	return server["environment"]
}

func runComposeServerEnv(t *testing.T, composePath string, envVars map[string]string) string {
	t.Helper()

	tmpDir := t.TempDir()
	envFilePath := filepath.Join(tmpDir, "test.env")
	var envFile bytes.Buffer
	for _, key := range []string{"SANDBOX_JWT_SECRET", "OPEN_SANDBOX_DATA_DIR", "SANDBOX_SECRETS_KEY"} {
		if value, ok := envVars[key]; ok {
			fmt.Fprintf(&envFile, "%s=%s\n", key, value)
		}
	}
	if err := os.WriteFile(envFilePath, envFile.Bytes(), 0o644); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}

	cmd := exec.Command("docker", "compose", "--env-file", envFilePath, "-f", composePath, "run", "--rm", "server")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose run failed: %v\n%s", err, output)
	}

	return strings.TrimSpace(string(output))
}

func findDockerRunLine(t *testing.T, logPath, containerName string) string {
	t.Helper()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read docker log: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if strings.Contains(line, "run") && strings.Contains(line, containerName) {
			return line
		}
	}

	t.Fatalf("failed to find docker run line for %s in %s", containerName, logPath)
	return ""
}

func writeYAML(t *testing.T, path string, value any) {
	t.Helper()

	content, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal yaml: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to write yaml file: %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("failed to write executable %s: %v", path, err)
	}
}
