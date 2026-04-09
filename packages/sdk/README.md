# `@open-sandbox/sdk`

TypeScript SDK for talking to the Open Sandbox server API (`/api/*`) with typed request/response schemas and Effect-based transport.

## In this monorepo

This package lives at `packages/sdk` and is consumed through Bun workspaces.

- Package name: `@open-sandbox/sdk`
- Build output: `packages/sdk/dist`
- From another workspace package, depend on it with:

```json
{
  "dependencies": {
    "@open-sandbox/sdk": "workspace:*"
  }
}
```

## Authentication modes

The SDK supports three auth strategies:

- **Session auth** (`sessionAuth()`): cookie-based auth (`credentials: "include"`)
- **Bearer auth** (`bearerAuth(token)`): sends `Authorization: Bearer <token>`
- **API key auth** (`apiKeyAuth(key)`): sends `X-API-Key: <key>`

If you do not pass `auth`, the runtime defaults to `sessionAuth()`.

## Quick start: convenience client

```ts
import { createClient } from "@open-sandbox/sdk";

const client = createClient({
  config: { baseUrl: "http://localhost:8080" },
  // auth omitted -> sessionAuth() by default
});

const sandboxes = await client.run(client.api.listSandboxes());
console.log(sandboxes.length);
```

## Create and run a runtime directly

```ts
import { Api, createSdkRuntime, runSdkEffect, sessionAuth } from "@open-sandbox/sdk";

const runtime = createSdkRuntime({
  config: { baseUrl: "http://localhost:8080" },
  auth: sessionAuth()
});

const sandboxes = await runSdkEffect(runtime, Api.listSandboxes());
```

## Bearer token auth example

```ts
import { createClient, bearerAuth } from "@open-sandbox/sdk";

const requireEnv = (name: string): string => {
  const value = process.env[name];
  if (!value) throw new Error(`Missing required env var: ${name}`);
  return value;
};

const client = createClient({
  config: { baseUrl: "https://sandbox.example.com" },
  auth: bearerAuth(requireEnv("SANDBOX_TOKEN"))
});

const sandboxes = await client.run(client.api.listSandboxes());
```

## API key auth example

```ts
import { createClient, apiKeyAuth } from "@open-sandbox/sdk";

const requireEnv = (name: string): string => {
  const value = process.env[name];
  if (!value) throw new Error(`Missing required env var: ${name}`);
  return value;
};

const client = createClient({
  config: { baseUrl: "https://sandbox.example.com" },
  auth: apiKeyAuth(requireEnv("SANDBOX_API_KEY"))
});

const keys = await client.run(client.api.listApiKeys());
```

## Common endpoint calls

```ts
import { bearerAuth, createClient } from "@open-sandbox/sdk";

const requireEnv = (name: string): string => {
  const value = process.env[name];
  if (!value) throw new Error(`Missing required env var: ${name}`);
  return value;
};

const client = createClient({
  config: { baseUrl: process.env.SANDBOX_BASE_URL ?? "http://localhost:8080" },
  auth: bearerAuth(requireEnv("SANDBOX_TOKEN"))
});

// list resources
const sandboxes = await client.run(client.api.listSandboxes());
const images = await client.run(client.api.listImages());

// create sandbox
const sandbox = await client.run(
  client.api.createSandbox({
    image: "node:20",
    name: "demo-sandbox"
  })
);

// run command inside sandbox
const exec = await client.run(
  client.api.execInSandbox(sandbox.id, {
    cmd: ["node", "-v"]
  })
);

console.log(exec.stdout);
```

## Stream/log/terminal URL helpers

For browser/EventSource/WebSocket usage, compose endpoint paths with helpers in `Api` and convert them to absolute URLs.

```ts
import {
  Api,
  resolveApiUrl,
  resolveWebSocketUrl,
  type SdkConfig
} from "@open-sandbox/sdk";

const config: SdkConfig = { baseUrl: "http://localhost:8080" };
const sandboxId = "sbx_123";

const logs = Api.resolveSandboxLogsSseUrl(sandboxId, { follow: true, tail: 200 });
const logsUrl = resolveApiUrl(config, logs.path, logs.query);

const terminal = Api.resolveSandboxTerminalWebSocketPath(sandboxId, { cols: 120, rows: 30 });
const terminalWsUrl = resolveWebSocketUrl(config, terminal.path, terminal.query);

const eventSource = new EventSource(logsUrl);
const terminalWs = new WebSocket(terminalWsUrl);
```

You can also stream build output through Effects using APIs like `buildImageStream(...)`.

## Runnable examples

Runnable examples live in `packages/sdk/examples`:

- `bun run --cwd packages/sdk example:bearer` (`SANDBOX_BASE_URL`, `SANDBOX_TOKEN`)
- `bun run --cwd packages/sdk example:api-key` (`SANDBOX_BASE_URL`, `SANDBOX_API_KEY`)
- `bun run --cwd packages/sdk example:api-key-management` (`SANDBOX_TOKEN`; optional `SANDBOX_BASE_URL`, `SANDBOX_NEW_API_KEY_NAME`) - demonstrates list/create/revoke, confirms revocation from the revoke response, and then lists remaining active keys (secret shown only at creation)
- `bun run --cwd packages/sdk example:resources` (`SANDBOX_TOKEN` or `SANDBOX_API_KEY`; optional `SANDBOX_BASE_URL`, defaults to `http://localhost:8080`)
- `bun run --cwd packages/sdk example:stream-urls`
