# AGENTS.md

**Workspace**

- Root tooling is Bun workspaces + Turbo. Use Bun `1.3.5`; CI installs with `bun install --frozen-lockfile`.
- `apps/server` is the Go `1.25.5` API. Entrypoint: `apps/server/main.go`. Most route/runtime logic lives in `apps/server/internal/api`. SQLite schema and migrations live in `apps/server/internal/store/sqlite.go`.
- `apps/client` is the SvelteKit 2 / Svelte 5 dashboard. Main wiring lives in `apps/client/src/routes/+layout.svelte`, `apps/client/src/routes/+page.svelte`, and `apps/client/src/lib/{api,auth-controller,stores}.svelte.ts`.
- For Effect code, follow `.agents/skills/effect-ts-guide/SKILL.md`, `.agents/skills/effect-ts-guide/references/best-practices.md`, and `.agents/skills/effect-ts-guide/references/platform-map.md`.

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

## Source Code Reference

Source code for dependencies is available in `opensrc/` for deeper understanding of implementation details.

See `opensrc/sources.json` for the list of available packages and their versions.

Use this source code when you need to understand how a package works internally, not just its types/interface.

### Fetching Additional Source Code

To fetch source code for a package or repository you need to understand, run:

```bash
npx opensrc <package>           # npm package (e.g., npx opensrc zod)
npx opensrc pypi:<package>      # Python package (e.g., npx opensrc pypi:requests)
npx opensrc crates:<package>    # Rust crate (e.g., npx opensrc crates:serde)
npx opensrc <owner>/<repo>      # GitHub repo (e.g., npx opensrc vercel/ai)
```

<!-- opensrc:end -->
