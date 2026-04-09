import { bearerAuth, createClient } from "../src/index";

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

const baseUrl = readEnv("SANDBOX_BASE_URL", "http://localhost:8080");
const createName = readEnv("SANDBOX_NEW_API_KEY_NAME", `sdk-example-${Date.now()}`);

section("API key management lifecycle");
console.log("Auth assumption: this flow uses SANDBOX_TOKEN (bearer auth) with API key management permissions.");
console.log("Security note: plaintext API key secret is returned only at create-time.");

const client = createClient({
	config: { baseUrl },
	auth: bearerAuth(readEnv("SANDBOX_TOKEN"))
});

section("1) List existing API keys");
const before = await client.run(client.api.listApiKeys());
console.log(`Status: found ${before.length} existing API key record(s)`);

section("2) Create a new API key");
const created = await client.run(client.api.createApiKey({ name: createName }));
console.log(`Status: created key id=${created.api_key.id} name=${created.api_key.name ?? "(unnamed)"}`);
console.log(`Plaintext secret (copy now, not returned again): ${created.secret}`);

section("3) List again (secret is not returned)");
const afterCreate = await client.run(client.api.listApiKeys());
const createdRecord = afterCreate.find((key) => key.id === created.api_key.id);
console.log(`Status: now ${afterCreate.length} API key record(s)`);
if (createdRecord) {
	console.log(
		`Created record preview: id=${createdRecord.id} preview=${createdRecord.preview ?? "(none)"} revoked_at=${createdRecord.revoked_at ?? "active"}`
	);
}

section("4) Revoke the created API key");
const revoked = await client.run(client.api.revokeApiKey(created.api_key.id));
console.log(`Status: revoked=${revoked.revoked} id=${revoked.id}`);

section("5) Confirm revoke response and list remaining active keys");
const afterRevoke = await client.run(client.api.listApiKeys());
console.log(`Revoke confirmation: revoked=${revoked.revoked} id=${revoked.id}`);
console.log(`Status: ${afterRevoke.length} active API key record(s) after revoke`);
