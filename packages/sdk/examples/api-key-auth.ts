import { apiKeyAuth, createClient } from "../src/index";

const section = (title: string) => {
	console.log(`\n=== ${title} ===`);
};

const readEnv = (name: string): string => {
	const processRef = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
	const value = processRef?.env?.[name]?.trim();
	if (!value) {
		throw new Error(`Missing required env var: ${name}`);
	}
	return value;
};

const client = createClient({
	config: { baseUrl: readEnv("SANDBOX_BASE_URL") },
	auth: apiKeyAuth(readEnv("SANDBOX_API_KEY"))
});

section("API key auth example");
console.log("Status: requesting API key metadata...");
const apiKeys = await client.run(client.api.listApiKeys());
console.log(`Status: success (${apiKeys.length} API key record(s) visible)`);
