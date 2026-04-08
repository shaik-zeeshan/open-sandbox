<script lang="ts">
	import { untrack } from "svelte";
	import * as yaml from "js-yaml";
	import CodeEditor from "./CodeEditor.svelte";
	import ProxyConfigEditor from "./ProxyConfigEditor.svelte";
	import Checkbox from "./Checkbox.svelte";
	import {
		formatApiFailure,
		getComposeProject,
		resolveApiUrl,
		runApiEffect,
		type ApiConfig,
		type ComposeProjectPreview,
		type ComposeRequest,
		type SandboxPortProxyConfig
	} from "$lib/api";
	import { runComposeAction, type ComposeAction } from "$lib/compose-panel-runtime";
	import { toast } from "$lib/toast.svelte";

	let {
		config,
		initialProjectName = ""
	} = $props<{
		config: ApiConfig;
		initialProjectName?: string;
	}>();

	let projectName = $state("");
	let composeContent = $state("");
	let selectedServices = $state<string[]>([]);
	let removeVolumes = $state(false);
	let removeOrphans = $state(true);
	let activeAction = $state<"up" | "status" | "down" | null>(null);
	let projectNameError = $state("");
	let composeContentError = $state("");
	let previewRefreshLoading = $state(false);
	const loading = $derived(activeAction !== null);
	let step = $state("Idle");
	let logs = $state("");
	let statusServiceNames = $state<string[]>([]);
	let composeProjectPreview = $state<ComposeProjectPreview | null>(null);
	let logsViewport = $state<HTMLPreElement | null>(null);

	// Per-service proxy config (keyed by service name)
	let serviceProxyConfigs = $state<Record<string, SandboxPortProxyConfig | null>>({});

	const stripAnsi = (value: string): string => value.replace(/\x1b\[[0-9;]*[mGKHF]/g, "");

	function appendLog(output: string): void {
		const normalized = output.replace(/\r\n/g, "\n").trim();
		if (normalized.length === 0) {
			return;
		}
		logs = logs.length > 0 ? `${logs}\n${normalized}` : normalized;
	}

	function parseServiceNamesFromCompose(content: string): string[] {
		const lines = content.split("\n");
		const names: string[] = [];
		let inServices = false;

		for (const line of lines) {
			const trimmed = line.trimEnd();
			if (!inServices) {
				if (/^services:\s*$/.test(trimmed)) {
					inServices = true;
				}
				continue;
			}

			if (/^[^\s#].*:/.test(trimmed)) {
				break;
			}

			const match = /^\s{2}([a-zA-Z0-9._-]+):\s*$/.exec(line);
			if (match && match[1]) {
				names.push(match[1]);
			}
		}

		return Array.from(new Set(names));
	}

	const inferredServices = $derived(parseServiceNamesFromCompose(composeContent));
	const availableServices = $derived.by(() =>
		Array.from(new Set([...inferredServices, ...statusServiceNames]))
	);
	const previewServices = $derived.by(() =>
		(composeProjectPreview?.services ?? []).map((service) => ({
			...service,
			ports: service.ports
				.filter((port) => port.preview_url.trim().length > 0)
				.map((port) => ({
					...port,
					preview_url: resolveApiUrl(config, port.preview_url)
				}))
		}))
	);

	$effect(() => {
		const availableSet = new Set(availableServices);
		const currentSelected = untrack(() => selectedServices);
		const nextSelected = currentSelected.filter((name) => availableSet.has(name));
		if (
			nextSelected.length === currentSelected.length &&
			nextSelected.every((name, index) => name === currentSelected[index])
		) {
			return;
		}
		selectedServices = nextSelected;
	});

	$effect(() => {
		projectName = initialProjectName;
	});

	$effect(() => {
		projectName;
		if (projectNameError && projectName.length > 0) {
			projectNameError = "";
		}
	});

	$effect(() => {
		composeContent;
		if (composeContentError && composeContent.length > 0) {
			composeContentError = "";
		}
	});

	$effect(() => {
		logs;
		if (!logsViewport) return;
		queueMicrotask(() => {
			if (logsViewport) {
				logsViewport.scrollTop = logsViewport.scrollHeight;
			}
		});
	});

	function toggleService(service: string): void {
		if (selectedServices.includes(service)) {
			selectedServices = selectedServices.filter((name) => name !== service);
			return;
		}
		selectedServices = [...selectedServices, service];
	}

	/**
	 * Build a plain object representing the proxy config, suitable for
	 * structured YAML serialization. Only defined, non-empty fields are included.
	 */
	function buildProxyConfigObject(cfg: SandboxPortProxyConfig): Record<string, unknown> | null {
		const proxy: Record<string, unknown> = {};

		if (cfg.request_headers && Object.keys(cfg.request_headers).length > 0) {
			proxy.request_headers = { ...cfg.request_headers };
		}
		if (cfg.response_headers && Object.keys(cfg.response_headers).length > 0) {
			proxy.response_headers = { ...cfg.response_headers };
		}
		if (cfg.path_prefix_strip) {
			proxy.path_prefix_strip = cfg.path_prefix_strip;
		}
		if (cfg.skip_auth) {
			proxy.skip_auth = true;
		}
		if (cfg.cors) {
			const cors: Record<string, unknown> = {};
			if (cfg.cors.allow_origins && cfg.cors.allow_origins.length > 0) {
				cors.allow_origins = [...cfg.cors.allow_origins];
			}
			if (cfg.cors.allow_methods && cfg.cors.allow_methods.length > 0) {
				cors.allow_methods = [...cfg.cors.allow_methods];
			}
			if (cfg.cors.allow_headers && cfg.cors.allow_headers.length > 0) {
				cors.allow_headers = [...cfg.cors.allow_headers];
			}
			if (cfg.cors.allow_credentials) {
				cors.allow_credentials = true;
			}
			if (cfg.cors.max_age && cfg.cors.max_age > 0) {
				cors.max_age = cfg.cors.max_age;
			}
			if (Object.keys(cors).length > 0) {
				proxy.cors = cors;
			}
		}

		return Object.keys(proxy).length > 0 ? proxy : null;
	}

	/**
	 * Inject or remove x-open-sandbox.proxy extensions in compose YAML content using
	 * structured YAML parse/modify/serialize. This avoids raw string interpolation
	 * and brittle line-surgery. Falls back to original content if YAML cannot be
	 * parsed (e.g. content is empty or malformed).
	 *
	 * - Apply (cfg !== null): merges `proxy` into any existing `x-open-sandbox`
	 *   object so unrelated sibling fields are preserved.
	 * - Remove (cfg === null): deletes only the `proxy` key; removes
	 *   `x-open-sandbox` only when it becomes empty.
	 *
	 * Limitation: js-yaml round-trips lose YAML comments and anchors; this is an
	 * inherent constraint of the parse/dump approach and cannot be avoided without
	 * a comment-preserving YAML library.
	 */
	function injectProxyConfigsIntoYaml(
		content: string,
		configs: Record<string, SandboxPortProxyConfig | null>
	): string {
		if (Object.keys(configs).length === 0) return content;

		let doc: unknown;
		try {
			doc = yaml.load(content);
		} catch {
			// Unparseable YAML — return as-is, the server will reject it anyway
			return content;
		}

		if (doc === null || typeof doc !== "object" || Array.isArray(doc)) {
			return content;
		}

		const root = doc as Record<string, unknown>;

		// Ensure services is an object
		if (typeof root.services !== "object" || root.services === null || Array.isArray(root.services)) {
			return content;
		}

		const services = root.services as Record<string, unknown>;

		for (const [serviceName, cfg] of Object.entries(configs)) {
			const svc = services[serviceName];
			const isSvcObject = svc !== null && typeof svc === "object" && !Array.isArray(svc);

			if (cfg === null) {
				// Removal: only delete the `proxy` key; delete `x-open-sandbox` only
				// if no other sibling keys remain.
				if (!isSvcObject) continue;
				const svcRecord = svc as Record<string, unknown>;
				const existing = svcRecord["x-open-sandbox"];
				if (existing !== null && typeof existing === "object" && !Array.isArray(existing)) {
					const xos = existing as Record<string, unknown>;
					delete xos["proxy"];
					if (Object.keys(xos).length === 0) {
						delete svcRecord["x-open-sandbox"];
					}
				}
				continue;
			}

			const proxyObj = buildProxyConfigObject(cfg);
			if (proxyObj === null) continue;

			if (!(serviceName in services)) continue;

			// Coerce null/missing service definition to an object
			if (services[serviceName] === null || services[serviceName] === undefined) {
				services[serviceName] = {};
			}

			const svcAfterCoerce = services[serviceName];
			if (typeof svcAfterCoerce !== "object" || Array.isArray(svcAfterCoerce)) continue;

			const svcRecord = svcAfterCoerce as Record<string, unknown>;

			// Merge: preserve existing x-open-sandbox sibling fields, only set proxy.
			const existingXos = svcRecord["x-open-sandbox"];
			const mergedXos: Record<string, unknown> =
				existingXos !== null && typeof existingXos === "object" && !Array.isArray(existingXos)
					? { ...(existingXos as Record<string, unknown>) }
					: {};
			mergedXos["proxy"] = proxyObj;
			svcRecord["x-open-sandbox"] = mergedXos;
		}

		try {
			return yaml.dump(root, {
				indent: 2,
				lineWidth: -1,
				noRefs: true,
				quotingType: '"',
				forceQuotes: false
			});
		} catch {
			return content;
		}
	}

	function buildComposeRequest(): ComposeRequest {
		// Pass all configs including null entries so removals are also processed.
		const contentWithProxy = Object.keys(serviceProxyConfigs).length > 0
			? injectProxyConfigsIntoYaml(composeContent.trim(), serviceProxyConfigs)
			: composeContent.trim();

		const request: ComposeRequest = {
			content: contentWithProxy,
			project_name: projectName.trim() || undefined
		};

		if (selectedServices.length > 0) {
			request.services = selectedServices;
		}

		return request;
	}

	function validateForm(): boolean {
		const normalizedProjectName = projectName.trim();
		const normalizedComposeContent = composeContent.trim();
		const nextProjectNameError =
			normalizedProjectName.length === 0 ? "Enter a project name before running a compose action." : "";
		const nextComposeContentError =
			normalizedComposeContent.length === 0 ? "Paste compose YAML before running a compose action." : "";

		projectNameError = nextProjectNameError;
		composeContentError = nextComposeContentError;

		if (nextProjectNameError) {
			toast.error(nextProjectNameError);
		}

		if (nextComposeContentError) {
			toast.error(nextComposeContentError);
		}

		return nextProjectNameError.length === 0 && nextComposeContentError.length === 0;
	}

	async function runAction(action: ComposeAction): Promise<void> {
		if (!validateForm()) {
			return;
		}

		activeAction = action;
		await runComposeAction({
			action,
			config,
			request: buildComposeRequest(),
			removeVolumes,
			removeOrphans,
			runtime: {
				setLoading: (value) => {
					if (!value) {
						activeAction = null;
					}
				},
				setStep: (value) => {
					step = value;
				},
				setLogs: (value) => {
					logs = value;
				},
				appendLog,
				setStatusServiceNames: (value) => {
					statusServiceNames = value;
				},
				setComposeProjectPreview: (value) => {
					composeProjectPreview = value;
				}
			}
		});
	}

	async function refreshProjectPreviews(): Promise<void> {
		const normalizedProject = projectName.trim();
		if (normalizedProject.length === 0) {
			toast.error("Enter a project name to refresh service previews.");
			return;
		}

		previewRefreshLoading = true;
		try {
			composeProjectPreview = await runApiEffect(getComposeProject(config, normalizedProject));
			toast.ok("Service previews refreshed.");
		} catch (error) {
			toast.error(formatApiFailure(error));
		} finally {
			previewRefreshLoading = false;
		}
	}
</script>

<div class="compose-page anim-fade-up">
	<div class="compose-header">
		<div>
			<p class="section-label">Runtime</p>
			<h1 class="compose-title">Compose</h1>
		</div>
	</div>

	<section class="panel">
		<div class="panel-header">
			<span class="panel-title">Manage compose project</span>
		</div>
		<div class="panel-body compose-body">
			<label class="field-col">
				<span class="section-label" title="Compose projects are stored under .open-sandbox/compose in the server workspace root.">Project name</span>
				<input
					class="field"
					class:field--error={projectNameError.length > 0}
					bind:value={projectName}
					placeholder="my-project"
					required
				/>
				{#if projectNameError}
					<span class="field-error-text">{projectNameError}</span>
				{/if}
			</label>

			<div class="field-col">
				<span class="section-label">docker-compose.yml</span>
				<CodeEditor
					bind:value={composeContent}
					language="yaml"
					placeholder="version: '3.8'&#10;services:&#10;  app:&#10;    image: ubuntu:24.04&#10;    command: sleep infinity"
					minHeight="16rem"
				/>
				{#if composeContentError}
					<span class="field-error-text">{composeContentError}</span>
				{/if}
			</div>

			<div class="field-col">
				<span class="section-label">Services <span class="opt">(optional)</span></span>
				{#if availableServices.length === 0}
					<p class="field-help">No services detected yet. Add services to compose YAML or fetch status.</p>
				{:else}
					<div class="services-grid">
						{#each availableServices as service}
							<Checkbox
								checked={selectedServices.includes(service)}
								onchange={() => toggleService(service)}
								label={service}
								labelClass="service-chip{selectedServices.includes(service) ? ' service-chip--checked' : ''}"
							/>
						{/each}
					</div>
				{/if}
			</div>

			{#if inferredServices.length > 0}
				<div class="field-col">
					<span class="section-label">Proxy settings <span class="opt">(optional, per service)</span></span>
					<div class="proxy-services-list">
						{#each inferredServices as service}
							<details class="proxy-service-details">
								<summary class="proxy-service-summary">
									<svg width="9" height="9" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" class="proxy-service-chevron"><polyline points="9 18 15 12 9 6"/></svg>
									<code class="proxy-service-name">{service}</code>
									{#if serviceProxyConfigs[service] != null}
										<span class="proxy-service-badge">configured</span>
									{/if}
								</summary>
								<div class="proxy-service-body">
									<ProxyConfigEditor
										value={serviceProxyConfigs[service] ?? null}
										onchange={(v: SandboxPortProxyConfig | null) => {
											serviceProxyConfigs = { ...serviceProxyConfigs, [service]: v };
										}}
									/>
								</div>
							</details>
						{/each}
					</div>
				</div>
			{/if}

			<div class="compose-actions">
				<Checkbox bind:checked={removeVolumes} label="Remove volumes" labelClass="checkbox-row" />
				<Checkbox bind:checked={removeOrphans} label="Remove orphans" labelClass="checkbox-row" />
				<div class="compose-actions-spacer"></div>
				<button class="btn-primary" type="button" onclick={() => void runAction("up")} disabled={loading}>
					{#if activeAction === "up"}
						<svg class="btn-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="12" height="12"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
						Starting...
					{:else}
						Run up
					{/if}
				</button>
				<button class="btn-ghost" type="button" onclick={() => void runAction("status")} disabled={loading}>
					{#if activeAction === "status"}
						<svg class="btn-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="12" height="12"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
						Checking...
					{:else}
						Status
					{/if}
				</button>
				<button class="btn-danger" type="button" onclick={() => void runAction("down")} disabled={loading}>
					{#if activeAction === "down"}
						<svg class="btn-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="12" height="12"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
						Stopping...
					{:else}
						Down
					{/if}
				</button>
			</div>

			{#if step !== "Idle" || logs}
				<div class="pipeline-panel">
					<div class="pipeline-header">
						<span class="pipeline-title">Output</span>
						<span class="pipeline-step">{loading ? `${step}...` : step}</span>
					</div>
					<pre bind:this={logsViewport} class="pipeline-log">{stripAnsi(logs) || "Waiting for output..."}</pre>
				</div>
			{/if}

			{#if previewServices.length > 0 || composeProjectPreview}
			<div class="pipeline-panel">
				<div class="pipeline-header">
					<span class="pipeline-title">Service previews</span>
					<div class="preview-header-actions">
						{#if composeProjectPreview}
							<span class="pipeline-step">{composeProjectPreview.project_name}</span>
						{/if}
						<button class="btn-ghost btn-xs" type="button" onclick={() => void refreshProjectPreviews()} disabled={loading || previewRefreshLoading}>
							{previewRefreshLoading ? "Refreshing..." : "Refresh previews"}
						</button>
					</div>
				</div>
				{#if previewServices.length === 0}
					<p class="field-help compose-preview-empty">No published preview ports yet. Run up or status after exposing service ports.</p>
				{:else}
					<div class="compose-preview-grid">
						{#each previewServices as service}
							<section class="preview-service">
								<div class="preview-service-header">
									<span class="preview-service-name">{service.service_name}</span>
								</div>
								{#if service.ports.length === 0}
									<span class="field-help">No published ports</span>
								{:else}
									<div class="port-chips">
										{#each service.ports as port}
											<a class="port-chip" href={port.preview_url} target="_blank" rel="noreferrer" title={port.preview_url}>
												:{port.private_port} -> {port.public_port} ({port.type})
											</a>
										{/each}
									</div>
								{/if}
							</section>
						{/each}
					</div>
				{/if}
			</div>
			{/if}
		</div>
	</section>
</div>

<style>
	.compose-page {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		padding: 1.5rem;
	}

	.compose-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding-bottom: 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.compose-title {
		margin: 0.2rem 0 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-style: italic;
		font-weight: 400;
		color: var(--text-primary);
	}

	.compose-body {
		display: flex;
		flex-direction: column;
		gap: 0.85rem;
	}

	.field-col {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.field-help {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		line-height: 1.45;
	}

	.field--error {
		border-color: var(--status-error-border);
	}

	.field-error-text {
		color: var(--status-error);
		font-family: var(--font-mono);
		font-size: 0.6rem;
	}

	.opt {
		font-size: 0.58rem;
		color: var(--text-muted);
	}

	.services-grid {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 0.4rem;
	}

	:global(.service-chip) {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		padding: 0.22rem 0.5rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		background: var(--bg-raised);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-secondary);
		transition: border-color 0.12s, background 0.12s, color 0.12s;
	}

	:global(.service-chip:hover) {
		border-color: var(--border-hi);
		color: var(--text-primary);
	}

	:global(.service-chip--checked) {
		border-color: var(--border-focus);
		color: var(--text-primary);
		background: var(--bg-overlay);
	}

	:global(.checkbox-row) {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
	}

	.compose-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.compose-actions-spacer {
		flex: 1;
	}

	.compose-actions :global(button) {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
	}

	.btn-spinner {
		flex-shrink: 0;
		animation: spin 0.75s linear infinite;
	}

	@keyframes spin {
		from { transform: rotate(0deg); }
		to   { transform: rotate(360deg); }
	}

	.pipeline-panel {
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		overflow: hidden;
	}

	.pipeline-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		padding: 0.55rem 0.65rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.pipeline-title,
	.pipeline-step {
		font-family: var(--font-mono);
		font-size: 0.62rem;
	}

	.pipeline-step {
		color: var(--text-secondary);
	}

	.preview-header-actions {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
	}

	.pipeline-log {
		margin: 0;
		padding: 0.65rem;
		min-height: 8rem;
		max-height: 14rem;
		overflow: auto;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		line-height: 1.5;
		color: var(--text-secondary);
		background: #050505;
		white-space: pre-wrap;
	}

	.compose-preview-empty {
		padding: 0.65rem;
	}

	.compose-preview-grid {
		display: grid;
		gap: 0.5rem;
		padding: 0.65rem;
	}

	.preview-service {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
		padding: 0.55rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-md);
		background: var(--bg-surface);
	}

	.preview-service-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}

	.preview-service-name {
		font-family: var(--font-mono);
		font-size: 0.64rem;
		color: var(--text-primary);
	}

	.port-chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.4rem;
	}

	.port-chip {
		display: inline-flex;
		align-items: center;
		padding: 0.2rem 0.42rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-secondary);
		text-decoration: none;
		transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
	}

	.port-chip:hover {
		border-color: var(--border-strong);
		color: var(--text-primary);
		background: var(--bg-raised);
	}

	/* Proxy settings per-service */
	.proxy-services-list {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.proxy-service-details {
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.proxy-service-summary {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.5rem 0.65rem;
		cursor: pointer;
		list-style: none;
		user-select: none;
		background: var(--bg-raised);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-secondary);
		transition: background 0.1s;
	}

	.proxy-service-summary::-webkit-details-marker { display: none; }

	.proxy-service-summary:hover {
		background: var(--bg-overlay);
	}

	.proxy-service-chevron {
		flex-shrink: 0;
		transition: transform 0.15s;
	}

	.proxy-service-details[open] .proxy-service-chevron {
		transform: rotate(90deg);
	}

	.proxy-service-name {
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-primary);
	}

	.proxy-service-badge {
		margin-left: auto;
		font-family: var(--font-mono);
		font-size: 0.58rem;
		color: var(--text-muted);
		background: var(--accent-dim);
		border: 1px solid var(--border-mid);
		border-radius: 999px;
		padding: 0.08rem 0.4rem;
	}

	.proxy-service-body {
		padding: 0.65rem;
		background: var(--bg-surface);
		border-top: 1px solid var(--border-dim);
	}
</style>
