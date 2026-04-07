<script lang="ts">
	import { getCachedImages, invalidateImagesCache, refreshCachedImages } from "$lib/api-cache";
	import { clearScheduledTimeout, scheduleTimeout, type TimeoutHandle } from "$lib/client/browser";
	import { toast } from "$lib/toast.svelte";
	import Combobox from "./Combobox.svelte";
	import CodeEditor from "./CodeEditor.svelte";
	import {
		buildImageStream,
		formatApiFailure,
		pullImage,
		removeImage,
		runApiEffect,
		searchImages,
		type ApiConfig,
		type ImageSearchResult,
		type ImageSummary,
		type StreamEvent
	} from "$lib/api";
	import { Context, Effect, Layer } from "effect";

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
	let deletingId = $state<string | null>(null);

	// Field-level validation errors
	let pullImageError = $state("");
	let buildContextPathError = $state("");
	let buildTagError = $state("");
	let inlineTagError = $state("");
	let inlineContentError = $state("");

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

	function validateCreateForm(): boolean {
		pullImageError = "";
		buildContextPathError = "";
		buildTagError = "";
		inlineTagError = "";
		inlineContentError = "";

		if (createMethod === "pull") {
			if (createPullImage.trim().length === 0) {
				pullImageError = "Image name is required.";
				return false;
			}
		} else if (createMethod === "build-context") {
			let valid = true;
			if (createBuildContextPath.trim().length === 0) {
				buildContextPathError = "Context path is required.";
				valid = false;
			}
			if (createBuildTag.trim().length === 0) {
				buildTagError = "Output tag is required.";
				valid = false;
			}
			if (!valid) return false;
		} else if (createMethod === "build-inline") {
			let valid = true;
			if (createInlineTag.trim().length === 0) {
				inlineTagError = "Output tag is required.";
				valid = false;
			}
			if (createInlineContent.trim().length === 0) {
				inlineContentError = "Dockerfile content is required.";
				valid = false;
			}
			if (!valid) return false;
		}
		return true;
	}

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

	interface ImagesApiService {
		list: (options?: { force?: boolean }) => Effect.Effect<ImageSummary[], unknown>;
		search: (query: string, limit: number) => Effect.Effect<ImageSearchResult[], unknown>;
		pull: (request: { image: string; tag?: string }) => Effect.Effect<{ output: string; image: string }, unknown>;
		buildFromContext: (
			request: { contextPath: string; tag: string },
			onEvent: (event: StreamEvent) => void
		) => Effect.Effect<void, unknown>;
		buildInline: (
			request: { dockerfileContent: string; tag: string },
			onEvent: (event: StreamEvent) => void
		) => Effect.Effect<void, unknown>;
		remove: (imageId: string) => Effect.Effect<void, unknown>;
	}

	interface ImagesFeedbackService {
		error: (error: unknown) => Effect.Effect<void>;
		ok: (message: string) => Effect.Effect<void>;
	}

	interface DeleteConfirmationService {
		request: (imageId: string) => Effect.Effect<boolean>;
		clear: Effect.Effect<void>;
	}

	const ImagesApiService = Context.GenericTag<ImagesApiService>("images-panel/ImagesApiService");
	const ImagesFeedbackService = Context.GenericTag<ImagesFeedbackService>("images-panel/ImagesFeedbackService");
	const DeleteConfirmationService = Context.GenericTag<DeleteConfirmationService>("images-panel/DeleteConfirmationService");

	const imagesApiService: ImagesApiService = {
		list: (options) => (options?.force ? refreshCachedImages(config) : getCachedImages(config)),
		search: (query, limit) => Effect.promise(() => runApiEffect(searchImages(config, query, limit))),
		pull: (request) => Effect.promise(() => runApiEffect(pullImage(config, request))),
		buildFromContext: (request, onEvent) =>
			Effect.promise(() => runApiEffect(buildImageStream(
				config,
				{
					context_path: request.contextPath,
					dockerfile: "Dockerfile",
					tag: request.tag
				},
				onEvent
			))).pipe(Effect.asVoid),
		buildInline: (request, onEvent) =>
			Effect.promise(() => runApiEffect(buildImageStream(
				config,
				{
					dockerfile: "Dockerfile",
					dockerfile_content: request.dockerfileContent,
					tag: request.tag
				},
				onEvent
			))).pipe(Effect.asVoid),
		remove: (imageId) =>
			Effect.promise(() => runApiEffect(removeImage(config, imageId, false))).pipe(Effect.asVoid)
	};

	const imagesFeedbackService: ImagesFeedbackService = {
		error: (error) => Effect.sync(() => {
			toast.error(formatApiFailure(error));
		}),
		ok: (message) => Effect.sync(() => {
			toast.ok(message);
		})
	};

	const deleteConfirmationService: DeleteConfirmationService = (() => {
		let pendingImageId: string | null = null;
		let timerHandle: TimeoutHandle | null = null;

		const clearPending = (): void => {
			clearScheduledTimeout(timerHandle);
			timerHandle = null;
			pendingImageId = null;
			deleteConfirmId = null;
		};

		return {
			request: (imageId) =>
				Effect.sync(() => {
					if (pendingImageId === imageId) {
						clearPending();
						return true;
					}

					clearScheduledTimeout(timerHandle);
					pendingImageId = imageId;
					deleteConfirmId = imageId;
					timerHandle = scheduleTimeout(() => {
						pendingImageId = null;
						deleteConfirmId = null;
						timerHandle = null;
					}, 3000);
					return false;
				}),
			clear: Effect.sync(() => {
				clearPending();
			})
		};
	})();

	const imagesPanelLayer = Layer.mergeAll(
		Layer.succeed(ImagesApiService, imagesApiService),
		Layer.succeed(ImagesFeedbackService, imagesFeedbackService),
		Layer.succeed(DeleteConfirmationService, deleteConfirmationService)
	);

	const runImagesProgram = <A>(program: Effect.Effect<A, unknown>): Promise<A> => Effect.runPromise(program);

	const refreshImagesProgram = (options?: { force?: boolean }): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* ImagesApiService;
			const feedback = yield* ImagesFeedbackService;

			yield* Effect.sync(() => {
				loading = true;
			});

			try {
				const listedImages = yield* api.list(options);
				yield* Effect.sync(() => {
					images = listedImages;
				});
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					loading = false;
				});
			}
		}).pipe(Effect.provide(imagesPanelLayer));

	const runImageSearchProgram = (queryOverride?: string): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* ImagesApiService;
			const query = (queryOverride ?? createPullSearchQuery).trim();

			yield* Effect.sync(() => {
				createPullSearchError = "";
			});

			if (query.length === 0) {
				yield* Effect.sync(() => {
					createPullSearchResults = [];
					createPullSelectedImage = "";
					createPullSearchError = "Search query is required.";
				});
				return;
			}

			yield* Effect.sync(() => {
				createPullSearchLoading = true;
			});

			try {
				const results = yield* api.search(query, 25);
				yield* Effect.sync(() => {
					createPullSearchResults = results;
					const exactMatch = results.find((result) => result.name === createPullImage.trim()) ?? results[0] ?? null;
					createPullSelectedImage = exactMatch?.name ?? "";
					if (exactMatch !== null) {
						createPullImage = exactMatch.name;
					}
					if (results.length === 0) {
						createPullSearchError = `No Docker Hub results for ${query}.`;
					}
				});
			} catch (error) {
				yield* Effect.sync(() => {
					createPullSearchResults = [];
					createPullSelectedImage = "";
					createPullSearchError = formatApiFailure(error);
				});
			} finally {
				yield* Effect.sync(() => {
					createPullSearchLoading = false;
				});
			}
		}).pipe(Effect.provide(imagesPanelLayer));

	const handleBuildStreamEvent = (
		event: StreamEvent,
		onError: (errorMessage: string) => void
	): Effect.Effect<void> =>
		Effect.sync(() => {
			if ((event.event === "stdout" || event.event === "stderr") && event.data.length > 0) {
				appendCreateLog(event.data);
			}
			if (event.event === "error") {
				onError(event.data.trim());
			}
			if (event.event === "done" && event.data.trim().length > 0) {
				appendCreateLog(`Image ready: ${event.data.trim()}`);
			}
		});

	const pullImageProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* ImagesApiService;
			const feedback = yield* ImagesFeedbackService;

			const imageName = createPullImage.trim();
			const imageTag = createPullTag.trim();
			if (imageName.length === 0) {
				throw new Error("Image name is required for pull.");
			}

			const resolvedImage = imageTag.length > 0 ? `${imageName}:${imageTag}` : imageName;
			yield* Effect.sync(() => {
				createResolvedImage = resolvedImage;
				createStep = "Pulling image";
				appendCreateLog(`Pulling ${resolvedImage}`);
			});

			const pulled = yield* api.pull({
				image: imageName,
				tag: imageTag || undefined
			});

			yield* Effect.sync(() => {
				appendCreateLog(pulled.output);
				appendCreateLog(`Image ready: ${pulled.image}`);
			});
			yield* feedback.ok(`Image ready: ${pulled.image}`);
		}).pipe(Effect.provide(imagesPanelLayer));

	const buildImageFromContextProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* ImagesApiService;
			const feedback = yield* ImagesFeedbackService;

			const contextPath = createBuildContextPath.trim();
			const tag = createBuildTag.trim();
			if (contextPath.length === 0) {
				throw new Error("Context path is required for image build.");
			}
			if (tag.length === 0) {
				throw new Error("Image tag is required for image build.");
			}

			yield* Effect.sync(() => {
				createResolvedImage = tag;
				createStep = "Building image";
				appendCreateLog(`Building ${tag} from ${contextPath}`);
			});

			let buildError = "";
			yield* api.buildFromContext(
				{ contextPath, tag },
				(event) => {
					Effect.runSync(handleBuildStreamEvent(event, (errorMessage) => {
						buildError = errorMessage;
					}));
				}
			);

			if (buildError.length > 0) {
				throw new Error(buildError);
			}

			yield* feedback.ok(`Image ready: ${tag}`);
		}).pipe(Effect.provide(imagesPanelLayer));

	const buildImageInlineProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* ImagesApiService;
			const feedback = yield* ImagesFeedbackService;

			const dockerfileContent = createInlineContent.trim();
			const tag = createInlineTag.trim();
			if (dockerfileContent.length === 0) {
				throw new Error("Dockerfile content is required.");
			}
			if (tag.length === 0) {
				throw new Error("Image tag is required for inline build.");
			}

			yield* Effect.sync(() => {
				createResolvedImage = tag;
				createStep = "Building image";
				appendCreateLog(`Building ${tag} from inline Dockerfile`);
			});

			let buildError = "";
			yield* api.buildInline(
				{ dockerfileContent, tag },
				(event) => {
					Effect.runSync(handleBuildStreamEvent(event, (errorMessage) => {
						buildError = errorMessage;
					}));
				}
			);

			if (buildError.length > 0) {
				throw new Error(buildError);
			}

			yield* feedback.ok(`Image ready: ${tag}`);
		}).pipe(Effect.provide(imagesPanelLayer));

	const submitCreateProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const feedback = yield* ImagesFeedbackService;

			const valid = yield* Effect.sync(() => validateCreateForm());
			if (!valid) return;

			yield* Effect.sync(() => {
				createLoading = true;
				resetPipelineState();
				createStep = "Preparing";
			});

			try {
				if (createMethod === "pull") {
					yield* pullImageProgram();
				}
				if (createMethod === "build-context") {
					yield* buildImageFromContextProgram();
				}
				if (createMethod === "build-inline") {
					yield* buildImageInlineProgram();
				}

				yield* Effect.sync(() => {
					createStep = "Done";
				});
				yield* invalidateImagesCache(config);
				yield* refreshImagesProgram();
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					createLoading = false;
				});
			}
		}).pipe(Effect.provide(imagesPanelLayer));

	const removeImageProgram = (image: ImageSummary): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* ImagesApiService;
			const feedback = yield* ImagesFeedbackService;
			const deleteConfirmation = yield* DeleteConfirmationService;

			yield* Effect.sync(() => {
				deletingId = image.id;
			});

			try {
				yield* deleteConfirmation.clear;
				yield* api.remove(image.id);
				yield* invalidateImagesCache(config);
				yield* feedback.ok("Image removed.");
				yield* refreshImagesProgram();
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					deletingId = null;
				});
			}
		}).pipe(Effect.provide(imagesPanelLayer));

	const submitDeleteProgram = (image: ImageSummary): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const deleteConfirmation = yield* DeleteConfirmationService;

			const confirmed = yield* deleteConfirmation.request(image.id);
			if (!confirmed) {
				return;
			}

			yield* removeImageProgram(image);
		}).pipe(Effect.provide(imagesPanelLayer));

	const refreshImages = (options?: { force?: boolean }): Promise<void> => runImagesProgram(refreshImagesProgram(options));

	const runImageSearch = (queryOverride?: string): Promise<void> =>
		runImagesProgram(runImageSearchProgram(queryOverride));

	function selectPullSearchImage(imageName: string): void {
		createPullSelectedImage = imageName;
		if (imageName.trim().length > 0) {
			createPullImage = imageName;
		}
	}

	const submitCreate = (): Promise<void> => runImagesProgram(submitCreateProgram());

	const submitDelete = (image: ImageSummary): Promise<void> => runImagesProgram(submitDeleteProgram(image));

	$effect(() => {
		return () => {
			void runImagesProgram(deleteConfirmationService.clear);
		};
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
		<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshImages({ force: true })} disabled={loading}>
			{#if loading}
				<svg class="btn-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="12" height="12"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
				Refreshing...
			{:else}
				Refresh
			{/if}
		</button>
	</div>

	<div class="images-layout">
		<section class="panel">
			<div class="panel-header">
				<span class="panel-title">Create image</span>
			</div>
			<div class="panel-body create-body">
			<div class="method-tabs">
				{#each createMethods as method}
					<button
						type="button"
						class="method-tab"
						class:method-tab--active={createMethod === method.id}
						title={method.description}
						onclick={() => {
							createMethod = method.id;
							pullImageError = "";
							buildContextPathError = "";
							buildTagError = "";
							inlineTagError = "";
							inlineContentError = "";
						}}
					>
						{method.label}
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
									pullImageError = "";
									void runImageSearch(query);
								}}
								onSelect={(option) => { selectPullSearchImage(option.value); pullImageError = ""; }}
							/>
							{#if pullImageError}
								<span class="field-inline-error">{pullImageError}</span>
							{/if}
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
							<input
								class="field"
								class:field--error={buildContextPathError}
								bind:value={createBuildContextPath}
								placeholder="apps/server"
								required
								oninput={() => buildContextPathError = ""}
							/>
							{#if buildContextPathError}
								<span class="field-inline-error">{buildContextPathError}</span>
							{/if}
							<span class="field-help">Resolved on the server inside its workspace root. Must include a <code class="inline-code">Dockerfile</code>.</span>
						</label>
						<label class="field-col">
							<span class="section-label">Output tag</span>
							<input
								class="field"
								class:field--error={buildTagError}
								bind:value={createBuildTag}
								placeholder="sandbox-app:latest"
								required
								oninput={() => buildTagError = ""}
							/>
							{#if buildTagError}
								<span class="field-inline-error">{buildTagError}</span>
							{/if}
						</label>
					</div>
				{/if}

				{#if createMethod === "build-inline"}
					<div class="form-stack">
						<label class="field-col">
							<span class="section-label">Output tag</span>
							<input
								class="field"
								class:field--error={inlineTagError}
								bind:value={createInlineTag}
								placeholder="sandbox-inline:latest"
								required
								oninput={() => inlineTagError = ""}
							/>
							{#if inlineTagError}
								<span class="field-inline-error">{inlineTagError}</span>
							{/if}
						</label>
						<div class="field-col">
							<span class="section-label">Dockerfile</span>
							<CodeEditor
								bind:value={createInlineContent}
								language="dockerfile"
								placeholder="FROM ubuntu:24.04&#10;WORKDIR /workspace"
								minHeight="14rem"
							/>
							{#if inlineContentError}
								<span class="field-inline-error">{inlineContentError}</span>
							{/if}
						</div>
					</div>
				{/if}

				<div class="create-footer">
					<button class="btn-primary" type="button" onclick={() => void submitCreate()} disabled={createLoading}>
						{#if createLoading}
							<svg class="btn-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="12" height="12"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
							{createStep}...
						{:else}
							Create image
						{/if}
					</button>
				</div>
			</div>
		</section>

		<section class="panel">
			<div class="panel-header">
				<span class="panel-title">Local images</span>
				<span class="images-count">{sortedImages.length}</span>
			</div>
			<div class="panel-body images-list-body">
				{#if loading && sortedImages.length === 0}
					<div class="images-list">
						{#each { length: 4 } as _, i}
							<div class="image-row skeleton-image-row">
								<div class="image-row-main">
									<div class="skel-tags">
										<div class="skel-tag"></div>
										<div class="skel-tag skel-tag--short"></div>
									</div>
									<div class="skel-meta">
										<div class="skel-line skel-line--size"></div>
										<div class="skel-line skel-line--date"></div>
									</div>
								</div>
								<div class="skel-btn"></div>
							</div>
						{/each}
					</div>
				{:else if sortedImages.length === 0 && !loading}
					<div class="images-empty-state">
						<div class="images-empty-icon">
							<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
								<rect x="2" y="4" width="20" height="16" rx="2"/>
								<path d="M2 8h20M8 4v4"/>
								<circle cx="8" cy="14" r="2"/><path d="m14 12 3 3-3 3"/>
							</svg>
						</div>
						<p class="images-empty-title">No local images</p>
						<p class="images-empty-sub">Pull from a registry or build from a Dockerfile to get started.</p>
					</div>
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
									<span>{formatSize(image.size)}</span>
									<span>{formatDate(image.created)}</span>
								</div>
								</div>
							<button class="btn-danger btn-xs" type="button" onclick={() => void submitDelete(image)} disabled={loading || deletingId !== null}>
								{#if deletingId === image.id}
									<svg class="btn-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="12" height="12"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
									Deleting...
								{:else if deleteConfirmId === image.id}
									Confirm delete
								{:else}
									Delete
								{/if}
							</button>
						</div>
					{/each}
					</div>
				{/if}
			</div>
		</section>
	</div>

	{#if createStep !== "Idle" || createResolvedImage || createLogs}
		<section class="panel pipeline-output-panel">
			<div class="panel-header">
				<span class="panel-title">Build output</span>
				<span class="pipeline-step">{createStep}</span>
			</div>
			<div class="panel-body">
				{#if createResolvedImage}
					<p class="pipeline-image">Image: <code>{createResolvedImage}</code></p>
				{/if}
				<pre class="pipeline-log">{stripAnsi(createLogs) || "Waiting for pipeline..."}</pre>
			</div>
		</section>
	{/if}
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
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.create-body,
	.images-list-body {
		display: flex;
		flex-direction: column;
		gap: 0.85rem;
	}

	.images-header button,
	.create-footer button,
	.image-row button {
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

	.method-tabs {
		display: flex;
		gap: 0;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		overflow: hidden;
		background: var(--bg-raised);
	}

	.method-tab {
		flex: 1;
		padding: 0.5rem 0.5rem;
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-secondary);
		background: transparent;
		border: none;
		border-right: 1px solid var(--border-dim);
		cursor: pointer;
		transition: color 0.12s, background 0.12s;
		text-align: center;
		white-space: nowrap;
	}

	.method-tab:last-child {
		border-right: none;
	}

	.method-tab:hover {
		color: var(--text-primary);
		background: var(--bg-overlay);
	}

	.method-tab--active {
		color: var(--text-primary);
		background: var(--accent-dim);
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

	.field--error {
		border-color: var(--status-error-border) !important;
	}

	.field-inline-error {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--status-error);
		margin-top: 0.1rem;
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

	.pipeline-output-panel {
		margin-top: 0;
	}

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

	/* ── Images empty state ─────────────────────────────────────────────────── */
	.images-empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.625rem;
		padding: 3rem 1.5rem;
		text-align: center;
	}

	.images-empty-icon {
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

	.images-empty-title {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.82rem;
		color: var(--text-secondary);
	}

	.images-empty-sub {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
	}

	/* ── Skeleton loading rows ───────────────────────────────────────────────── */
	@keyframes shimmer {
		0%   { background-position: 200% 0; }
		100% { background-position: -200% 0; }
	}

	.skeleton-image-row {
		pointer-events: none;
	}

	.skel-tags {
		display: flex;
		align-items: center;
		gap: 0.35rem;
	}

	.skel-tag {
		height: 18px;
		width: 7rem;
		border-radius: 3px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-tag--short { width: 4.5rem; }

	.skel-meta {
		display: flex;
		align-items: center;
		gap: 0.55rem;
		flex-wrap: wrap;
	}

	.skel-line {
		height: 8px;
		border-radius: 4px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-line--size { width: 3rem; }
	.skel-line--date { width: 8rem; }

	.skel-btn {
		height: 26px;
		width: 52px;
		border-radius: 4px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
		flex-shrink: 0;
	}

</style>
