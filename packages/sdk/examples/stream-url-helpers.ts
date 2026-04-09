import { Api, resolveApiUrl, resolveWebSocketUrl, type SdkConfig } from "../src/index";

const section = (title: string) => {
	console.log(`\n=== ${title} ===`);
};

const readEnv = (name: string, fallback?: string): string => {
	const processRef = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
	const value = processRef?.env?.[name]?.trim();
	if (value) {
		return value;
	}
	if (fallback) {
		return fallback;
	}
	throw new Error(`Missing required env var: ${name}`);
};

const config: SdkConfig = {
	baseUrl: readEnv("SANDBOX_BASE_URL", "http://localhost:8080")
};

const sandboxId = readEnv("SANDBOX_ID", "sandbox_example");

const logs = Api.resolveSandboxLogsSseUrl(sandboxId, { follow: true, tail: 200 });
const terminal = Api.resolveSandboxTerminalWebSocketPath(sandboxId, { cols: 120, rows: 40 });

section("Stream URL helper example");
console.log(`Base URL: ${config.baseUrl}`);
console.log(`Sandbox ID: ${sandboxId}`);
console.log("Status: resolved URLs");
console.log(`Logs SSE URL: ${resolveApiUrl(config, logs.path, logs.query)}`);
console.log(`Terminal WebSocket URL: ${resolveWebSocketUrl(config, terminal.path, terminal.query)}`);
