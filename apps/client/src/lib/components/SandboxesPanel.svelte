<script lang="ts">
	import SandboxCard from "./SandboxCard.svelte";
	import Combobox from "./Combobox.svelte";
	import CodeEditor from "./CodeEditor.svelte";
	import PortsEditor from "./PortsEditor.svelte";
	import type { ContainerSummary, ImageSearchResult, ImageSummary, Sandbox } from "$lib/api";

	type CreateMethod = "existing" | "pull" | "build-context" | "build-inline" | "compose";

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
		onStop,
		onDelete,
		onRefresh,
		showCreateForm,
		createName = $bindable(),
		createMethod = $bindable(),
		createExistingImage = $bindable(),
		createPullImage = $bindable(),
		createPullTag = $bindable(),
		createPullSearchQuery = $bindable(),
		createPullSearchResults,
		createPullSelectedImage = $bindable(),
		createPullSearchLoading,
		createPullSearchError,
		createBuildContextPath = $bindable(),
		createBuildTag = $bindable(),
		createInlineTag = $bindable(),
		createInlineContent = $bindable(),
		createComposeContent = $bindable(),
		createComposeProjectName = $bindable(),
		createRepoUrl = $bindable(),
		createBranch = $bindable(),
		createPorts = $bindable(),
		createLoading,
		createStep,
		createLogs,
		createResolvedImage,
		onToggleCreate,
		onSearchImages,
		onSelectPullImage,
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
		onStop: (id: string) => void;
		onDelete: (id: string) => void;
		onRefresh: () => void;
		showCreateForm: boolean;
		createName: string;
		createMethod: CreateMethod;
		createExistingImage: string;
		createPullImage: string;
		createPullTag: string;
		createPullSearchQuery: string;
		createPullSearchResults: ImageSearchResult[];
		createPullSelectedImage: string;
		createPullSearchLoading: boolean;
		createPullSearchError: string;
		createBuildContextPath: string;
		createBuildTag: string;
		createInlineTag: string;
		createInlineContent: string;
		createComposeContent: string;
		createComposeProjectName: string;
		createRepoUrl: string;
		createBranch: string;
		createPorts: string;
		createLoading: boolean;
		createStep: string;
		createLogs: string;
		createResolvedImage: string;
		onToggleCreate: () => void;
		onSearchImages: (query: string) => void;
		onSelectPullImage: (imageName: string) => void;
		onCreateSubmit: () => void;
		onApplyPreset: (name: string, image: string) => void;
	}>();

	const presets = [
		{ label: "Ubuntu", name: "ubuntu-workspace", image: "ubuntu:24.04" },
		{ label: "Node",   name: "node-workspace",   image: "node:22"       },
		{ label: "Python", name: "python-workspace",  image: "python:3.12"  },
		{ label: "Go",     name: "go-workspace",      image: "golang:1.24"  }
	] as const;

	const createMethods: Array<{ id: CreateMethod; label: string; description: string; icon: string }> = [
		{
			id: "existing",
			label: "Existing image",
			description: "Use a local tag already on the server",
			icon: `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>`
		},
		{
			id: "pull",
			label: "Pull image",
			description: "Pull from Docker Hub or any registry",
			icon: `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>`
		},
		{
			id: "build-context",
			label: "Build from context",
			description: "Build from a server path with a Dockerfile",
			icon: `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`
		},
		{
			id: "build-inline",
			label: "Inline Dockerfile",
			description: "Write a Dockerfile directly in the editor",
			icon: `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>`
		},
		{
			id: "compose",
			label: "Docker Compose",
			description: "Run services from a docker-compose.yml",
			icon: `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/><path d="M7 8h2M7 12h2M11 8h6M11 12h6"/></svg>`
		}
	];

	// Build combobox options from local images
	const localImageOptions = $derived(
		Array.from(
			new Set(images.flatMap((img: ImageSummary) => img.repo_tags.filter((t: string) => t !== "<none>:<none>"))) as Set<string>
		).map((tag: string) => ({ value: tag, label: tag }))
	);

	// Build combobox options from pull search results
	const pullSearchOptions = $derived(
		createPullSearchResults.map((r: ImageSearchResult) => ({
			value: r.name,
			label: r.name,
			description: `${r.stars} stars${r.official ? " · official" : ""}${r.automated ? " · automated" : ""}`,
			badge: r.official ? "official" : undefined
		}))
	);

	// ANSI stripping for pipeline log display (display only, not storage)
	const stripAnsi = (str: string): string => str.replace(/\x1b\[[0-9;]*[mGKHF]/g, "");
</script>

<div class="list-view anim-fade-up">
	<!-- Page header -->
	<div class="list-header">
		<div class="list-title-group">
			<h1 class="list-title">Sandboxes</h1>
			{#if sandboxes.length > 0}
				<span class="list-count">{sandboxes.length}</span>
			{/if}
		</div>
		<div class="list-actions">
			<button class="btn-ghost btn-sm" type="button" onclick={onRefresh} disabled={loading}>
				{#if loading}
					<svg class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{:else}
					<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
				{/if}
				Refresh
			</button>
			<button class="btn-primary btn-sm" type="button" onclick={onToggleCreate}>
				{showCreateForm ? "Cancel" : "+ New sandbox"}
			</button>
		</div>
	</div>

	<!-- Alerts -->
	{#if errorMessage}<p class="alert-error anim-fade-up">{errorMessage}</p>{/if}
	{#if notice}<p class="alert-ok anim-fade-up">{notice}</p>{/if}

	<!-- Card grid -->
	{#if sandboxes.length === 0 && !showCreateForm}
		<div class="empty-state anim-fade-up anim-delay-1">
			<div class="empty-icon">
				<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>
				</svg>
			</div>
			<p class="empty-title">{loading ? "Loading..." : "No sandboxes yet"}</p>
			{#if !loading}
				<p class="empty-sub">Create your first sandbox to get started.</p>
				<button class="btn-ghost btn-sm" type="button" onclick={onToggleCreate}>+ New sandbox</button>
			{/if}
		</div>
	{:else if sandboxes.length > 0}
		<div class="sandbox-grid">
			{#each sandboxes as sandbox, i (sandbox.id)}
				<div class="anim-fade-up" style="animation-delay: {i * 0.035}s">
					<SandboxCard
						{sandbox}
						container={containers.find((c: ContainerSummary) => c.id === sandbox.container_id) ?? null}
						isSelected={false}
						onOpen={() => onOpen(sandbox.id)}
						onRestart={() => onRestart(sandbox.id)}
						onReset={() => onReset(sandbox.id)}
						onStop={() => onStop(sandbox.id)}
						onDelete={() => onDelete(sandbox.id)}
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

			<!-- Name -->
			<div class="form-section">
				<label class="field-col">
					<span class="section-label">Sandbox name</span>
					<input class="field" bind:value={createName} placeholder="my-workspace" required />
				</label>
			</div>

			<!-- Method -->
			<div class="form-section">
				<span class="section-label">Image source</span>
				<div class="method-grid">
					{#each createMethods as method}
						<button
							class="method-card"
							class:is-active={createMethod === method.id}
							type="button"
							onclick={() => createMethod = method.id}
						>
							<span class="method-icon">{@html method.icon}</span>
							<div class="method-text">
								<span class="method-label">{method.label}</span>
								<span class="method-desc">{method.description}</span>
							</div>
						</button>
					{/each}
				</div>
			</div>

			<!-- Method-specific fields -->
			{#if createMethod === "existing"}
				<div class="form-section">
					<label class="field-col">
						<span class="section-label">Local image</span>
						<Combobox
							bind:value={createExistingImage}
							options={localImageOptions}
							placeholder="Search local images..."
							emptyText={localImageOptions.length === 0 ? "No local images available. Pull or build one first." : "No matches"}
						/>
						{#if localImageOptions.length === 0}
							<span class="field-help">No local images yet. Use Pull or Build first.</span>
						{/if}
					</label>
				</div>
			{/if}

			{#if createMethod === "pull"}
				<div class="form-section">
					<label class="field-col">
						<span class="section-label">Search Docker Hub</span>
						<Combobox
							bind:value={createPullImage}
							options={pullSearchOptions}
							placeholder="Search images (e.g. ubuntu, node, python)..."
							loading={createPullSearchLoading}
							emptyText="Type to search Docker Hub"
							onSearch={(q) => { createPullSearchQuery = q; onSearchImages(q); }}
							onSelect={(opt) => { onSelectPullImage(opt.value); }}
						/>
						<span class="field-help">Search remote images or type your own image name directly.</span>
					</label>
					{#if createPullSearchError}
						<p class="alert-error">{createPullSearchError}</p>
					{/if}
					<label class="field-col">
						<span class="section-label">Tag <span class="opt">(optional)</span></span>
						<input class="field" bind:value={createPullTag} placeholder="latest" />
					</label>
				</div>
			{/if}

			{#if createMethod === "build-context"}
				<div class="form-section">
					<div class="form-row-2">
						<label class="field-col">
							<span class="section-label">Context path</span>
							<input class="field" bind:value={createBuildContextPath} placeholder="apps/server" required />
							<span class="field-help">Resolved on the server inside its workspace root. Must contain a <code class="inline-code">Dockerfile</code>.</span>
						</label>
						<label class="field-col">
							<span class="section-label">Output tag</span>
							<input class="field" bind:value={createBuildTag} placeholder="sandbox-app:latest" required />
						</label>
					</div>
				</div>
			{/if}

			{#if createMethod === "build-inline"}
				<div class="form-section">
					<label class="field-col">
						<span class="section-label">Output tag</span>
						<input class="field" bind:value={createInlineTag} placeholder="sandbox-inline:latest" required />
					</label>
					<div class="field-col">
						<span class="section-label">Dockerfile</span>
						<CodeEditor
							bind:value={createInlineContent}
							language="dockerfile"
							placeholder="FROM ubuntu:24.04&#10;WORKDIR /workspace"
							minHeight="14rem"
						/>
					</div>
				</div>
			{/if}

			{#if createMethod === "compose"}
				<div class="form-section">
					<div class="compose-note">
						<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
						<span>Docker Compose runs services directly — no sandbox image is created. The Name and Runtime options below are not used for this method.</span>
					</div>
					<label class="field-col">
						<span class="section-label">Project name <span class="opt">(optional)</span></span>
						<input class="field" bind:value={createComposeProjectName} placeholder="my-project" />
						<span class="field-help">Defaults to the directory name. Used to namespace compose resources.</span>
					</label>
					<div class="field-col">
						<span class="section-label">docker-compose.yml</span>
						<CodeEditor
							bind:value={createComposeContent}
							language="yaml"
							placeholder="version: '3.8'&#10;services:&#10;  app:&#10;    image: ubuntu:24.04&#10;    command: sleep infinity"
							minHeight="16rem"
						/>
					</div>
				</div>
			{/if}

			<!-- Repo + Ports (not shown for compose — it manages its own services) -->
			{#if createMethod !== "compose"}
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
				<div class="field-col">
					<span class="section-label">Port mappings <span class="opt">(host → container)</span></span>
					<PortsEditor bind:value={createPorts} />
				</div>
			</div>
			{/if}

		</fieldset>

		<!-- Pipeline output -->
		{#if createStep !== "Idle" || createResolvedImage || createLogs}
			<div class="pipeline-panel">
				<div class="pipeline-header">
					<span class="pipeline-title">Pipeline output</span>
					<span class="pipeline-step">{createStep}</span>
				</div>
				{#if createResolvedImage}
					<p class="pipeline-image">Image: <code>{createResolvedImage}</code></p>
				{/if}
				<pre class="pipeline-log">{stripAnsi(createLogs) || "Waiting for pipeline..."}</pre>
			</div>
		{/if}
	</div>

	<div class="drawer-footer">
		<button class="btn-ghost btn-sm" type="button" onclick={onToggleCreate} disabled={createLoading}>
			Cancel
		</button>
		<button class="btn-primary" type="button" onclick={onCreateSubmit} disabled={createLoading}>
			{#if createLoading}
				<svg class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{createStep}...
			{:else if createMethod === "compose"}
				Run compose
			{:else}
				Run pipeline
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
		grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
		gap: 0.875rem;
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

	.inline-code {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		padding: 0.05rem 0.3rem;
		color: var(--text-secondary);
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

	/* Method grid */
	.method-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.45rem;
	}

	.method-card {
		display: flex;
		align-items: flex-start;
		gap: 0.6rem;
		padding: 0.7rem 0.75rem;
		border-radius: var(--radius-md);
		border: 1px solid var(--border-dim);
		background: var(--bg-raised);
		color: var(--text-secondary);
		text-align: left;
		cursor: pointer;
		transition: border-color 0.12s, color 0.12s, background 0.12s;
	}

	.method-card:hover {
		border-color: var(--border-mid);
		color: var(--text-primary);
	}

	.method-card.is-active {
		border-color: var(--border-hi);
		background: var(--bg-overlay);
		color: var(--text-primary);
	}

	.method-icon {
		display: grid;
		place-items: center;
		flex-shrink: 0;
		margin-top: 0.05rem;
		color: inherit;
	}

	.method-text {
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
	}

	.method-label {
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: inherit;
		font-weight: 500;
	}

	.method-desc {
		font-size: 0.65rem;
		line-height: 1.4;
		color: var(--text-muted);
	}

	/* Pipeline panel */
	.pipeline-panel {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.85rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		margin-top: 1rem;
	}

	.pipeline-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
	}

	.pipeline-title {
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-secondary);
	}

	.pipeline-step {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-primary);
		padding: 0.15rem 0.45rem;
		border-radius: 999px;
		border: 1px solid var(--border-dim);
		background: var(--bg-surface);
	}

	.pipeline-image {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-muted);
	}

	.pipeline-log {
		margin: 0;
		max-height: 12rem;
		overflow: auto;
		padding: 0.75rem;
		border-radius: var(--radius-sm);
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		line-height: 1.5;
		white-space: pre-wrap;
		word-break: break-word;
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
		.sandbox-grid { grid-template-columns: 1fr; }
		.drawer { width: 100vw; }
		.method-grid { grid-template-columns: 1fr; }
		.form-row-2 { grid-template-columns: 1fr; }
	}
</style>
