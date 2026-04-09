import { bearerAuth, createClient } from "../src/index";

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
	auth: bearerAuth(readEnv("SANDBOX_TOKEN"))
});

section("Bearer auth example");
console.log("Status: requesting sandboxes...");
const sandboxes = await client.run(client.api.listSandboxes());
console.log(`Status: success (${sandboxes.length} sandbox(es) fetched)`);
