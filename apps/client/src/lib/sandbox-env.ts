export const parseSandboxEnvEntry = (entry: string): { key: string; value: string } => {
	const separatorIndex = entry.indexOf("=");
	if (separatorIndex === -1) {
		return { key: entry, value: "" };
	}

	return {
		key: entry.slice(0, separatorIndex),
		value: entry.slice(separatorIndex + 1)
	};
};

export const serializeSandboxEnvEntry = (key: string, value: string): string => `${key.trim()}=${value}`;

export const normalizeSandboxEnv = (entries: string[] | undefined | null): string[] =>
	(entries ?? [])
		.map(parseSandboxEnvEntry)
		.map(({ key, value }) => ({ key: key.trim(), value }))
		.filter(({ key }) => key.length > 0)
		.map(({ key, value }) => serializeSandboxEnvEntry(key, value));

export const cloneSandboxEnv = (entries: string[] | undefined | null): string[] => [...normalizeSandboxEnv(entries)];

export const normalizeSandboxSecretEnv = (entries: string[] | undefined | null): string[] =>
	(entries ?? [])
		.map(parseSandboxEnvEntry)
		.map(({ key, value }) => ({ key: key.trim(), value }))
		.filter(({ key, value }) => key.length > 0 && value.length > 0)
		.map(({ key, value }) => serializeSandboxEnvEntry(key, value));

export const listSandboxEnvKeys = (entries: string[] | undefined | null): string[] =>
	normalizeSandboxEnv(entries).map((entry) => parseSandboxEnvEntry(entry).key);
