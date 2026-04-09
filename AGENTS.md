# AGENTS.md

## Purpose

`open-sandbox` is a monorepo for a local Docker sandbox platform used by agentic coding workflows. The repo contains the API/control plane, the dashboard UI, and the TypeScript SDK for that API.

## What Lives Here

- Tooling: Bun workspaces + Turbo. Use Bun `1.3.5`; CI installs with `bun install --frozen-lockfile`.
- `apps/server`: Go `1.25.5` API, auth, and sandbox control plane. Entrypoint: `apps/server/main.go`.
- `apps/client`: SvelteKit 2 / Svelte 5 dashboard. Prefer existing rune-mode patterns (`$state`, `$derived`, `$effect`).
- `packages/sdk`: TypeScript SDK for the server API. Keep it aligned with server request/response/auth changes.
- Effect code: follow `.agents/skills/effect-ts-guide/SKILL.md` and the references in that directory.

## Universal Repo Rules

- Prefer Bun/Turbo entrypoints over ad hoc commands: `bun run dev`, `bun run build`, `bun run check`, `bun run test`.
- Run the smallest relevant verification after changes: `bun run test:server`, `bun run check:client`, `bun run build:client`, or `bun run build` for cross-workspace changes.
- `bun run test:server` is unit-style and does not require a live Docker daemon.
- If server API behavior or schemas change, regenerate Swagger docs in `apps/server/docs` and update `packages/sdk`.
- If Docker or deploy files change, verify with `docker build ./apps/server` and `docker build . -f apps/client/Dockerfile --build-arg VITE_SANDBOX_BASE_URL=/`.

## Critical Gotchas

- The server auto-loads both repo-root `.env` and `apps/server/.env`.
- If `SANDBOX_DB_PATH` is unset, SQLite defaults to `apps/server/open-sandbox.db`, including when `air` runs from `apps/server/tmp`.
- If `SANDBOX_WORKSPACE_DIR` is unset, the server uses the current user's home directory.
- `/health`, `/metrics`, and `/auth/*` are public; `/api/*` is behind auth middleware.
- The client is a static build. In container or self-hosted builds, `VITE_SANDBOX_BASE_URL` must stay `/` so the app resolves the backend from `window.location.origin`.
- Compose deploys require `OPEN_SANDBOX_DATA_DIR` to be an absolute host path, mounted at that same absolute path inside the server container.

## Read More Only When Relevant

- `README.md`: local development, compose workflows, self-hosting, and common commands.
- `apps/server/README.md`: server env vars, auth model, preview routing, and API behavior.
- `apps/client/README.md`: client env vars, dev server behavior, and production build notes.
- `packages/sdk/README.md`: SDK usage and runnable examples.

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
