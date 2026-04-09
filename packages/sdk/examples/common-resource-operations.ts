import { apiKeyAuth, bearerAuth, createClient } from "../src/index";

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

const readOptionalEnv = (name: string): string | undefined => {
	const processRef = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
	return processRef?.env?.[name]?.trim();
};

const token = readOptionalEnv("SANDBOX_TOKEN");
const apiKey = readOptionalEnv("SANDBOX_API_KEY");

if (!token && !apiKey) {
	throw new Error("Missing auth env var: set SANDBOX_TOKEN or SANDBOX_API_KEY");
}

const auth = token ? bearerAuth(token) : apiKeyAuth(apiKey!);
const authMode = token ? "bearer token" : "API key";

const client = createClient({
	config: { baseUrl: readEnv("SANDBOX_BASE_URL", "http://localhost:8080") },
	auth
});

section("Common resource operations");
console.log(`Auth: ${authMode}`);
console.log("Status: requesting sandboxes and images...");
const [sandboxes, images] = await Promise.all([
	client.run(client.api.listSandboxes()),
	client.run(client.api.listImages())
]);

console.log("Status: success");
console.log(`Summary: ${sandboxes.length} sandbox(es), ${images.length} image(s)`);
