# open-sandbox client

This app is the Svelte dashboard in `apps/client`.

## Development

```sh
bun run dev:client
```

By default the UI talks to `http://localhost:8080`. Override that with `apps/client/.env`:

```sh
VITE_SANDBOX_BASE_URL=http://localhost:8080
```

## Production build

The production build uses `@sveltejs/adapter-static` and emits a static site.

```sh
bun run build:client
```

In the bundled self-hosted stack, this container serves static assets only. Traefik is the public edge proxy and routes `/api`, `/auth`, `/health`, `/metrics`, `/swagger`, and `/proxy/...` to the correct backend service.
