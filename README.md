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
