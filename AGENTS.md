# AGENTS.md

**Workspace**

- Root tooling is Bun workspaces + Turbo. Use Bun `1.3.5`; CI installs with `bun install --frozen-lockfile`.
- `apps/server` is the Go `1.25.5` API. Entrypoint: `apps/server/main.go`. Most route/runtime logic lives in `apps/server/internal/api`. SQLite schema and migrations live in `apps/server/internal/store/sqlite.go`.
- `apps/client` is the SvelteKit 2 / Svelte 5 dashboard. Main wiring lives in `apps/client/src/routes/+layout.svelte`, `apps/client/src/routes/+page.svelte`, and `apps/client/src/lib/{api,auth-controller,stores}.svelte.ts`.

**Commands**

- Full dev: `bun run dev`
- Server dev: `bun run dev:server` (`go tool air -c .air.toml`; injects fallback `SANDBOX_JWT_SECRET=dev-jwt-signing-secret`)
- Client dev: `bun run dev:client`
- Client verification: `bun run check:client`
- Server verification: `bun run test:server`
- CI order is `bun run check:client` -> `bun run test:server` -> Docker image build validation for both apps.

**Gotchas**

- The server auto-loads both repo-root `.env` and `apps/server/.env`.
- If `SANDBOX_DB_PATH` is unset, SQLite resolves to `apps/server/open-sandbox.db` even when `air` runs from `apps/server/tmp`.
- If `SANDBOX_WORKSPACE_DIR` is unset, the server uses the current user's home directory; relative workspace paths resolve inside that root.
- Current Go tests are unit-style: mocked Docker API plus temp SQLite, so `bun run test:server` does not require a live Docker daemon.
- `/health`, `/metrics`, and `/auth/*` are public; `/api/*` is behind auth middleware.
- Client code is in Svelte 5 rune mode by default; follow existing `$state`, `$derived`, and `$effect` patterns.
- `VITE_SANDBOX_BASE_URL` defaults to `http://localhost:8080`. In container/self-hosted builds it must stay `/`, which the client resolves from `window.location.origin`.
- The client build is static (`@sveltejs/adapter-static` with `fallback: 'index.html'`); Traefik is the edge proxy for `/api`, `/auth`, `/health`, `/metrics`, `/swagger`, and `/proxy/...`.
- Compose deploys require `OPEN_SANDBOX_DATA_DIR` to be an absolute host path, and the workspace directory must be mounted at that same absolute path inside the server container.

**Docker**

- Server image build context is `apps/server`.
- Client image build context is repo root with `-f apps/client/Dockerfile`; it needs the root `package.json` and `bun.lock`.
- If Docker or deploy files change, mirror CI with `docker build ./apps/server` and `docker build . -f apps/client/Dockerfile --build-arg VITE_SANDBOX_BASE_URL=/`.

<!-- opensrc:start -->

- Dependency source snapshots live in `opensrc/`; check them before guessing package internals.
- `opensrc/sources.json` lists fetched packages and versions.
- Fetch more with `npx opensrc <package>`, `npx opensrc pypi:<package>`, `npx opensrc crates:<package>`, or `npx opensrc <owner>/<repo>`.

<!-- opensrc:end -->
