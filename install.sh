#!/usr/bin/env bash

set -euo pipefail

PROJECT_NAME="open-sandbox"
DATA_DIR="${OPEN_SANDBOX_DATA_DIR:-/var/lib/open-sandbox}"
CONFIG_DIR="${OPEN_SANDBOX_CONFIG_DIR:-$DATA_DIR/config}"
ENV_FILE="$CONFIG_DIR/open-sandbox.env"
DB_DIR="$DATA_DIR/db"
WORKSPACE_DIR="$DATA_DIR/workspace"
IMAGE_TAG="${IMAGE_TAG:-latest}"
OPEN_SANDBOX_HTTP_PORT="${OPEN_SANDBOX_HTTP_PORT:-3000}"
SERVER_IMAGE="ghcr.io/shaik-zeeshan/open-sandbox-server:${IMAGE_TAG}"
CLIENT_IMAGE="ghcr.io/shaik-zeeshan/open-sandbox-client:${IMAGE_TAG}"
INSTALL_USER="${SUDO_USER:-${USER:-$(id -un)}}"
INSTALL_GROUP="$(id -gn "$INSTALL_USER")"
NETWORK_NAME="open-sandbox"
SERVER_CONTAINER_NAME="open-sandbox-server"
CLIENT_CONTAINER_NAME="open-sandbox-client"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'Missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

replace_env_value() {
  local key="$1"
  local value="$2"
  local tmp_file

  tmp_file=$(mktemp)
  grep -vE "^${key}=" "$ENV_FILE" >"$tmp_file" || true
  printf '%s=%s\n' "$key" "$value" >>"$tmp_file"
  mv "$tmp_file" "$ENV_FILE"
}

run_sudo() {
  if [[ $(id -u) -eq 0 ]]; then
    "$@"
    return
  fi

  require_command sudo
  sudo "$@"
}

remove_container_if_present() {
  local container_name="$1"

  if docker container inspect "$container_name" >/dev/null 2>&1; then
    docker rm -f "$container_name" >/dev/null
  fi
}

ensure_network() {
  if docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
    return
  fi

  docker network create "$NETWORK_NAME" >/dev/null
}

wait_for_server_health() {
  local status

  for ((i = 0; i < 30; i += 1)); do
    status=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}starting{{end}}' "$SERVER_CONTAINER_NAME")
    if [[ "$status" == "healthy" ]]; then
      return
    fi
    if [[ "$status" == "unhealthy" ]]; then
      break
    fi

    sleep 2
  done

  printf 'The server container did not become healthy in time.\n' >&2
  docker logs "$SERVER_CONTAINER_NAME" >&2 || true
  exit 1
}

require_command docker
require_command openssl
require_command curl
require_command grep

run_sudo mkdir -p "$DATA_DIR" "$DB_DIR" "$WORKSPACE_DIR" "$CONFIG_DIR"
run_sudo chown root:root "$DATA_DIR" "$DB_DIR" "$WORKSPACE_DIR"
run_sudo chmod 755 "$DATA_DIR"
run_sudo chmod 770 "$DB_DIR" "$WORKSPACE_DIR"
run_sudo chown "$INSTALL_USER:$INSTALL_GROUP" "$CONFIG_DIR"
run_sudo chmod 700 "$CONFIG_DIR"

if [[ ! -f "$ENV_FILE" ]]; then
  : >"$ENV_FILE"
fi

