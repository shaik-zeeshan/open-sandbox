# open-sandbox monorepo

Monorepo for the open-sandbox backend and dashboard.

## Structure

- `apps/server`: Go API service (existing open-sandbox backend)
- `apps/client`: Svelte dashboard for managing sandboxes and containers

## Quick start

`apps/server` auto-loads a local `.env` file via `godotenv`, so you can set env vars there.

```bash
export SANDBOX_JWT_SECRET="dev-jwt-signing-secret"
go run ./apps/server
```

On first launch, create the initial admin account from the UI login screen.

In a second terminal, run the dashboard:

```bash
bun --cwd apps/client install
bun --cwd apps/client dev
```

Default URLs:
- API: `http://localhost:8080`
- UI: `http://localhost:5173`

Or run both together:

```bash
make dev
```

## Run server tests

```bash
go test ./apps/server/...
```

## Run client checks

```bash
bun --cwd apps/client check
```

## Helpful Make targets

```bash
make test-server
make check-client
make build-client
```

More API usage docs are in `apps/server/README.md`.
