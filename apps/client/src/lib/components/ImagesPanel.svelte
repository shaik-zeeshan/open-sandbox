<script lang="ts">
	import { onMount } from "svelte";
	import { toast } from "$lib/toast.svelte";
	import Combobox from "./Combobox.svelte";
	import CodeEditor from "./CodeEditor.svelte";
	import {
		buildImageStream,
		formatApiFailure,
		listImages,
		pullImage,
		removeImage,
		runApiEffect,
		searchImages,
		type ApiConfig,
		type ImageSearchResult,
		type ImageSummary
	} from "$lib/api";

	type ImageCreateMethod = "pull" | "build-context" | "build-inline";

	let {
		config
	} = $props<{
		config: ApiConfig;
	}>();

	let images = $state<ImageSummary[]>([]);
	let loading = $state(false);

	let createMethod = $state<ImageCreateMethod>("pull");
	let createPullImage = $state("");
	let createPullTag = $state("");
	let createPullSearchQuery = $state("");
	let createPullSearchResults = $state<ImageSearchResult[]>([]);
	let createPullSelectedImage = $state("");
	let createPullSearchLoading = $state(false);
	let createPullSearchError = $state("");
	let createBuildContextPath = $state("");
	let createBuildTag = $state("");
	let createInlineTag = $state("");
	let createInlineContent = $state("");
	let createLoading = $state(false);
	let createStep = $state("Idle");
	let createLogs = $state("");
	let createResolvedImage = $state("");
	let deleteConfirmId = $state<string | null>(null);

	const createMethods: Array<{ id: ImageCreateMethod; label: string; description: string }> = [
		{ id: "pull", label: "Pull image", description: "Pull from Docker Hub or another registry" },
		{ id: "build-context", label: "Build from context", description: "Build from a server path containing a Dockerfile" },
		{ id: "build-inline", label: "Inline Dockerfile", description: "Build from Dockerfile content entered below" }
	];

	const pullSearchOptions = $derived(
		createPullSearchResults.map((result: ImageSearchResult) => ({
			value: result.name,
			label: result.name,
			description: `${result.stars} stars${result.official ? " · official" : ""}${result.automated ? " · automated" : ""}`,
			badge: result.official ? "official" : undefined
		}))
	);

	const stripAnsi = (value: string): string => value.replace(/\x1b\[[0-9;]*[mGKHF]/g, "");

	const sortedImages = $derived.by(() =>
		[...images].sort((a, b) => b.created - a.created)
	);

	function resetPipelineState(): void {
		createStep = "Idle";
		createLogs = "";
		createResolvedImage = "";
	}

	function appendCreateLog(output: string): void {
		const normalized = output.replace(/\r\n/g, "\n").trim();
		if (normalized.length === 0) {
			return;
		}
		createLogs = createLogs.length > 0 ? `${createLogs}\n${normalized}` : normalized;
	}

	const formatDate = (unixSeconds: number): string =>
		new Date(unixSeconds * 1000).toLocaleString(undefined, {
			year: "numeric",
			month: "short",
			day: "numeric",
			hour: "2-digit",
			minute: "2-digit"
		});

	const formatSize = (bytes: number): string => {
		if (bytes < 1024 * 1024) {
			return `${Math.round(bytes / 1024)} KB`;
		}
		if (bytes < 1024 * 1024 * 1024) {
			return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
		}
		return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
	};

	async function refreshImages(): Promise<void> {
		loading = true;
		try {
			images = await runApiEffect(listImages(config));
		} catch (error) {
			toast.error(formatApiFailure(error));
		} finally {
			loading = false;
		}
	}

	async function runImageSearch(queryOverride?: string): Promise<void> {
		const query = (queryOverride ?? createPullSearchQuery).trim();
		createPullSearchError = "";
		if (query.length === 0) {
			createPullSearchResults = [];
			createPullSelectedImage = "";
			createPullSearchError = "Search query is required.";
			return;
		}

		createPullSearchLoading = true;
		try {
			const results = await runApiEffect(searchImages(config, query, 25));
			createPullSearchResults = results;
			const exactMatch = results.find((result) => result.name === createPullImage.trim()) ?? results[0] ?? null;
			createPullSelectedImage = exactMatch?.name ?? "";
			if (exactMatch !== null) {
				createPullImage = exactMatch.name;
			}
			if (results.length === 0) {
				createPullSearchError = `No Docker Hub results for ${query}.`;
			}
		} catch (error) {
			createPullSearchResults = [];
			createPullSelectedImage = "";
			createPullSearchError = formatApiFailure(error);
		} finally {
			createPullSearchLoading = false;
		}
	}

	function selectPullSearchImage(imageName: string): void {
		createPullSelectedImage = imageName;
		if (imageName.trim().length > 0) {
			createPullImage = imageName;
		}
	}

	async function submitCreate(): Promise<void> {
		createLoading = true;
		resetPipelineState();
		createStep = "Preparing";

		try {
			if (createMethod === "pull") {
				const imageName = createPullImage.trim();
				const imageTag = createPullTag.trim();
				if (imageName.length === 0) {
					throw new Error("Image name is required for pull.");
				}

				const resolvedImage = imageTag.length > 0 ? `${imageName}:${imageTag}` : imageName;
				createResolvedImage = resolvedImage;
				createStep = "Pulling image";
				appendCreateLog(`Pulling ${resolvedImage}`);
				const pulled = await runApiEffect(
					pullImage(config, {
						image: imageName,
						tag: imageTag || undefined
					})
				);
				appendCreateLog(pulled.output);
				appendCreateLog(`Image ready: ${pulled.image}`);
				toast.ok(`Image ready: ${pulled.image}`);
			}

			if (createMethod === "build-context") {
				const contextPath = createBuildContextPath.trim();
				const tag = createBuildTag.trim();
				if (contextPath.length === 0) {
					throw new Error("Context path is required for image build.");
				}
				if (tag.length === 0) {
					throw new Error("Image tag is required for image build.");
				}

				createResolvedImage = tag;
				createStep = "Building image";
				appendCreateLog(`Building ${tag} from ${contextPath}`);
				let buildError = "";
				await runApiEffect(buildImageStream(
					config,
					{
						context_path: contextPath,
						dockerfile: "Dockerfile",
						tag
					},
					(event) => {
						if ((event.event === "stdout" || event.event === "stderr") && event.data.length > 0) {
							appendCreateLog(event.data);
						}
						if (event.event === "error") {
							buildError = event.data.trim();
						}
						if (event.event === "done" && event.data.trim().length > 0) {
							appendCreateLog(`Image ready: ${event.data.trim()}`);
						}
					}
				));
				if (buildError.length > 0) {
					throw new Error(buildError);
				}
				toast.ok(`Image ready: ${tag}`);
			}

			if (createMethod === "build-inline") {
				const dockerfileContent = createInlineContent.trim();
				const tag = createInlineTag.trim();
				if (dockerfileContent.length === 0) {
					throw new Error("Dockerfile content is required.");
				}
				if (tag.length === 0) {
					throw new Error("Image tag is required for inline build.");
				}

				createResolvedImage = tag;
				createStep = "Building image";
				appendCreateLog(`Building ${tag} from inline Dockerfile`);
				let buildError = "";
				await runApiEffect(buildImageStream(
					config,
					{
						dockerfile: "Dockerfile",
						dockerfile_content: dockerfileContent,
						tag
					},
					(event) => {
						if ((event.event === "stdout" || event.event === "stderr") && event.data.length > 0) {
							appendCreateLog(event.data);
						}
						if (event.event === "error") {
							buildError = event.data.trim();
						}
						if (event.event === "done" && event.data.trim().length > 0) {
							appendCreateLog(`Image ready: ${event.data.trim()}`);
						}
					}
				));
				if (buildError.length > 0) {
					throw new Error(buildError);
				}
				toast.ok(`Image ready: ${tag}`);
			}

			createStep = "Done";
			await refreshImages();
		} catch (error) {
			toast.error(formatApiFailure(error));
		} finally {
			createLoading = false;
		}
	}

	async function submitDelete(image: ImageSummary): Promise<void> {
		if (deleteConfirmId !== image.id) {
			deleteConfirmId = image.id;
			setTimeout(() => {
				if (deleteConfirmId === image.id) {
					deleteConfirmId = null;
				}
			}, 3000);
			return;
		}

		loading = true;
		deleteConfirmId = null;
		try {
			await runApiEffect(removeImage(config, image.id, false));
			toast.ok("Image removed.");
			await refreshImages();
		} catch (error) {
			toast.error(formatApiFailure(error));
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void refreshImages();
	});

	$effect(() => {
		if (createPullSelectedImage.trim().length > 0 && createPullImage.trim() !== createPullSelectedImage.trim()) {
			createPullSelectedImage = "";
		}
	});

	$effect(() => {
		config.baseUrl;
		void refreshImages();
	});
</script>

<div class="images-panel anim-fade-up">
	<div class="images-header">
		<div>
			<p class="section-label">Build</p>
			<h1 class="images-title">Images</h1>
		</div>
		<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshImages()} disabled={loading}>
			{loading ? "Refreshing..." : "Refresh"}
		</button>
	</div>

	<div class="images-layout">
		<section class="panel">
			<div class="panel-header">
				<span class="panel-title">Create image</span>
			</div>
			<div class="panel-body create-body">
				<div class="method-grid">
					{#each createMethods as method}
						<button
							type="button"
							class="method-btn"
							class:method-btn--active={createMethod === method.id}
							onclick={() => createMethod = method.id}
						>
							<span class="method-label">{method.label}</span>
							<span class="method-description">{method.description}</span>
						</button>
					{/each}
				</div>

				{#if createMethod === "pull"}
					<div class="form-stack">
						<label class="field-col">
							<span class="section-label">Search Docker Hub</span>
							<Combobox
								bind:value={createPullImage}
								options={pullSearchOptions}
								placeholder="Search images (e.g. ubuntu, node, python)..."
								loading={createPullSearchLoading}
								emptyText="Type to search Docker Hub"
								onSearch={(query) => {
									createPullSearchQuery = query;
									void runImageSearch(query);
								}}
								onSelect={(option) => selectPullSearchImage(option.value)}
							/>
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
					<div class="form-stack">
						<label class="field-col">
							<span class="section-label">Context path</span>
							<input class="field" bind:value={createBuildContextPath} placeholder="apps/server" required />
							<span class="field-help">Resolved on the server inside its workspace root. Must include a <code class="inline-code">Dockerfile</code>.</span>
						</label>
						<label class="field-col">
							<span class="section-label">Output tag</span>
							<input class="field" bind:value={createBuildTag} placeholder="sandbox-app:latest" required />
						</label>
					</div>
				{/if}

				{#if createMethod === "build-inline"}
					<div class="form-stack">
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

				<div class="create-footer">
					<button class="btn-primary" type="button" onclick={() => void submitCreate()} disabled={createLoading}>
						{createLoading ? `${createStep}...` : "Create image"}
					</button>
				</div>

				{#if createStep !== "Idle" || createResolvedImage || createLogs}
					<div class="pipeline-panel">
						<div class="pipeline-header">
							<span class="pipeline-title">Creation output</span>
							<span class="pipeline-step">{createStep}</span>
						</div>
						{#if createResolvedImage}
							<p class="pipeline-image">Image: <code>{createResolvedImage}</code></p>
						{/if}
						<pre class="pipeline-log">{stripAnsi(createLogs) || "Waiting for pipeline..."}</pre>
					</div>
				{/if}
			</div>
		</section>

		<section class="panel">
			<div class="panel-header">
				<span class="panel-title">Local images</span>
				<span class="images-count">{sortedImages.length}</span>
			</div>
			<div class="panel-body images-list-body">
				{#if sortedImages.length === 0 && !loading}
					<p class="empty-copy">No images yet. Pull or build one to use in sandbox creation.</p>
				{:else}
					<div class="images-list">
						{#each sortedImages as image}
							<div class="image-row">
								<div class="image-row-main">
									<div class="image-tags">
										{#if image.repo_tags.length > 0}
											{#each image.repo_tags.filter((tag) => tag !== "<none>:<none>") as tag}
												<span class="tag-chip">{tag}</span>
											{/each}
										{:else}
											<span class="tag-chip">untagged</span>
										{/if}
									</div>
									<div class="image-meta">
										<span class="image-id">{image.id.slice(0, 16)}</span>
										<span>{formatSize(image.size)}</span>
										<span>{formatDate(image.created)}</span>
									</div>
								</div>
								<button class="btn-danger btn-xs" type="button" onclick={() => void submitDelete(image)} disabled={loading}>
									{deleteConfirmId === image.id ? "Confirm delete" : "Delete"}
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		</section>
	</div>
</div>

<style>
	.images-panel {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		padding: 1.5rem;
	}

	.images-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding-bottom: 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.images-title {
		margin: 0.2rem 0 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-style: italic;
		font-weight: 400;
		color: var(--text-primary);
	}

	.images-layout {
		display: grid;
		grid-template-columns: minmax(20rem, 28rem) minmax(0, 1fr);
		gap: 1rem;
	}

	.create-body,
	.images-list-body {
		display: flex;
		flex-direction: column;
		gap: 0.85rem;
	}

	.method-grid {
		display: grid;
		gap: 0.45rem;
	}

	.method-btn {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
		text-align: left;
		padding: 0.65rem 0.75rem;
		border-radius: var(--radius-md);
		border: 1px solid var(--border-dim);
		background: var(--bg-raised);
		color: var(--text-secondary);
		cursor: pointer;
		transition: border-color 0.12s, color 0.12s, background 0.12s;
	}

	.method-btn:hover {
		border-color: var(--border-mid);
		color: var(--text-primary);
	}

	.method-btn--active {
		border-color: var(--border-hi);
		color: var(--text-primary);
		background: var(--accent-dim);
	}

	.method-label {
		font-family: var(--font-mono);
		font-size: 0.68rem;
	}

	.method-description {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-muted);
	}

	.form-stack {
		display: flex;
		flex-direction: column;
		gap: 0.7rem;
	}

	.field-col {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
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

	.opt {
		font-size: 0.58rem;
		color: var(--text-muted);
	}

	.create-footer {
		display: flex;
		justify-content: flex-end;
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
	.pipeline-step,
	.pipeline-image {
		font-family: var(--font-mono);
		font-size: 0.62rem;
	}

	.pipeline-step {
		color: var(--text-secondary);
	}

	.pipeline-image {
		margin: 0;
		padding: 0.55rem 0.65rem 0;
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

	.images-count {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
	}

	.images-list {
		display: flex;
		flex-direction: column;
		gap: 0.6rem;
	}

	.image-row {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.7rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		background: var(--bg-raised);
	}

	.image-row-main {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
		min-width: 0;
	}

	.image-tags {
		display: flex;
		align-items: center;
		gap: 0.35rem;
		flex-wrap: wrap;
	}

	.tag-chip {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.12rem 0.4rem;
		border-radius: 3px;
		border: 1px solid var(--border-mid);
		color: var(--text-secondary);
		background: var(--bg-overlay);
	}

	.image-meta {
		display: flex;
		align-items: center;
		gap: 0.55rem;
		flex-wrap: wrap;
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-muted);
	}

	.image-id {
		color: var(--text-secondary);
	}

	.empty-copy {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-muted);
	}

	@media (max-width: 1024px) {
		.images-layout {
			grid-template-columns: 1fr;
		}
	}
</style>
