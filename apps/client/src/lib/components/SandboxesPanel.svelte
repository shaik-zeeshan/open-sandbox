<script lang="ts">
	import SandboxCard from "./SandboxCard.svelte";
	import Combobox from "./Combobox.svelte";
	import PortsEditor from "./PortsEditor.svelte";
	import type { ContainerSummary, ImageSummary, PortSummary, Sandbox } from "$lib/api";

	let {
		sandboxes,
		containers,
		images,
		loading,
		errorMessage,
		notice,
		onOpen,
		onRestart,
		onReset,
		onResetContainer,
		onStop,
		onDelete,
		onOpenContainer,
		onRestartContainer,
		onStopContainer,
		onRemoveContainer,
		onRefresh,
		composeHref = "/compose",
		showCreateForm,
		createName = $bindable(),
		createExistingImage = $bindable(),
		createRepoUrl = $bindable(),
		createBranch = $bindable(),
		createWorkdir = $bindable(),
		createPorts = $bindable(),
		createLoading,
		createImageHref = "/images",
		onToggleCreate,
		onCreateSubmit,
		onApplyPreset
	} = $props<{
		sandboxes: Sandbox[];
		containers: ContainerSummary[];
		images: ImageSummary[];
		loading: boolean;
		errorMessage: string;
		notice: string;
		onOpen: (id: string) => void;
		onRestart: (id: string) => void;
		onReset: (id: string) => void;
		onResetContainer: (id: string) => void;
		onStop: (id: string) => void;
		onDelete: (id: string) => void;
		onOpenContainer: (id: string) => void;
		onRestartContainer: (id: string) => void;
		onStopContainer: (id: string) => void;
		onRemoveContainer: (id: string) => void;
		onRefresh: () => void;
		composeHref?: string;
		showCreateForm: boolean;
		createName: string;
		createExistingImage: string;
		createRepoUrl: string;
		createBranch: string;
		createWorkdir: string;
		createPorts: string;
		createLoading: boolean;
		createImageHref?: string;
		onToggleCreate: () => void;
		onCreateSubmit: () => void;
		onApplyPreset: (name: string, image: string) => void;
	}>();

	const presets = [
		{ label: "Ubuntu", name: "ubuntu-workspace", image: "ubuntu:24.04" },
		{ label: "Node",   name: "node-workspace",   image: "node:22"       },
		{ label: "Python", name: "python-workspace",  image: "python:3.12"  },
		{ label: "Go",     name: "go-workspace",      image: "golang:1.24"  }
	] as const;

	// Build combobox options from local images
	const localImageOptions = $derived(
		Array.from(
			new Set(images.flatMap((img: ImageSummary) => img.repo_tags.filter((t: string) => t !== "<none>:<none>"))) as Set<string>
		).map((tag: string) => ({ value: tag, label: tag }))
	);
	let workloadSearch = $state("");
	const sandboxContainerIDs = $derived(new Set(sandboxes.map((sandbox: Sandbox) => sandbox.id)));
	const runtimeContainers = $derived(containers.filter((container: ContainerSummary) => !sandboxContainerIDs.has(container.id)));

	type WorkloadCardItem = {
		key: string;
		kind: "sandbox" | "container";
		id: string;
		name: string;
		image: string;
		status: string;
		containerId: string;
		ports: PortSummary[];
		createdAt: number | null;
		metaLabel: string;
		metaValue: string;
		canReset: boolean;
		searchText: string;
	};

	const workloads = $derived.by<WorkloadCardItem[]>(() => {
		const sandboxItems = sandboxes.map((sandbox: Sandbox) => ({
			key: `sandbox:${sandbox.id}`,
			kind: "sandbox" as const,
			id: sandbox.id,
			name: sandbox.name,
			image: sandbox.image,
			status: sandbox.status,
			containerId: sandbox.container_id,
			ports: containers.find((c: ContainerSummary) => c.id === sandbox.id)?.ports ?? sandbox.ports ?? [],
			createdAt: sandbox.created_at,
			metaLabel: sandbox.owner_username ? "Owner" : "",
			metaValue: sandbox.owner_username ?? "",
			canReset: true,
			searchText: [sandbox.name, sandbox.image, sandbox.owner_username ?? "", sandbox.id, sandbox.container_id].join(" ").toLowerCase()
		}));

		const containerItems = runtimeContainers.map((container: ContainerSummary) => {
			const composeProject = container.project_name ?? "";
			const composeService = container.service_name ?? "";
			const primaryName = container.names[0] ?? container.id.slice(0, 12);
			const metaValue = composeProject ? `${composeProject}${composeService ? ` / ${composeService}` : ""}` : "Runtime container";
			return {
				key: `container:${container.id}`,
				kind: "container" as const,
				id: container.id,
				name: primaryName,
				image: container.image,
				status: container.status,
				containerId: container.container_id,
				ports: container.ports ?? [],
				createdAt: container.created ?? null,
				metaLabel: composeProject ? "Compose" : "Type",
				metaValue,
				canReset: container.resettable,
				searchText: [primaryName, container.image, metaValue, container.id, container.container_id, ...(container.names ?? [])].join(" ").toLowerCase()
			};
		});

		return [...sandboxItems, ...containerItems].sort((a, b) => {
			const createdDiff = (b.createdAt ?? 0) - (a.createdAt ?? 0);
			if (createdDiff !== 0) {
				return createdDiff;
			}
			return a.name.localeCompare(b.name);
		});
	});

	const filteredWorkloads = $derived.by(() => {
		const query = workloadSearch.trim().toLowerCase();
		if (query.length === 0) {
			return workloads;
		}
		return workloads.filter((item) => item.searchText.includes(query));
	});

	const workloadCount = $derived(workloads.length);
