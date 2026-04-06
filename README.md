# open-sandbox monorepo

Monorepo for the open-sandbox API and dashboard.

Traefik is the only public proxy in self-hosted deployments. The client container serves only static assets, while the server remains the API/auth/control plane.

## Repo layout

- `apps/server`: Go API service for sandbox/container management
- `apps/client`: Svelte dashboard UI
- `compose.dev.yaml`: Docker Compose hot-reload development stack
- `compose.yaml`: local build-based Docker deployment
- `compose.ghcr.yaml`: image-based Docker deployment from GHCR
- `install.sh`: standalone installer for GHCR images (`docker run` based)

## Prerequisites

- Bun `1.3.x`
- Go `1.25.x`
- Docker (for runtime features and self-hosting)

## Local development

The server auto-loads `apps/server/.env` via `godotenv` if present.

See `make help` for a full command list (workspace + compose shortcuts).

```bash
bun install
bun run dev
```

Default dev URLs:
- API: `http://localhost:8080`
- UI: `http://localhost:5173`

Run each app separately when needed:

```bash
bun run dev:server
bun run dev:client
```

Notes:
- `bun run dev:server` uses `go tool air -c .air.toml` and falls back to `SANDBOX_JWT_SECRET=dev-jwt-signing-secret` for local development.
- If `SANDBOX_DB_PATH` is not set, SQLite defaults to `apps/server/open-sandbox.db` (including when `air` runs binaries from `apps/server/tmp`).

On first launch, create the initial admin account from the login screen.

### Docker Compose hot reload (DX)

Use `compose.dev.yaml` when you want both apps running in containers with live reload while editing source files on the host.

```bash
docker compose -f compose.dev.yaml up
```

Or via Make:

```bash
make compose-dev-up
```

Default URL:
- UI + API + auth launcher routes: `http://localhost:3000` (or any hostname resolving to this machine)

Notes:
- server hot reload runs with `go tool air -c .air.toml`
- client hot reload runs with Vite on port `80` inside the container, proxied by Traefik
- code is bind-mounted from the repo (`./:/workspace`) so edits are reflected immediately
- development data defaults to `/tmp/open-sandbox` unless `OPEN_SANDBOX_DATA_DIR` is set

## Common commands

```bash
bun run test:server
bun run check:client
bun run build:client
```

Equivalent Make targets:

```bash
make help
make test-server
make check-client
make build-client
```

## Self-host with Docker Compose

Use `compose.yaml` to build and run the stack on one host.

```bash
cp .env.example .env
mkdir -p /var/lib/open-sandbox/db /var/lib/open-sandbox/workspace
docker compose build
docker compose up -d
```

Default URL:
- UI + API + auth launcher routes: `http://localhost:3000` (or any hostname resolving to this machine)

The stack runs three containers:
- `traefik`: public edge proxy (the only published port)
- `server`: Go API/auth/control-plane service
- `client`: static dashboard content

The API talks to the host Docker daemon through `/var/run/docker.sock`.

### Preview routing model

Preview URLs are launcher routes on the main host and redirect to dedicated preview subdomains:
- sandboxes: `/auth/preview/launch/sandboxes/<sandbox-id>/<private-port>`
- managed containers: `/auth/preview/launch/containers/<managed-id>/<private-port>`
- compose services: `/auth/preview/launch/compose/<project>/<service>/<private-port>`

Important behavior:
- compose previews are available only for ports published to the host (`HOST:CONTAINER`)
- container ports that are only internal (for example `3000/tcp` with no host publish) are not previewable
- previews are served from dedicated hosts (`*.preview.lvh.me` by default), so apps run at `/` without path-prefix rewrites

### Compose environment variables

