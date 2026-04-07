# open-sandbox server

This document is for the backend service at `apps/server` in the monorepo.

`open-sandbox` is a local Docker sandbox API for agentic coding workflows.

It supports:
- Docker image build/pull/list/remove
- Docker Compose up/down/status
- Git repository cloning inside running containers
- Direct container creation/start/list/stop/remove
- Running commands inside containers or by sandbox ID
- Streaming container logs (SSE)
- Reading and uploading files inside containers/sandboxes
- Sandbox abstraction with persistent metadata in SQLite
- Swagger UI for API exploration

## Auth

Auth is required for all `/api/*` and `/swagger/*` routes.

Required:
- `SANDBOX_JWT_SECRET`: HS256 secret used to sign issued JWTs

Optional:
- `SANDBOX_JWT_TTL`: token lifetime (default `15m`)
- `SANDBOX_REFRESH_TTL`: refresh token lifetime (default `720h` / 30 days)
- `SANDBOX_JWT_ISSUER`: issuer claim (default `open-sandbox`)
- `SANDBOX_CORS_ORIGINS`: comma-separated origins (default `http://localhost:5173,http://127.0.0.1:5173`)
- `SANDBOX_WORKSPACE_DIR`: base directory for `context_path` and managed compose projects (default user home directory)
- `SANDBOX_DB_PATH`: SQLite path for sandbox metadata (default resolves to `apps/server/open-sandbox.db` in the repo layout)
- `SANDBOX_RUNTIME_MEMORY_LIMIT`: default memory limit for created sandboxes and managed containers, for example `4g`
- `SANDBOX_RUNTIME_CPU_LIMIT`: default CPU limit for created sandboxes and managed containers, for example `2`
- `SANDBOX_RUNTIME_PIDS_LIMIT`: default PID limit for created sandboxes and managed containers, for example `512`
- `SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE`: retention window for stale direct-container specs and compose project directories, default `168h`
- `SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE`: retention window for sandbox records whose container is already gone, default `24h`
- `SANDBOX_PROXY_AUTH_RATE_LIMIT_RPS`: per-user ForwardAuth request rate limit for `/auth/proxy/authorize`, default `120`
- `SANDBOX_PROXY_AUTH_RATE_LIMIT_BURST`: per-user ForwardAuth burst allowance, default `240`
- `SANDBOX_PROXY_AUTH_RATE_LIMIT_IDLE_TTL`: cleanup TTL for idle per-user rate limiter entries, default `10m`
- `SANDBOX_PUBLIC_BASE_URL`: public app URL used to build preview launcher links, default `http://app.lvh.me:3000`
- `SANDBOX_PREVIEW_BASE_DOMAIN`: wildcard preview domain; when unset it is auto-derived from `SANDBOX_PUBLIC_BASE_URL` host as `preview.<base-domain>` (fallback `preview.lvh.me`)
- `SANDBOX_PREVIEW_SESSION_TTL`: signed preview session lifetime, default `10m`
- `SANDBOX_TRAEFIK_TRUST_FORWARDED_HEADERS`: set to `true` when an external proxy (Nginx, OpenResty, etc.) sits in front of Traefik, so Traefik trusts incoming `X-Forwarded-*` headers; default `false`

`/health` is intentionally public.

`/metrics` is also public so it can be scraped through the same Traefik entrypoint on a single server.

Traefik is the only public proxy in packaged deployments. The server remains the API/auth/control plane and publishes dynamic Traefik config for host-based preview routes.

Preview caveats:
- previews are launched via `/auth/preview/launch/...` and redirected to `*.preview.lvh.me`
- preview routes are created only for published ports
- compose services with internal-only ports are intentionally not previewable

When the database has no users yet, create the initial admin account through `/auth/bootstrap` or the UI login screen. Additional users are managed through `/api/users` by an admin session.

## Run

You can place env vars in `apps/server/.env` (auto-loaded at startup), or export them in your shell.

```bash
bun run dev:server
```

This uses `go tool air -c .air.toml` for hot reload. From `apps/server`, you can run the same watcher directly with `SANDBOX_JWT_SECRET=dev-jwt-signing-secret go tool air -c .air.toml`.

Server defaults to `:8080` (override with `PORT`).

When `SANDBOX_DB_PATH` is not set, the server detects the repo layout and stores SQLite data in `apps/server/open-sandbox.db` by default, including when `air` runs the compiled binary from `apps/server/tmp`.

When `SANDBOX_WORKSPACE_DIR` is not set, the workspace root defaults to the current user's home directory.

## Swagger

- URL: `http://localhost:8080/swagger/index.html`
- Use a Bearer token from `/auth/login` in requests.

## API examples

Set helper variables:

```bash
export BASE_URL="http://localhost:8080"
export USERNAME="admin"
export PASSWORD="local-dev-password"
export TOKEN=$(curl -s -X POST "$BASE_URL/auth/bootstrap" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}" | jq -r '.token')
export AUTH_HEADER="Authorization: Bearer ${TOKEN}"
```

### Health

```bash
curl "$BASE_URL/health"
```

### Bootstrap first admin

```bash
curl -X POST "$BASE_URL/auth/bootstrap" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"local-dev-password"}'
```

### Login

```bash
curl -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"local-dev-password"}'
```

### Refresh session

`/auth/refresh` rotates the refresh token cookie and issues a fresh access token.

