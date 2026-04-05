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

The production build uses `@sveltejs/adapter-static` and emits a static site that can be served by Nginx or another reverse proxy.

```sh
bun run build:client
```

For the bundled self-hosted stack, the client is served by Nginx and proxies `/api`, `/auth`, `/health`, and `/swagger` to the backend so the browser can use a same-origin base URL.
