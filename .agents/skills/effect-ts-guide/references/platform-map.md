# @effect/platform Map

## Core idea

- `@effect/platform` provides platform-neutral service interfaces and `Layer`s.
- It does not monkey-patch globals; you must provide a platform layer.

## Implementation packages

- Node: `@effect/platform-node`
- Bun: `@effect/platform-bun`
- Browser: `@effect/platform-browser`

## Common mappings

- `FileSystem` -> `node:fs`, `fs/promises`, or Bun file APIs
- `Path` -> `node:path`, Bun path APIs, or Deno path
- `Command` and `CommandExecutor` -> `child_process.spawn`, `exec`, `Deno.Command`, or `Bun.spawn`
- `Terminal` -> `process.stdin`, `stdout`, or `readline`
- `KeyValueStore` -> `Map`, `localStorage`, or file-backed KV
- `PlatformConfigProvider` -> `dotenv`, `process.env`, or file tree config
- `PlatformLogger` -> console or file logging
- `Runtime` and `runMain` -> manual main plus process signal handling

## HTTP stack

- `HttpClient` -> `fetch`, `undici`, or `axios`
- `FetchHttpClient` -> a `fetch` implementation
- `HttpServer`, `HttpRouter`, and `HttpMiddleware` -> `node:http`, `express`, `fastify`, or `koa`
- `HttpApi` and `OpenApi` -> manual routes plus schema and OpenAPI tooling

## Sockets and workers

- `Socket` or `SocketServer` -> `net`, `ws`, or `WebSocket`
- `Worker` or `WorkerRunner` -> `worker_threads` or Web Workers

## Data and utilities

- `Headers`, `Cookies`, `Multipart`, and `Etag` -> manual parsing or third-party middleware
- `Url` and `UrlParams` -> `URL` and `URLSearchParams`
- `Ndjson` and `MsgPack` -> ad-hoc codecs or third-party libraries