JWT_SECRET=$(grep -E '^SANDBOX_JWT_SECRET=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)
if [[ -z "$JWT_SECRET" || "$JWT_SECRET" == "change-me" ]]; then
  JWT_SECRET=$(openssl rand -hex 32)
  replace_env_value SANDBOX_JWT_SECRET "$JWT_SECRET"
fi

replace_env_value OPEN_SANDBOX_DATA_DIR "$DATA_DIR"
replace_env_value OPEN_SANDBOX_HTTP_PORT "$OPEN_SANDBOX_HTTP_PORT"
chmod 600 "$ENV_FILE"

SANDBOX_RUNTIME_MEMORY_LIMIT=$(grep -E '^SANDBOX_RUNTIME_MEMORY_LIMIT=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)
SANDBOX_RUNTIME_CPU_LIMIT=$(grep -E '^SANDBOX_RUNTIME_CPU_LIMIT=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)
SANDBOX_RUNTIME_PIDS_LIMIT=$(grep -E '^SANDBOX_RUNTIME_PIDS_LIMIT=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)
SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE=$(grep -E '^SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)
SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE=$(grep -E '^SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)

SANDBOX_RUNTIME_MEMORY_LIMIT=${SANDBOX_RUNTIME_MEMORY_LIMIT:-4g}
SANDBOX_RUNTIME_CPU_LIMIT=${SANDBOX_RUNTIME_CPU_LIMIT:-2}
SANDBOX_RUNTIME_PIDS_LIMIT=${SANDBOX_RUNTIME_PIDS_LIMIT:-512}
SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE=${SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE:-168h}
SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE=${SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE:-24h}

docker pull "$SERVER_IMAGE"
docker pull "$CLIENT_IMAGE"

ensure_network
remove_container_if_present "$CLIENT_CONTAINER_NAME"
remove_container_if_present "$SERVER_CONTAINER_NAME"

docker run -d \
  --name "$SERVER_CONTAINER_NAME" \
  --restart unless-stopped \
  --network "$NETWORK_NAME" \
  --network-alias server \
  --memory 1g \
  --cpus 1.0 \
  --pids-limit 256 \
  --health-cmd 'curl -fsS http://127.0.0.1:8080/health || exit 1' \
  --health-interval 10s \
  --health-timeout 5s \
  --health-retries 12 \
  -e PORT=8080 \
  -e SANDBOX_DB_PATH=/data/open-sandbox.db \
  -e SANDBOX_JWT_SECRET="$JWT_SECRET" \
  -e SANDBOX_WORKSPACE_DIR="$WORKSPACE_DIR" \
  -e SANDBOX_RUNTIME_MEMORY_LIMIT="$SANDBOX_RUNTIME_MEMORY_LIMIT" \
  -e SANDBOX_RUNTIME_CPU_LIMIT="$SANDBOX_RUNTIME_CPU_LIMIT" \
  -e SANDBOX_RUNTIME_PIDS_LIMIT="$SANDBOX_RUNTIME_PIDS_LIMIT" \
  -e SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE="$SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE" \
  -e SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE="$SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$DB_DIR:/data" \
  -v "$WORKSPACE_DIR:$WORKSPACE_DIR" \
  "$SERVER_IMAGE" >/dev/null

wait_for_server_health

docker run -d \
  --name "$CLIENT_CONTAINER_NAME" \
  --restart unless-stopped \
  --network "$NETWORK_NAME" \
  --memory 256m \
  --cpus 0.5 \
  --pids-limit 128 \
  -p "$OPEN_SANDBOX_HTTP_PORT:80" \
  "$CLIENT_IMAGE" >/dev/null

for ((i = 0; i < 30; i += 1)); do
  if curl -fsS "http://127.0.0.1:${OPEN_SANDBOX_HTTP_PORT}/health" >/dev/null 2>&1; then
    printf 'open-sandbox is ready at http://localhost:%s\n' "$OPEN_SANDBOX_HTTP_PORT"
    printf 'Config: %s\n' "$CONFIG_DIR"
    printf 'Images: %s and %s\n' "$CLIENT_IMAGE" "$SERVER_IMAGE"
    exit 0
  fi

  sleep 2
done

printf 'The stack started but the health check did not pass in time.\n' >&2
printf 'Inspect it with: docker ps --filter name=%s --filter name=%s\n' "$SERVER_CONTAINER_NAME" "$CLIENT_CONTAINER_NAME" >&2
exit 1
