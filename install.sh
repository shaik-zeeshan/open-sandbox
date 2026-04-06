#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ENV_FILE="$SCRIPT_DIR/.env"
COMPOSE_FILE="$SCRIPT_DIR/compose.ghcr.yaml"
DATA_DIR="/var/lib/open-sandbox"
DB_DIR="$DATA_DIR/db"
WORKSPACE_DIR="$DATA_DIR/workspace"
IMAGE_TAG="${IMAGE_TAG:-latest}"
OPEN_SANDBOX_HTTP_PORT="${OPEN_SANDBOX_HTTP_PORT:-3000}"

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

require_command docker
require_command openssl
require_command curl
require_command grep

if ! docker compose version >/dev/null 2>&1; then
  printf 'Missing required command: docker compose\n' >&2
  exit 1
fi

if [[ ! -f "$COMPOSE_FILE" ]]; then
  printf 'Missing compose file: %s\n' "$COMPOSE_FILE" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$SCRIPT_DIR/.env.example" "$ENV_FILE"
fi

JWT_SECRET=$(grep -E '^SANDBOX_JWT_SECRET=' "$ENV_FILE" | tail -n 1 | cut -d '=' -f 2- || true)
if [[ -z "$JWT_SECRET" || "$JWT_SECRET" == "change-me" ]]; then
  JWT_SECRET=$(openssl rand -hex 32)
  replace_env_value SANDBOX_JWT_SECRET "$JWT_SECRET"
fi

replace_env_value OPEN_SANDBOX_DATA_DIR "$DATA_DIR"
replace_env_value OPEN_SANDBOX_HTTP_PORT "$OPEN_SANDBOX_HTTP_PORT"
replace_env_value IMAGE_TAG "$IMAGE_TAG"
chmod 600 "$ENV_FILE"

run_sudo mkdir -p "$DATA_DIR" "$DB_DIR" "$WORKSPACE_DIR"
run_sudo chown root:root "$DATA_DIR" "$DB_DIR" "$WORKSPACE_DIR"
run_sudo chmod 755 "$DATA_DIR"
run_sudo chmod 770 "$DB_DIR" "$WORKSPACE_DIR"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" pull
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d

for ((i = 0; i < 30; i += 1)); do
  if curl -fsS "http://127.0.0.1:${OPEN_SANDBOX_HTTP_PORT}/health" >/dev/null 2>&1; then
    printf 'open-sandbox is ready at http://localhost:%s\n' "$OPEN_SANDBOX_HTTP_PORT"
    printf 'Images: ghcr.io/shaik-zeeshan/open-sandbox-client:%s and ghcr.io/shaik-zeeshan/open-sandbox-server:%s\n' "$IMAGE_TAG" "$IMAGE_TAG"
    exit 0
  fi

  sleep 2
done

printf 'The stack started but the health check did not pass in time.\n' >&2
printf 'Inspect it with: docker compose --env-file %s -f %s ps\n' "$ENV_FILE" "$COMPOSE_FILE" >&2
exit 1