Top-level values (from `.env`):
- `SANDBOX_JWT_SECRET` (required): set a strong secret for production
- `OPEN_SANDBOX_DATA_DIR` (required): absolute host path for persistent state (example: `/var/lib/open-sandbox`)
- `OPEN_SANDBOX_HTTP_PORT` (optional, default `3000`)
- `SANDBOX_RUNTIME_MEMORY_LIMIT` (optional, default `4g`)
- `SANDBOX_RUNTIME_CPU_LIMIT` (optional, default `2`)
- `SANDBOX_RUNTIME_PIDS_LIMIT` (optional, default `512`)
- `SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE` (optional, default `168h`)
- `SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE` (optional, default `24h`)
- `SANDBOX_PROXY_AUTH_RATE_LIMIT_RPS` (optional, default `120`)
- `SANDBOX_PROXY_AUTH_RATE_LIMIT_BURST` (optional, default `240`)
- `SANDBOX_PROXY_AUTH_RATE_LIMIT_IDLE_TTL` (optional, default `10m`)
- `SANDBOX_PUBLIC_BASE_URL` (optional, default `http://app.lvh.me:${OPEN_SANDBOX_HTTP_PORT}`)
  - set this to your external app URL when you expose a non-default host/port (for preview callback links)
- `SANDBOX_PREVIEW_BASE_DOMAIN` (optional, default auto-derived from `SANDBOX_PUBLIC_BASE_URL` host as `preview.<base-domain>`, fallback `preview.lvh.me`)
- `SANDBOX_PREVIEW_SESSION_TTL` (optional, default `10m`)

Backend values inside the server container:
- `SANDBOX_DB_PATH=/data/open-sandbox.db`
- `SANDBOX_WORKSPACE_DIR=${OPEN_SANDBOX_DATA_DIR}/workspace`

`OPEN_SANDBOX_DATA_DIR` must be an absolute host path. The workspace directory is bind-mounted at the same absolute path inside the server container so Docker path-based operations resolve correctly via the host daemon.

## GHCR images

Published images:
- `ghcr.io/shaik-zeeshan/open-sandbox-client`
- `ghcr.io/shaik-zeeshan/open-sandbox-server`

Common tags:
- `latest` (from `main`)
- `sha-<commit>`
- branch names
- `pr-<number>` (same-repo pull requests)
- `v*` git tags

Use `compose.ghcr.yaml` when you want to pull images instead of building locally.
It uses the same routing topology (`traefik` edge + static `client` + `server` control plane).

## Install from GHCR

Quick install (download and run in one command):

```bash
curl -fsSL https://raw.githubusercontent.com/shaik-zeeshan/open-sandbox/main/install.sh | bash
```

Or download first and inspect the script:

```bash
curl -fsSL https://raw.githubusercontent.com/shaik-zeeshan/open-sandbox/main/install.sh -o install.sh
chmod +x install.sh
./install.sh
```

```bash
./install.sh
```

Optional tag override:

```bash
IMAGE_TAG=v1.2.3 ./install.sh
```

`install.sh`:
- can run as a standalone script
- creates/updates `/var/lib/open-sandbox/config/open-sandbox.env`
- generates `SANDBOX_JWT_SECRET` with `openssl` if missing
- prepares `/var/lib/open-sandbox/db` and `/var/lib/open-sandbox/workspace`
- sets required directory permissions
- pulls GHCR images and starts `traefik`, `server`, and `client` with `docker run`

Because it writes under `/var/lib/open-sandbox`, it may prompt for `sudo`.

## Data and upgrades

Persistent data under `${OPEN_SANDBOX_DATA_DIR}`:
- `${OPEN_SANDBOX_DATA_DIR}/db`: SQLite users, refresh tokens, sandbox metadata
- `${OPEN_SANDBOX_DATA_DIR}/workspace`: managed compose projects and workspace state

After first start:
- open `http://localhost:${OPEN_SANDBOX_HTTP_PORT:-3000}` (or your mapped hostname)
- create the initial admin account from the login screen
- health endpoint: `/health`
- Swagger: `/swagger/index.html`

Upgrade commands:

```bash
docker compose build
docker compose up -d
```

For image-based deploys, use `docker compose pull` before `docker compose up -d`.

## App-level docs

- API/service details: `apps/server/README.md`
- Dashboard details: `apps/client/README.md`