```bash
curl -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"local-dev-password"}' \
  -c ./auth-cookies.txt

curl -X POST "$BASE_URL/auth/refresh" \
  -b ./auth-cookies.txt \
  -c ./auth-cookies.txt
```

### List users

```bash
curl "$BASE_URL/api/users" -H "$AUTH_HEADER"
```

### Create user

```bash
curl -X POST "$BASE_URL/api/users" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret-password","role":"member"}'
```

### Build image

```bash
curl -X POST "$BASE_URL/api/images/build" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "context_path": "./project",
    "dockerfile": "Dockerfile",
    "tag": "sandbox-app:latest",
    "build_args": {"APP_ENV":"dev"}
  }'
```

### Build image from Dockerfile text (no project directory needed)

```bash
curl -X POST "$BASE_URL/api/images/build" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "tag": "sandbox-inline:latest",
    "dockerfile": "Dockerfile",
    "dockerfile_content": "FROM alpine:3.20\nRUN echo hello\n",
    "context_files": {
      "app.txt": "hello from context"
    }
  }'
```

Use either `context_path` or `dockerfile_content`.

### Pull image

```bash
curl -X POST "$BASE_URL/api/images/pull" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{"image":"alpine","tag":"3.20"}'
```

### List images

```bash
curl "$BASE_URL/api/images" -H "$AUTH_HEADER"
```

### Remove image

```bash
curl -X DELETE "$BASE_URL/api/images/alpine:3.20?force=true" \
  -H "$AUTH_HEADER"
```

### Compose up

Compose projects are written to `<workspace-root>/.open-sandbox/compose/<project-name>/docker-compose.yml` before `docker compose` runs, so naming and relative paths use a stable managed directory instead of a random temp file.

```bash
curl -X POST "$BASE_URL/api/compose/up" \
  -H "$AUTH_HEADER" \
  -H "Accept: text/event-stream" \
  -H "Accept-Encoding: identity" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "services:\n  app:\n    image: alpine:3.20\n",
    "project_name": "sandbox",
    "services": ["app"]
  }'
```

Use `project_name` whenever possible. It becomes both the Docker Compose project name and the managed directory name under `.open-sandbox/compose`.

### Compose down

```bash
curl -X POST "$BASE_URL/api/compose/down" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "services:\n  app:\n    image: alpine:3.20\n",
    "project_name": "sandbox",
    "volumes": true,
    "remove_orphans": true
  }'
```

### Compose status

```bash
curl -X POST "$BASE_URL/api/compose/status" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "services:\n  app:\n    image: alpine:3.20\n",
    "project_name": "sandbox"
  }'
```

### Clone git repo inside container

```bash
curl -X POST "$BASE_URL/api/git/clone" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "container_id": "<running-container-id>",
    "repo_url": "https://github.com/example/repo.git",
    "target_path": "/workspace/repo",
    "branch": "main"
  }'
```

### Create sandbox

Creates an isolated container + Docker volume + persisted sandbox record.

```bash
curl -X POST "$BASE_URL/api/sandboxes" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "workspace",
    "image": "ubuntu:24.04",
    "repo_url": "https://github.com/example/repo.git",
    "branch": "main"
  }'
```

### List sandboxes

```bash
curl "$BASE_URL/api/sandboxes" -H "$AUTH_HEADER"
```

### Exec in sandbox

```bash
curl -X POST "$BASE_URL/api/sandboxes/<sandbox-id>/exec" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{"cmd": ["sh", "-lc", "ls -la /workspace"]}'
```

### Upload file to sandbox

```bash
curl -X PUT "$BASE_URL/api/sandboxes/<sandbox-id>/files" \
  -H "$AUTH_HEADER" \
  -F "target_path=/workspace/notes.txt" \
  -F "file=@./notes.txt"
```

### Read file from sandbox

```bash
curl "$BASE_URL/api/sandboxes/<sandbox-id>/files?path=/workspace/notes.txt" \
  -H "$AUTH_HEADER"
```

### Create container directly

If the image is not available locally, the API automatically pulls it and retries container creation.

```bash
curl -X POST "$BASE_URL/api/containers/create" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "image": "alpine:latest",
    "name": "sandbox-alpine",
    "cmd": ["sleep", "3600"],
    "env": ["APP_ENV=dev"],
    "workdir": "/",
    "binds": ["/tmp:/host-tmp"],
    "auto_remove": true,
    "start": true
  }'
```

### Execute command in container

```bash
curl -X POST "$BASE_URL/api/containers/<container-id>/exec" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "cmd": ["sh", "-lc", "ls -la /workspace"],
    "workdir": "/workspace",
    "env": ["NODE_ENV=development"],
    "detach": false,
    "tty": false
  }'
```

### Stream logs (SSE)

```bash
curl -N "$BASE_URL/api/containers/<container-id>/logs?follow=true&tail=100" \
  -H "$AUTH_HEADER" \
  -H "Accept: text/event-stream" \
  -H "Accept-Encoding: identity"
```

## Auth notes

All `/api/*` and `/swagger/*` endpoints require `Authorization: Bearer <token>`.
Use `/auth/login` to get short-lived access tokens and a refresh cookie.
Use `/auth/refresh` to rotate the refresh token and mint a new access token before access token expiry.

## Run endpoint tests

Unit tests for auth + endpoint handler behavior:

```bash
bun run test:server
```

From `apps/server`, you can still run `go test ./...` directly.

Focused endpoint tests:

```bash
go test ./internal/api -run Test
```