</script>

<div class="list-view anim-fade-up">
	<!-- Page header -->
	<div class="list-header">
		<div class="list-title-group">
			<h1 class="list-title">Workloads</h1>
			{#if workloadCount > 0}
				<span class="list-count">{filteredWorkloads.length === workloadCount ? workloadCount : `${filteredWorkloads.length}/${workloadCount}`}</span>
			{/if}
		</div>
		<div class="list-actions">
			<label class="search-field" aria-label="Search workloads">
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
				<input class="search-input" bind:value={workloadSearch} placeholder="Search workloads..." />
			</label>
			<button class="btn-ghost btn-sm" type="button" onclick={onRefresh} disabled={loading}>
				{#if loading}
					<svg class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{:else}
					<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
				{/if}
				Refresh
			</button>
			<a class="btn-ghost btn-sm" href={composeHref}>Compose</a>
			<button class="btn-primary btn-sm" type="button" onclick={onToggleCreate}>
				{showCreateForm ? "Cancel" : "+ New sandbox"}
			</button>
		</div>
	</div>

	<!-- Alerts -->
	{#if errorMessage}<p class="alert-error anim-fade-up">{errorMessage}</p>{/if}
	{#if notice}<p class="alert-ok anim-fade-up">{notice}</p>{/if}

	<!-- Card grid -->
	{#if workloadCount === 0 && !showCreateForm}
		<div class="empty-state anim-fade-up anim-delay-1">
			<div class="empty-icon">
				<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>
				</svg>
			</div>
			<p class="empty-title">{loading ? "Loading..." : "No workloads yet"}</p>
			{#if !loading}
				<p class="empty-sub">Create a sandbox to get started.</p>
				<button class="btn-ghost btn-sm" type="button" onclick={onToggleCreate}>+ New sandbox</button>
			{/if}
		</div>
	{:else if filteredWorkloads.length === 0}
		<div class="empty-state anim-fade-up anim-delay-1">
			<div class="empty-icon">
				<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
			</div>
			<p class="empty-title">No workloads match</p>
			<p class="empty-sub">Try a different name, image, or workload id.</p>
		</div>
	{:else}
		<div class="sandbox-grid">
			{#each filteredWorkloads as workload, i (workload.key)}
				<div class="anim-fade-up" style="animation-delay: {i * 0.035}s">
					<SandboxCard
						name={workload.name}
						image={workload.image}
						status={workload.status}
						containerId={workload.containerId}
						ports={workload.ports}
						createdAt={workload.createdAt}
						metaLabel={workload.metaLabel}
						metaValue={workload.metaValue}
						isSelected={false}
						showReset={workload.canReset}
						deleteLabel={workload.kind === "sandbox" ? "Delete" : "Remove"}
						deleteTitle={workload.kind === "sandbox" ? "Delete sandbox" : "Remove container"}
						onOpen={() => workload.kind === "sandbox" ? onOpen(workload.id) : onOpenContainer(workload.id)}
						onRestart={() => workload.kind === "sandbox" ? onRestart(workload.id) : onRestartContainer(workload.id)}
						onReset={() => workload.kind === "sandbox" ? onReset(workload.id) : onResetContainer(workload.id)}
						onStop={() => workload.kind === "sandbox" ? onStop(workload.id) : onStopContainer(workload.id)}
						onDelete={() => workload.kind === "sandbox" ? onDelete(workload.id) : onRemoveContainer(workload.id)}
					/>
				</div>
			{/each}
		</div>
	{/if}
</div>

<!-- Slide-over backdrop -->
{#if showCreateForm}
	<div class="drawer-backdrop" role="none" onclick={onToggleCreate}></div>
{/if}

<!-- Slide-over drawer -->
<div class="drawer" class:drawer--open={showCreateForm} aria-hidden={!showCreateForm}>
	<div class="drawer-header">
		<div class="drawer-title-row">
			<h2 class="drawer-title">New sandbox</h2>
			<button class="drawer-close" type="button" onclick={onToggleCreate} aria-label="Close">
				<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
				</svg>
			</button>
		</div>
		<!-- Preset chips -->
		<div class="preset-chips">
			{#each presets as preset}
				<button class="preset-chip" type="button" onclick={() => onApplyPreset(preset.name, preset.image)}>
					{preset.label}
				</button>
			{/each}
		</div>
	</div>

	<div class="drawer-body">
		<fieldset class="create-fieldset" disabled={createLoading}>
			<div class="form-section">
				<label class="field-col">
					<span class="section-label">Sandbox name</span>
					<input class="field" bind:value={createName} placeholder="my-workspace" required />
				</label>
			</div>

			<div class="form-section">
				<span class="section-label">Image</span>
				<label class="field-col">
					<Combobox
						bind:value={createExistingImage}
						options={localImageOptions}
						placeholder="Search local images..."
						emptyText={localImageOptions.length === 0 ? "No local images available." : "No matches"}
					/>
					{#if localImageOptions.length === 0}
						<div class="create-image-empty">
							<span class="field-help">No local images found. Create or pull one from the Images route.</span>
							<a class="btn-ghost btn-xs" href={createImageHref}>Open Images</a>
						</div>
					{/if}
				</label>
			</div>

			<div class="form-section">
				<span class="section-label form-section-title">Runtime options <span class="opt">(optional)</span></span>
				<div class="form-row-2">
					<label class="field-col">
						<span class="section-label">Repo URL</span>
						<input class="field" bind:value={createRepoUrl} placeholder="https://github.com/org/repo.git" />
					</label>
					<label class="field-col">
						<span class="section-label">Branch</span>
						<input class="field" bind:value={createBranch} placeholder="main" />
					</label>
				</div>
				<label class="field-col">
					<span class="section-label">Workdir</span>
					<input class="field" bind:value={createWorkdir} />
					<span class="field-help">Leave empty to use the image <code class="inline-code">WORKDIR</code>. If the image does not define one, the sandbox keeps the container default working directory and skips the workspace volume.</span>
				</label>
				<div class="field-col">
					<span class="section-label">Port mappings <span class="opt">(host → container)</span></span>
					<PortsEditor bind:value={createPorts} />
				</div>
			</div>

		</fieldset>
	</div>

	<div class="drawer-footer">
		<button class="btn-ghost btn-sm" type="button" onclick={onToggleCreate} disabled={createLoading}>
			Cancel
		</button>
		<button class="btn-primary" type="button" onclick={onCreateSubmit} disabled={createLoading}>
			{#if createLoading}
				<svg class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				Creating...
			{:else}
				Create sandbox
			{/if}
		</button>
	</div>
</div>

<style>
	.list-view {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		padding: 1.5rem;
		min-height: 100%;
	}

	/* Header */
	.list-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding-bottom: 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.list-title-group {
		display: flex;
		align-items: center;
		gap: 0.625rem;
	}

	.list-title {
		font-family: var(--font-display);
		font-size: 1.35rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		margin: 0;
		letter-spacing: -0.01em;
	}

	.list-count {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		padding: 0.1rem 0.45rem;
	}

	.list-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.search-field {
		display: inline-flex;
		align-items: center;
		gap: 0.45rem;
		padding: 0.45rem 0.65rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		background: var(--bg-surface);
		color: var(--text-muted);
		min-width: min(22rem, 40vw);
	}

	.search-input {
		border: 0;
		outline: 0;
		background: transparent;
		color: var(--text-primary);
		font-family: var(--font-mono);
		font-size: 0.72rem;
		width: 100%;
	}

	.search-input::placeholder {
		color: var(--text-muted);
	}

	/* Empty state */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.625rem;
		padding: 5rem 2rem;
		text-align: center;
	}

	.empty-icon {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		color: var(--text-muted);
		margin-bottom: 0.5rem;
	}

	.empty-title {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.82rem;
		color: var(--text-secondary);
	}

	.empty-sub {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
	}

	/* Card grid */
	.sandbox-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(320px, 320px));
		gap: 0.875rem;
		align-items: stretch;
		justify-content: start;
	}

	.sandbox-grid > :global(.anim-fade-up) {
		height: 100%;
		display: flex;
	}

	/* Backdrop */
	.drawer-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0,0,0,0.55);
		z-index: 40;
		animation: fadeIn 0.2s var(--ease-out) both;
	}

	@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

	/* Slide-over drawer */
	.drawer {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(520px, 100vw);
		background: var(--bg-surface);
		border-left: 1px solid var(--border-mid);
		z-index: 50;
		display: flex;
		flex-direction: column;
		transform: translateX(100%);
		transition: transform 0.28s var(--ease-snappy);
		box-shadow: -12px 0 40px rgba(0,0,0,0.45);
	}

	.drawer--open {
		transform: translateX(0);
	}

	.drawer-header {
		padding: 1.125rem 1.25rem 0.875rem;
		border-bottom: 1px solid var(--border-dim);
		flex-shrink: 0;
	}

	.drawer-title-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.75rem;
	}

	.drawer-title {
		margin: 0;
		font-family: var(--font-display);
		font-size: 1.2rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.drawer-close {
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		background: transparent;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.drawer-close:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	.preset-chips {
		display: flex;
		gap: 0.3rem;
		flex-wrap: wrap;
	}

	.preset-chip {
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.2rem 0.55rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.preset-chip:hover {
		color: var(--text-primary);
		border-color: var(--border-mid);
		background: var(--accent-dim);
	}

	/* Drawer body (scrollable) */
	.drawer-body {
		flex: 1;
		overflow-y: auto;
		padding: 1.125rem 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 0;
	}

	.create-fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}

	/* Form sections */
	.form-section {
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
		padding: 1rem 0;
		border-bottom: 1px solid var(--border-dim);
	}

	.form-section:last-child {
		border-bottom: none;
	}

	.form-section-title {
		font-size: 0.7rem;
		margin-bottom: 0.25rem;
	}

	.form-row-2 {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.625rem;
	}

	.field-col {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.opt {
		font-size: 0.58rem;
		color: var(--text-muted);
	}

	.field-help {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		line-height: 1.45;
	}

	.create-image-empty {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.6rem;
		flex-wrap: wrap;
		padding: 0.45rem 0.5rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		background: var(--bg-raised);
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


	/* Drawer footer */
	.drawer-footer {
		padding: 0.875rem 1.25rem;
		border-top: 1px solid var(--border-dim);
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 0.625rem;
		flex-shrink: 0;
		background: var(--bg-surface);
	}

	/* Shared */
	.btn-ghost { display: inline-flex; align-items: center; gap: 0.35rem; }
	.btn-primary { display: inline-flex; align-items: center; gap: 0.4rem; }
	.spin { animation: rotate 0.8s linear infinite; }
	@keyframes rotate { to { transform: rotate(360deg); } }

	@media (max-width: 640px) {
		.list-view { padding: 1rem; }
		.list-header { align-items: stretch; flex-direction: column; }
		.list-actions { width: 100%; flex-wrap: wrap; }
		.search-field { min-width: 100%; }
		.sandbox-grid { grid-template-columns: 1fr; }
		.drawer { width: 100vw; }
		.form-row-2 { grid-template-columns: 1fr; }
	}
</style>
