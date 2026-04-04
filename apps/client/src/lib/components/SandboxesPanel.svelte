<script lang="ts">
	import SandboxCard from "./SandboxCard.svelte";
	import type { ContainerSummary, ImageSearchResult, ImageSummary, Sandbox } from "$lib/api";

	type CreateMethod = "existing" | "pull" | "build-context" | "build-inline";

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
		createBuildDockerfile = $bindable(),
		createBuildTag = $bindable(),
		createInlineDockerfile = $bindable(),
		createInlineTag = $bindable(),
		createInlineContent = $bindable(),
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
		createBuildDockerfile: string;
		createBuildTag: string;
		createInlineDockerfile: string;
		createInlineTag: string;
		createInlineContent: string;
		createRepoUrl: string;
		createBranch: string;
		createPorts: string;
		createLoading: boolean;
		createStep: string;
		createLogs: string;
		createResolvedImage: string;
		onToggleCreate: () => void;
		onSearchImages: () => void;
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

	const createMethods = [
		{ id: "existing", label: "Existing image", description: "Use a local tag that is already available." },
		{ id: "pull", label: "Pull image", description: "Pull from a registry, then create the sandbox." },
		{ id: "build-context", label: "Build from context", description: "Build from a workspace path and Dockerfile." },
		{ id: "build-inline", label: "Build from Dockerfile", description: "Build from inline Dockerfile text." }
	] as const;

	const listImageTags = (items: ImageSummary[]): string[] => Array.from(new Set(
		items.flatMap((image) => image.repo_tags.filter((tag) => tag !== "<none>:<none>"))
	));

	const localImageTags = $derived(listImageTags(images));

	const pullSearchOptionLabel = (result: ImageSearchResult): string => {
		const badges: string[] = [];
		if (result.official) {
			badges.push("official");
		}
		if (result.automated) {
			badges.push("automated");
		}
		const suffix = badges.length > 0 ? ` - ${badges.join(", ")}` : "";
		return `${result.name} (${result.stars} stars)${suffix}`;
	};
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

	<!-- Create form -->
	{#if showCreateForm}
		<div class="create-form panel anim-fade-up">
			<div class="panel-header">
				<span class="panel-title">New sandbox</span>
				<div class="preset-chips">
					{#each presets as preset}
						<button class="preset-chip" type="button" onclick={() => onApplyPreset(preset.name, preset.image)}>
							{preset.label}
						</button>
					{/each}
				</div>
			</div>
			<div class="panel-body">
				<fieldset class="create-fieldset" disabled={createLoading}>
					<div class="create-grid">
						<label class="field-col">
							<span class="section-label">Name</span>
							<input class="field" bind:value={createName} placeholder="workspace" required />
						</label>
						<div class="field-col span-2">
							<span class="section-label">Image source</span>
							<div class="method-grid">
								{#each createMethods as method}
									<button
										class="method-card"
										class:is-active={createMethod === method.id}
										type="button"
										onclick={() => createMethod = method.id}
									>
										<span class="method-label">{method.label}</span>
										<span class="method-desc">{method.description}</span>
									</button>
								{/each}
							</div>
						</div>

						{#if createMethod === "existing"}
							<label class="field-col span-2">
								<span class="section-label">Local image tag</span>
								<select class="field" bind:value={createExistingImage} required>
									<option value="" disabled>Select a local image</option>
									{#each localImageTags as tag (tag)}
										<option value={tag}>{tag}</option>
									{/each}
								</select>
								<span class="field-help">
									{#if localImageTags.length > 0}
										Choose from the local images already loaded on the server.
									{:else}
										No local images available yet. Pull or build one first.
									{/if}
								</span>
							</label>
						{/if}

						{#if createMethod === "pull"}
							<label class="field-col">
								<span class="section-label">Search Docker Hub</span>
								<input class="field" bind:value={createPullSearchQuery} placeholder="ubuntu" />
								<span class="field-help">Search remote images, then select one or type your own image below.</span>
							</label>
							<div class="field-col pull-search-actions">
								<span class="section-label">Search</span>
								<button class="btn-ghost pull-search-button" type="button" onclick={onSearchImages} disabled={createPullSearchLoading}>
									{createPullSearchLoading ? "Searching..." : "Search images"}
								</button>
							</div>
							{#if createPullSearchResults.length > 0}
								<label class="field-col span-2">
									<span class="section-label">Search results</span>
									<select class="field" bind:value={createPullSelectedImage} onchange={(event) => onSelectPullImage((event.currentTarget as HTMLSelectElement).value)}>
										<option value="">Keep manual image entry</option>
										{#each createPullSearchResults as result (result.name)}
											<option value={result.name}>{pullSearchOptionLabel(result)}</option>
										{/each}
									</select>
								</label>
							{/if}
							{#if createPullSearchError}
								<p class="alert-error pull-search-error span-2">{createPullSearchError}</p>
							{/if}
							<label class="field-col">
								<span class="section-label">Registry image</span>
								<input class="field" bind:value={createPullImage} placeholder="ubuntu" required />
							</label>
							<label class="field-col">
								<span class="section-label">Tag <span class="opt">(optional)</span></span>
								<input class="field" bind:value={createPullTag} placeholder="24.04" />
							</label>
						{/if}

						{#if createMethod === "build-context"}
							<label class="field-col">
								<span class="section-label">Context path</span>
								<input class="field" bind:value={createBuildContextPath} placeholder="apps/server" required />
								<span class="field-help">Resolved on the server inside its configured workspace root.</span>
							</label>
							<label class="field-col">
								<span class="section-label">Dockerfile</span>
								<input class="field" bind:value={createBuildDockerfile} placeholder="Dockerfile" />
							</label>
							<label class="field-col span-2">
								<span class="section-label">Output tag</span>
								<input class="field" bind:value={createBuildTag} placeholder="sandbox-app:latest" required />
							</label>
						{/if}

						{#if createMethod === "build-inline"}
							<label class="field-col">
								<span class="section-label">Dockerfile name</span>
								<input class="field" bind:value={createInlineDockerfile} placeholder="Dockerfile" />
							</label>
							<label class="field-col">
								<span class="section-label">Output tag</span>
								<input class="field" bind:value={createInlineTag} placeholder="sandbox-inline:latest" required />
							</label>
							<label class="field-col span-2">
								<span class="section-label">Dockerfile content</span>
								<textarea class="field field-textarea field-code" bind:value={createInlineContent} placeholder="FROM ubuntu:24.04&#10;WORKDIR /workspace"></textarea>
							</label>
						{/if}

						<label class="field-col">
							<span class="section-label">Repo URL <span class="opt">(optional)</span></span>
							<input class="field" bind:value={createRepoUrl} placeholder="https://github.com/org/repo.git" />
						</label>
						<label class="field-col">
							<span class="section-label">Branch <span class="opt">(optional)</span></span>
							<input class="field" bind:value={createBranch} placeholder="main" />
						</label>
						<label class="field-col span-2">
							<span class="section-label">Ports <span class="opt">(one per line)</span></span>
							<textarea class="field field-textarea" bind:value={createPorts} placeholder="3000:3000&#10;8080:8080"></textarea>
						</label>
					</div>
					<div class="create-footer">
						<button class="btn-primary" type="button" onclick={onCreateSubmit} disabled={createLoading}>
							{createLoading ? `${createStep}...` : "Run pipeline"}
						</button>
					</div>
				</fieldset>

				{#if createStep !== "Idle" || createResolvedImage || createLogs}
					<div class="pipeline-panel">
						<div class="pipeline-header">
							<span class="pipeline-title">Pipeline</span>
							<span class="pipeline-step">{createStep}</span>
						</div>
						{#if createResolvedImage}
							<p class="pipeline-image">Resolved image: <code>{createResolvedImage}</code></p>
						{/if}
						<pre class="pipeline-log">{createLogs || "Waiting for pipeline output..."}</pre>
					</div>
				{/if}

			</div>
		</div>
	{/if}

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

	/* Create form */
	.create-fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		min-width: 0;
	}
	.create-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.75rem;
		margin-bottom: 1rem;
	}
	.field-col { display: flex; flex-direction: column; gap: 0.3rem; }
	.span-2 { grid-column: span 2; }
	.create-footer { display: flex; justify-content: flex-end; }
	.pull-search-actions {
		justify-content: flex-end;
	}
	.pull-search-button {
		justify-content: center;
		min-height: 2.5rem;
	}
	.pull-search-error {
		margin: 0;
	}
	.method-grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 0.5rem;
	}
	.method-card {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 0.2rem;
		padding: 0.75rem;
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
		border-color: var(--border-strong);
		background: color-mix(in srgb, var(--bg-raised) 85%, white 15%);
		color: var(--text-primary);
	}
	.method-label {
		font-family: var(--font-mono);
		font-size: 0.68rem;
	}
	.method-desc {
		font-size: 0.7rem;
		line-height: 1.45;
		color: var(--text-muted);
	}
	.preset-chips { display: flex; gap: 0.3rem; flex-wrap: wrap; }
	.preset-chip {
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.15rem 0.5rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s;
	}
	.preset-chip:hover { color: var(--text-primary); border-color: var(--border-mid); }
	.opt { font-size: 0.58rem; color: var(--text-muted); }
	.field-help {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		line-height: 1.45;
	}
	.field-code {
		min-height: 11rem;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		line-height: 1.5;
	}
	.pipeline-panel {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.85rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		background: var(--bg-raised);
	}
	.pipeline-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
	}
	.pipeline-title,
	.pipeline-step,
	.pipeline-image {
		font-family: var(--font-mono);
	}
	.pipeline-title {
		font-size: 0.68rem;
		color: var(--text-secondary);
	}
	.pipeline-step {
		font-size: 0.62rem;
		color: var(--text-primary);
		padding: 0.15rem 0.45rem;
		border-radius: 999px;
		border: 1px solid var(--border-dim);
		background: var(--bg-surface);
	}
	.pipeline-image {
		margin: 0;
		font-size: 0.65rem;
		color: var(--text-muted);
	}
	.pipeline-log {
		margin: 0;
		max-height: 14rem;
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

	/* Shared */
	.btn-ghost { display: inline-flex; align-items: center; gap: 0.35rem; }
	.spin { animation: rotate 0.8s linear infinite; }
	@keyframes rotate { to { transform: rotate(360deg); } }

	@media (max-width: 640px) {
		.list-view { padding: 1rem; }
		.create-grid { grid-template-columns: 1fr; }
		.method-grid { grid-template-columns: 1fr; }
		.span-2 { grid-column: span 1; }
		.sandbox-grid { grid-template-columns: 1fr; }
	}
</style>
