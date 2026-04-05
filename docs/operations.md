# Operations Runbook

This runbook covers the Phase 2 single-server hardening path. It keeps Docker as the runtime and assumes the top-level `compose.yaml` is the deployed stack.

## Reverse Proxy

The built-in `client` container already proxies SSE and WebSocket traffic correctly to the Go API.

If you place another reverse proxy in front of the stack, point it at `http://127.0.0.1:${OPEN_SANDBOX_HTTP_PORT:-3000}` and keep these settings:

- HTTP/1.1 upstream
- `Upgrade` and `Connection` headers for terminal WebSockets
- `proxy_buffering off` / streaming flush enabled for SSE log streams
- `proxy_request_buffering off`
- long read timeout, at least `1h`

Reference configs:

- `deploy/nginx/open-sandbox.conf`
- `deploy/caddy/Caddyfile`

Proxy verification:

```bash
curl -i http://127.0.0.1:3000/health
curl -i -N http://127.0.0.1:3000/metrics
```

Terminal WebSocket and log streaming verification should be done through the proxy URL, not directly against port `8080`.

## Backup

Persistent application state is split between host files under `${OPEN_SANDBOX_DATA_DIR}` and optional Docker images/volumes you may want to preserve.

Back up host state:

```bash
docker compose stop server
tar -C "$(dirname "$OPEN_SANDBOX_DATA_DIR")" -czf "open-sandbox-data-$(date +%Y%m%d-%H%M%S).tgz" "$(basename "$OPEN_SANDBOX_DATA_DIR")"
docker compose start server
```

Optional Docker image backup for locally built images:

```bash
docker image save -o open-sandbox-images.tar $(docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -v '<none>')
```

## Restore

Restore host state:

```bash
docker compose down
rm -rf "$OPEN_SANDBOX_DATA_DIR"
mkdir -p "$(dirname "$OPEN_SANDBOX_DATA_DIR")"
tar -C "$(dirname "$OPEN_SANDBOX_DATA_DIR")" -xzf open-sandbox-data-YYYYMMDD-HHMMSS.tgz
docker compose up -d
```

Optional image restore:

```bash
docker image load -i open-sandbox-images.tar
```

Restore verification:

```bash
curl -fsS http://127.0.0.1:3000/health
curl -fsS http://127.0.0.1:3000/auth/setup
```

Then log in through the UI, open an existing sandbox, verify terminal access, and verify `/api/sandboxes/:id/logs` streams through the proxy.

## Cleanup And Retention

The API now exposes a scoped admin cleanup endpoint for stale metadata and orphaned state:

- stale direct-container recreate specs under `.open-sandbox/containers`
- stale managed compose project directories under `.open-sandbox/compose`
- sandbox records whose backing container is already gone

Dry run:

```bash
curl -X POST http://127.0.0.1:3000/api/admin/maintenance/cleanup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"dry_run":true}'
```

Apply cleanup:

```bash
curl -X POST http://127.0.0.1:3000/api/admin/maintenance/cleanup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"dry_run":false}'
```

Retention env vars:

- `SANDBOX_MAINTENANCE_ARTIFACT_MAX_AGE` default `168h`
- `SANDBOX_MAINTENANCE_MISSING_SANDBOX_MAX_AGE` default `24h`

For live but no-longer-needed sandboxes, use the normal sandbox delete endpoint instead of maintenance cleanup.

## Logs And Metrics

The server writes JSON request logs plus structured lifecycle and cleanup failure logs to container stdout.

Metrics are exposed at `/metrics` and include:

- `open_sandbox_sandbox_lifecycle_total`
- `open_sandbox_cleanup_runs_total`
- `open_sandbox_cleanup_removed_total`
- `open_sandbox_cleanup_errors_total`

Quick checks:

```bash
curl -fsS http://127.0.0.1:3000/metrics
docker compose logs server --since=10m
```

## Resource Limits

The compose stack now limits the long-running service containers and applies default limits to newly created runtime containers and sandboxes.

Defaults:

- `SANDBOX_RUNTIME_MEMORY_LIMIT=4g`
- `SANDBOX_RUNTIME_CPU_LIMIT=2`
- `SANDBOX_RUNTIME_PIDS_LIMIT=512`

Guidance:

- keep at least `2 GiB` free for the host OS and Docker engine
- start with sandbox memory at 25-40% of total host RAM on small single-server installs
- lower CPU and PIDs if multiple concurrent sandboxes are expected
- arbitrary user-supplied Compose projects are not rewritten; review those separately for their own resource settings

## Verification Checklist

1. Load the UI through the reverse proxy URL.
2. Open a sandbox terminal and verify the WebSocket session stays connected.
3. Open sandbox logs and verify SSE streaming works through the proxy.
4. Run maintenance cleanup in dry-run mode and review counts.
5. Run the documented backup flow, restore into the same host path, and confirm the UI/session state returns.
