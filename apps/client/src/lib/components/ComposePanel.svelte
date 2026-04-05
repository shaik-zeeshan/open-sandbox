<script lang="ts">
	import { untrack } from "svelte";
	import { toast } from "$lib/toast.svelte";
	import CodeEditor from "./CodeEditor.svelte";
	import {
		composeDown,
		composeStatus,
		composeUpStream,
		formatApiFailure,
		runApiEffect,
		type ApiConfig,
		type ComposeRequest
	} from "$lib/api";

	type ComposeAction = "up" | "status" | "down";

	let {
		config
	} = $props<{
		config: ApiConfig;
	}>();

	let projectName = $state("");
	let composeContent = $state("");
	let selectedServices = $state<string[]>([]);
	let removeVolumes = $state(false);
	let removeOrphans = $state(true);
	let loading = $state(false);
	let step = $state("Idle");
	let logs = $state("");
	let statusServiceNames = $state<string[]>([]);
	let logsViewport = $state<HTMLPreElement | null>(null);

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

	function extractServiceNamesFromStatus(value: unknown): string[] {
		if (!Array.isArray(value)) {
			return [];
		}

		const names: string[] = [];
		for (const item of value) {
			if (typeof item !== "object" || item === null) {
				continue;
			}
			const record = item as Record<string, unknown>;
			const candidate = record["Service"] ?? record["service"] ?? record["Name"] ?? record["name"];
			if (typeof candidate === "string" && candidate.trim().length > 0) {
				names.push(candidate.trim());
			}
		}

		return Array.from(new Set(names));
	}

	const inferredServices = $derived(parseServiceNamesFromCompose(composeContent));
	const availableServices = $derived.by(() =>
		Array.from(new Set([...inferredServices, ...statusServiceNames]))
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

	function buildComposeRequest(): ComposeRequest {
		const request: ComposeRequest = {
			content: composeContent.trim(),
			project_name: projectName.trim() || undefined
		};

		if (selectedServices.length > 0) {
			request.services = selectedServices;
		}

		return request;
	}

	async function runComposeUp(): Promise<void> {
		loading = true;
		logs = "";
		step = "Preparing";

		try {
			const request = buildComposeRequest();
			if (!request.content) {
				throw new Error("docker-compose.yml content is required.");
			}
			if (!request.project_name) {
				throw new Error("Compose project name is required.");
			}

			step = "Running compose up";
			appendLog(`Starting docker compose up (project: ${request.project_name})...`);
			let composeError = "";

			const result = await composeUpStream(config, request, (event) => {
				if ((event.event === "stdout" || event.event === "stderr") && event.data.length > 0) {
					appendLog(event.data);
				}
				if (event.event === "error") {
					composeError = event.data.trim();
				}
			});

			if (result.stdout.trim().length > 0) {
				appendLog(result.stdout);
			}
			if (result.stderr.trim().length > 0) {
				appendLog(result.stderr);
			}

			if (composeError.length > 0) {
				throw new Error(composeError);
			}

			step = "Done";
			toast.ok("Compose project started.");
			appendLog("Compose up complete.");
		} catch (error) {
			toast.error(formatApiFailure(error));
			step = "Failed";
		} finally {
			loading = false;
		}
	}

	async function runComposeStatus(): Promise<void> {
		loading = true;
		step = "Fetching status";

		try {
			const request = buildComposeRequest();
			if (!request.content) {
				throw new Error("docker-compose.yml content is required.");
			}
			if (!request.project_name) {
				throw new Error("Compose project name is required.");
			}

			const result = await runApiEffect(composeStatus(config, request));
			statusServiceNames = extractServiceNamesFromStatus(result.services);
			appendLog(result.raw || JSON.stringify(result.services, null, 2));
			toast.ok("Compose status loaded.");
			step = "Done";
		} catch (error) {
			toast.error(formatApiFailure(error));
			step = "Failed";
		} finally {
			loading = false;
		}
	}

	async function runComposeDown(): Promise<void> {
		loading = true;
		step = "Running compose down";

		try {
			const request = buildComposeRequest();
			if (!request.content) {
				throw new Error("docker-compose.yml content is required.");
			}
			if (!request.project_name) {
				throw new Error("Compose project name is required.");
			}

			request.volumes = removeVolumes;
			request.remove_orphans = removeOrphans;

			const result = await runApiEffect(composeDown(config, request));
			if (result.stdout.trim().length > 0) {
				appendLog(result.stdout);
			}
			if (result.stderr.trim().length > 0) {
				appendLog(result.stderr);
			}
			toast.ok("Compose project stopped.");
			step = "Done";
		} catch (error) {
			toast.error(formatApiFailure(error));
			step = "Failed";
		} finally {
			loading = false;
		}
	}

	async function runAction(action: ComposeAction): Promise<void> {
		if (action === "up") {
			await runComposeUp();
			return;
		}
		if (action === "status") {
			await runComposeStatus();
			return;
		}
		await runComposeDown();
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
			<div class="compose-note">
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
				<span>Compose projects are stored under <code class="inline-code">.open-sandbox/compose</code> in the server workspace root.</span>
			</div>

			<label class="field-col">
				<span class="section-label">Project name</span>
				<input class="field" bind:value={projectName} placeholder="my-project" required />
			</label>

			<div class="field-col">
				<span class="section-label">docker-compose.yml</span>
				<CodeEditor
					bind:value={composeContent}
					language="yaml"
					placeholder="version: '3.8'&#10;services:&#10;  app:&#10;    image: ubuntu:24.04&#10;    command: sleep infinity"
					minHeight="16rem"
				/>
			</div>

			<div class="field-col">
				<span class="section-label">Services <span class="opt">(optional)</span></span>
				{#if availableServices.length === 0}
					<p class="field-help">No services detected yet. Add services to compose YAML or fetch status.</p>
				{:else}
					<div class="services-grid">
						{#each availableServices as service}
							<label class="service-chip">
								<input
									type="checkbox"
									checked={selectedServices.includes(service)}
									onchange={() => toggleService(service)}
								/>
								<span>{service}</span>
							</label>
						{/each}
					</div>
				{/if}
			</div>

			<div class="down-options">
				<label class="checkbox-row">
					<input type="checkbox" bind:checked={removeVolumes} />
					<span>Remove volumes on down</span>
				</label>
				<label class="checkbox-row">
					<input type="checkbox" bind:checked={removeOrphans} />
					<span>Remove orphan containers on down</span>
				</label>
			</div>

			<div class="compose-actions">
				<button class="btn-primary" type="button" onclick={() => void runAction("up")} disabled={loading}>Run up</button>
				<button class="btn-ghost" type="button" onclick={() => void runAction("status")} disabled={loading}>Status</button>
				<button class="btn-danger" type="button" onclick={() => void runAction("down")} disabled={loading}>Down</button>
			</div>

			<div class="pipeline-panel">
				<div class="pipeline-header">
					<span class="pipeline-title">Output</span>
					<span class="pipeline-step">{loading ? `${step}...` : step}</span>
				</div>
				<pre bind:this={logsViewport} class="pipeline-log">{stripAnsi(logs) || "Waiting for output..."}</pre>
			</div>
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

	.compose-note {
		display: flex;
		align-items: flex-start;
		gap: 0.5rem;
		padding: 0.625rem 0.75rem;
		background: var(--status-warn-bg);
		border: 1px solid var(--status-warn-border);
		border-radius: var(--radius-md);
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--status-warn);
		line-height: 1.5;
	}

	.compose-note svg {
		flex-shrink: 0;
		margin-top: 0.1rem;
		color: var(--status-warn);
	}

	.inline-code {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		padding: 0.05rem 0.3rem;
		color: var(--text-secondary);
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

	.service-chip {
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
	}

	.down-options {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.checkbox-row {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		font-family: var(--font-mono);
		font-size: 0.64rem;
		color: var(--text-secondary);
	}

	.compose-actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		flex-wrap: wrap;
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
</style>
