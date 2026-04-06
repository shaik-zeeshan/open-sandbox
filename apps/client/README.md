# open-sandbox client

This app is the Svelte dashboard in `apps/client`.

## Development

```sh
bun run dev:client
```

By default the UI talks to `http://localhost:8080`. Override that with `apps/client/.env`:

```sh
VITE_SANDBOX_BASE_URL=http://localhost:8080
VITE_ALLOWED_HOSTS=app.lvh.me,.lvh.me,my.custom.host
```

`VITE_ALLOWED_HOSTS` is a comma-separated list of additional hostnames that the Vite dev server should accept.

## Production build

The production build uses `@sveltejs/adapter-static` and emits a static site.

```sh
bun run build:client
```

In the bundled self-hosted stack, this container serves static assets only. Traefik is the public edge proxy and routes `/api`, `/auth`, `/health`, `/metrics`, `/swagger`, and preview launcher routes under `/auth/preview/launch/...` to the correct backend service.
