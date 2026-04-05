# open-sandbox monorepo

Monorepo for the open-sandbox backend and dashboard.

## Structure

- `apps/server`: Go API service (existing open-sandbox backend)
- `apps/client`: Svelte dashboard for managing sandboxes and containers

## Quick start

`apps/server` auto-loads a local `.env` file via `godotenv`, so you can set env vars there.

```bash
bun install
bun run dev
```

On first launch, create the initial admin account from the UI login screen.

Default URLs:
- API: `http://localhost:8080`
- UI: `http://localhost:5173`

Run individual apps when needed:

```bash
bun run dev:server
bun run dev:client
```

`bun run dev:server` uses `air` for Go hot reload, with a local fallback `SANDBOX_JWT_SECRET` for development.

Unless `SANDBOX_DB_PATH` is set, the server keeps its default SQLite file at `apps/server/open-sandbox.db`, even when the dev watcher starts the process from a nested temp directory.

## Run server tests

```bash
bun run test:server
```

## Run client checks

```bash
bun run check:client
```

## Helpful Make targets

```bash
make test-server
make check-client
make build-client
```

The Make targets are thin wrappers around the root Bun/Turbo scripts, so `bun run ...` and `make ...` are interchangeable for the common workflows above.

More API usage docs are in `apps/server/README.md`.

## Self-host with Docker Compose

The repo now includes a top-level `compose.yaml` for a single-server deployment.

Prerequisites:
- Docker with the Compose plugin

Setup:

```bash
cp .env.example .env
mkdir -p /var/lib/open-sandbox/db /var/lib/open-sandbox/workspace
docker compose build
docker compose up -d
```

Default URL:
- UI, API proxy, and Swagger: `http://localhost:3000`

The stack runs three containers:
- `client`: Nginx serving the built dashboard and proxying backend routes
- `server`: Go API service

The API uses the host Docker daemon through `/var/run/docker.sock`.

### Compose env vars

Top-level `.env` values used by `compose.yaml`:
- `SANDBOX_JWT_SECRET`: required; set this to a strong secret before production use
- `OPEN_SANDBOX_DATA_DIR`: required absolute host path for persistent state, default example `/var/lib/open-sandbox`
- `OPEN_SANDBOX_HTTP_PORT`: optional public port for the UI/proxy container, default `3000`
- `SANDBOX_RUNTIME_MEMORY_LIMIT`: optional default memory limit for created sandboxes/direct containers, default `4g`
- `SANDBOX_RUNTIME_CPU_LIMIT`: optional default CPU limit for created sandboxes/direct containers, default `2`
- `SANDBOX_RUNTIME_PIDS_LIMIT`: optional default PID limit for created sandboxes/direct containers, default `512`

Backend env vars inside the stack:
- `SANDBOX_DB_PATH=/data/open-sandbox.db`
- `SANDBOX_WORKSPACE_DIR=${OPEN_SANDBOX_DATA_DIR}/workspace`

`OPEN_SANDBOX_DATA_DIR` should be an absolute host path. The workspace directory is mounted into the server container at the same absolute path so `docker compose` and other Docker path-based operations keep resolving correctly through the host daemon.

### Persistent data

The compose stack keeps data under `${OPEN_SANDBOX_DATA_DIR}`:
- `${OPEN_SANDBOX_DATA_DIR}/db`: SQLite users, refresh tokens, and sandbox metadata
- `${OPEN_SANDBOX_DATA_DIR}/workspace`: managed compose projects and other workspace state

Sandbox containers, images, and named Docker volumes are stored in the host Docker engine because the stack uses the host socket.

### Startup notes

After the first start:
- Open `http://localhost:${OPEN_SANDBOX_HTTP_PORT:-3000}`
- Create the initial admin account from the login screen
- API health is available through the same origin at `/health`
- Swagger is available through the same origin at `/swagger/index.html`

### Upgrade basics

Container upgrades keep the named volumes by default.

```bash
docker compose build
docker compose up -d
```

For image-based deploys, replace the build step with `docker compose pull`.

If you need a backup before upgrading, back up `${OPEN_SANDBOX_DATA_DIR}` and any host Docker volumes or images you want to preserve.

