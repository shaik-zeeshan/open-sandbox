import type { ContainerSummary, PortSummary, SandboxPortProxyConfig, Sandbox } from "$lib/api";
import { cloneSandboxEnv } from "$lib/sandbox-env";
import type { PendingDuplicateCreateDraft } from "$lib/stores.svelte";

const cloneProxyConfig = (
	value: Record<string, SandboxPortProxyConfig> | undefined
): Record<string, SandboxPortProxyConfig> => {
	if (!value) {
		return {};
	}

	return JSON.parse(JSON.stringify(value)) as Record<string, SandboxPortProxyConfig>;
};

const normalizeName = (value: string): string => value.replace(/^\/+/, "").trim();

const suggestDuplicateName = (value: string): string => {
	const baseName = normalizeName(value) || "sandbox";
	const match = baseName.match(/^(.*?)(?: copy(?: (\d+))?)$/i);
	if (!match) {
		return `${baseName} copy`;
	}

	const duplicateBase = match[1]?.trim() || baseName;
	const duplicateNumber = Number(match[2] ?? "1");
	return `${duplicateBase} copy ${duplicateNumber + 1}`;
};

const formatContainerOnlyPorts = (ports: PortSummary[] | undefined): string =>
	(ports ?? [])
		.filter((port) => Number.isFinite(port.private) && port.private > 0)
		.map((port) => `${port.private}`)
		.join("\n");

const formatDuplicatePorts = (portSpecs: string[] | undefined, ports: PortSummary[] | undefined): string => {
	if (portSpecs && portSpecs.length > 0) {
		return portSpecs.join("\n");
	}
	return formatContainerOnlyPorts(ports);
};

export const buildSandboxDuplicateDraft = (sandbox: Sandbox): PendingDuplicateCreateDraft => ({
	name: suggestDuplicateName(sandbox.name),
	image: sandbox.image,
	repoUrl: sandbox.repo_url?.trim() ?? "",
	branch: "",
	workdir: sandbox.workspace_dir?.trim() ?? "",
	env: cloneSandboxEnv(sandbox.env),
	secretEnvKeys: [...(sandbox.secret_env_keys ?? [])],
	ports: formatDuplicatePorts(sandbox.port_specs, sandbox.ports),
	proxyConfig: cloneProxyConfig(sandbox.proxy_config)
});

export const buildContainerDuplicateDraft = (container: ContainerSummary): PendingDuplicateCreateDraft => ({
	name: suggestDuplicateName(container.names[0] ?? container.service_name ?? container.id.slice(0, 12)),
	image: container.image,
	repoUrl: "",
	branch: "",
	workdir: "",
	env: [],
	secretEnvKeys: [],
	ports: formatDuplicatePorts(container.port_specs, container.ports),
	proxyConfig: {}
});
