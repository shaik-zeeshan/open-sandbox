<script lang="ts">
	import { goto } from "$app/navigation";
	import { authController, checkHealth, clearAuthNotice, signOut as signOutSession } from "$lib/auth-controller.svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import SandboxesPanel from "$lib/components/SandboxesPanel.svelte";
	import SandboxWorkspace from "$lib/components/SandboxWorkspace.svelte";
	import {
		getCachedComposeProjects,
		getCachedContainers,
		getCachedImages,
		getCachedSandboxes,
		invalidateWorkloadCaches,
		refreshCachedComposeProjects,
		refreshCachedContainers,
		refreshCachedImages,
		refreshCachedSandboxes
	} from "$lib/api-cache";
	import type { HttpClient } from "@effect/platform";
	import {
		bootstrap,
		createSandbox,
		deleteSandbox,
		formatApiFailure,
		getSetupStatus,
		login,
		removeContainer,
		resetContainer,
		resetSandbox,
		restartContainer,
		restartSandbox,
		runApiEffect,
		stopContainer,
		stopSandbox,
		type ApiFailure,
		type ComposeProjectPreview,
		type ContainerSummary,
		type ImageSummary,
		type Sandbox,
		type SandboxPortProxyConfig
	} from "$lib/api";
	import { Effect } from "effect";
	import { clientState, setAuthSession, setBaseUrl } from "$lib/stores.svelte";
	import { toast } from "$lib/toast.svelte";
	import { scheduleTimeout } from "$lib/client/browser";

	// ── Sidebar collapse ───────────────────────────────────────────────────────
	// (managed in PageShell, but we need nothing here for +page.svelte)

	// ── Auth ───────────────────────────────────────────────────────────────────
	let loginUsername = $state("");
	let loginPassword = $state("");
	let loginLoading = $state(false);
	let loginError = $state("");
	let bootstrapRequired = $state(false);
	let setupStatusCheckedBaseUrl = $state<string | null>(null);
	let endpointValue = $state(clientState.baseUrl);
	let endpointSaved = $state(false);

	// ── Data ───────────────────────────────────────────────────────────────────
	let sandboxes = $state<Sandbox[]>([]);
	let containers = $state<ContainerSummary[]>([]);
	let composeProjects = $state<ComposeProjectPreview[]>([]);
	let images = $state<ImageSummary[]>([]);
	let dataLoading = $state(false);
	type RefreshOptions = {
		includeImages?: boolean;
		showLoading?: boolean;
		notifyOnError?: boolean;
		pollingSafeRetry?: boolean;
		force?: boolean;
	};
	type ResolvedRefreshOptions = {
		includeImages: boolean;
		showLoading: boolean;
		notifyOnError: boolean;
		pollingSafeRetry: boolean;
		force: boolean;
	};
	const POLLING_REFRESH_RETRIES = 2;
	const POLLING_RETRY_DELAY_MS = 350;
	const resolveRefreshOptions = (options?: RefreshOptions): ResolvedRefreshOptions => ({
		includeImages: options?.includeImages ?? true,
		showLoading: options?.showLoading ?? true,
		notifyOnError: options?.notifyOnError ?? true,
		pollingSafeRetry: options?.pollingSafeRetry ?? false,
		force: options?.force ?? false
	});
	const runApiProgram = <A>(
		effect: Effect.Effect<A, ApiFailure, HttpClient.HttpClient>,
		options?: { notifyAuthError?: boolean }
	): Effect.Effect<A, unknown> =>
		Effect.promise(() => runApiEffect(effect, options));
	const withPollingRetry = <A>(
		program: Effect.Effect<A, unknown>,
		attemptsLeft = POLLING_REFRESH_RETRIES
	): Effect.Effect<A, unknown> =>
		program.pipe(
			Effect.catchAll((error) =>
				attemptsLeft <= 0
					? Effect.fail(error)
					: Effect.sleep(POLLING_RETRY_DELAY_MS).pipe(
							Effect.andThen(withPollingRetry(program, attemptsLeft - 1))
						)
			)
		);
	const runProgram = <A>(program: Effect.Effect<A, unknown>): Promise<A> => Effect.runPromise(program);
	let refreshPromise: Promise<void> | null = null;
	let queuedRefreshIncludeImages = false;
	let queuedRefreshShowLoading = false;
	let queuedRefreshNotifyOnError = false;
	let queuedRefreshPollingSafeRetry = false;
	let queuedRefreshForce = false;

	// ── View routing ───────────────────────────────────────────────────────────
	type ActiveWorkload = { kind: "sandbox" | "container"; id: string } | null;
	let activeWorkload = $state<ActiveWorkload>(null);
	let pendingContainerActivationId = $state<string | null>(null);
	let activeRuntimeContainerSnapshot = $state<ContainerSummary | null>(null);

	const isValidUrl = (value: string): boolean => {
		try {
			new URL(value);
			return true;
		} catch {
			return false;
		}
	};

	const endpointDirty = $derived(endpointValue !== clientState.baseUrl);
	const endpointValid = $derived(isValidUrl(endpointValue));

	const activeSandbox = $derived.by(() => {
		const currentActive = activeWorkload;
		if (currentActive === null || currentActive.kind !== "sandbox") {
			return null;
		}
		return sandboxes.find((s) => s.id === currentActive.id) ?? null;
	});
	const activeRuntimeContainer = $derived.by(() => {
		const currentActive = activeWorkload;
		if (currentActive === null || currentActive.kind !== "container") {
			return null;
		}
		return containers.find((c) => c.id === currentActive.id) ?? null;
	});
	const activeVisibleRuntimeContainer = $derived.by(() => {
		const currentActive = activeWorkload;
		if (currentActive === null || currentActive.kind !== "container") {
			return null;
		}
		return activeRuntimeContainer ?? activeRuntimeContainerSnapshot;
	});
	const activeContainer = $derived(
		activeVisibleRuntimeContainer ?? (activeSandbox ? (containers.find(c => c.id === activeSandbox.id) ?? null) : null)
	);

	$effect(() => {
		const currentActive = activeWorkload;
		if (currentActive === null || currentActive.kind !== "container") {
			activeRuntimeContainerSnapshot = null;
			pendingContainerActivationId = null;
			return;
		}
		if (activeRuntimeContainer !== null) {
			activeRuntimeContainerSnapshot = activeRuntimeContainer;
			if (pendingContainerActivationId === activeRuntimeContainer.id) {
				pendingContainerActivationId = null;
			}
		}
	});

	// ── Create form ────────────────────────────────────────────────────────────
	let showCreateForm = $state(false);
	let createName = $state("");
	let createExistingImage = $state("");
	let createRepoUrl = $state("");
	let createBranch = $state("");
	let createWorkdir = $state("");
	let createPorts = $state("");
	let createProxyConfig = $state<Record<string, SandboxPortProxyConfig>>({});
	let createLoading = $state(false);

	async function resolveSetupStatus(): Promise<void> {
		try {
			const setup = await runApiEffect(getSetupStatus({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
			bootstrapRequired = setup.bootstrap_required;
		} catch (error) {
			bootstrapRequired = false;
			loginError = formatApiFailure(error);
		}
	}

	$effect(() => {
		clientState.baseUrl;
		endpointValue = clientState.baseUrl;
	});

	function applyEndpoint(): void {
		if (!endpointValid) return;
		setBaseUrl(endpointValue.replace(/\/$/, ""));
		endpointSaved = true;
		scheduleTimeout(() => {
			endpointSaved = false;
		}, 2000);
	}

	$effect(() => {
		if (clientState.isAuthenticated) {
			setupStatusCheckedBaseUrl = null;
			return;
		}

		const notice = authController.authNotice;
		if (notice.length > 0) {
			loginError = notice;
		}
	});

	$effect(() => {
		const baseUrl = clientState.baseUrl;
		if (!clientState.authResolved || clientState.isAuthenticated) {
			return;
		}
		if (setupStatusCheckedBaseUrl === baseUrl) {
			return;
		}
		setupStatusCheckedBaseUrl = baseUrl;
		void resolveSetupStatus();
	});

	// ── Auth actions ───────────────────────────────────────────────────────────
	async function submitLogin(): Promise<void> {
		loginError = "";
		clearAuthNotice();
		if (loginUsername.trim().length === 0 || loginPassword.trim().length === 0) {
			loginError = "Username and password are required.";
			return;
		}
		loginLoading = true;
		const toastId = toast.loading(bootstrapRequired ? "Creating admin account..." : "Authenticating...");
		try {
			const r = await runApiEffect(
				bootstrapRequired
					? bootstrap({ baseUrl: clientState.baseUrl }, loginUsername, loginPassword)
					: login({ baseUrl: clientState.baseUrl }, loginUsername, loginPassword),
				{ notifyAuthError: false }
			);
			setAuthSession({
				userId: r.user_id,
				username: r.username,
				role: r.role,
				expiresAt: r.expires_at
			});
			bootstrapRequired = false;
			loginUsername = "";
			loginPassword = "";
			toast.update(toastId, "ok", "Signed in.");
			await refreshData();
		} catch (err) {
			loginError = formatApiFailure(err);
			toast.update(toastId, "error", formatApiFailure(err));
		} finally {
			loginLoading = false;
		}
	}

	async function signOut(revoke = true): Promise<void> {
		await signOutSession(revoke);
		loginUsername = "";
		loginPassword = "";
		loginError = "";
		activeWorkload = null;
		pendingContainerActivationId = null;
		activeRuntimeContainerSnapshot = null;
		sandboxes = [];
		containers = [];
		images = [];
	}

	function toggleCreateForm(): void {
		showCreateForm = !showCreateForm;
	}

	// ── Data actions ───────────────────────────────────────────────────────────
	const refreshDataProgram = (options: ResolvedRefreshOptions): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const sandboxesEffect = options.force
				? refreshCachedSandboxes(clientState.config)
				: getCachedSandboxes(clientState.config);
			const containersEffect = options.force
				? refreshCachedContainers(clientState.config)
				: getCachedContainers(clientState.config);
			const imagesEffect = options.force
				? refreshCachedImages(clientState.config)
				: getCachedImages(clientState.config);
			const composeProjectsEffect = options.force
				? refreshCachedComposeProjects(clientState.config)
				: getCachedComposeProjects(clientState.config);
			const [sb, ct, cp, img] = yield* Effect.all([
				sandboxesEffect,
				containersEffect,
				composeProjectsEffect,
				options.includeImages ? imagesEffect : Effect.succeed(images)
			]);
			sandboxes = sb;
			containers = ct;
			composeProjects = cp;
			images = img;
			const currentActive = activeWorkload;
			if (currentActive?.kind === "sandbox" && !sb.some((s) => s.id === currentActive.id)) {
				activeWorkload = null;
			}
			if (currentActive?.kind === "container" && !ct.some((c) => c.id === currentActive.id)) {
				if (pendingContainerActivationId !== currentActive.id) {
					activeWorkload = null;
					activeRuntimeContainerSnapshot = null;
				}
			}
		});

	async function refreshData(options?: RefreshOptions): Promise<void> {
		const resolved = resolveRefreshOptions(options);

		if (refreshPromise) {
			queuedRefreshIncludeImages = queuedRefreshIncludeImages || resolved.includeImages;
			queuedRefreshShowLoading = queuedRefreshShowLoading || resolved.showLoading;
			queuedRefreshNotifyOnError = queuedRefreshNotifyOnError || resolved.notifyOnError;
			queuedRefreshPollingSafeRetry = queuedRefreshPollingSafeRetry || resolved.pollingSafeRetry;
			queuedRefreshForce = queuedRefreshForce || resolved.force;
			return refreshPromise;
		}

		const baseProgram = refreshDataProgram(resolved);
		const resilientProgram = resolved.pollingSafeRetry ? withPollingRetry(baseProgram) : baseProgram;
		refreshPromise = runProgram(
			Effect.gen(function* () {
				if (resolved.showLoading) {
					dataLoading = true;
				}
				try {
					yield* resilientProgram;
				} catch (err) {
					if (resolved.notifyOnError) {
						toast.error(formatApiFailure(err));
					}
				} finally {
					if (resolved.showLoading) {
						dataLoading = false;
					}
				}
			})
		);

		try {
			await refreshPromise;
		} finally {
			refreshPromise = null;
			if (
				queuedRefreshIncludeImages ||
				queuedRefreshShowLoading ||
				queuedRefreshNotifyOnError ||
				queuedRefreshPollingSafeRetry ||
				queuedRefreshForce
			) {
				const nextIncludeImages = queuedRefreshIncludeImages;
				const nextShowLoading = queuedRefreshShowLoading;
				const nextNotifyOnError = queuedRefreshNotifyOnError;
				const nextPollingSafeRetry = queuedRefreshPollingSafeRetry;
				const nextForce = queuedRefreshForce;
				queuedRefreshIncludeImages = false;
				queuedRefreshShowLoading = false;
				queuedRefreshNotifyOnError = false;
				queuedRefreshPollingSafeRetry = false;
				queuedRefreshForce = false;
				await refreshData({
					includeImages: nextIncludeImages,
					showLoading: nextShowLoading,
					notifyOnError: nextNotifyOnError,
					pollingSafeRetry: nextPollingSafeRetry,
					force: nextForce
				});
			}
		}
	}

	async function runActionProgram<A>(
		program: Effect.Effect<A, unknown>,
		options: {
			successMessage: string;
			afterSuccess?: (result: A) => void;
			refreshOptions?: RefreshOptions;
			invalidate?: Effect.Effect<void, unknown>;
		}
	): Promise<void> {
		try {
			const result = await runProgram(program);
			options.afterSuccess?.(result);
			if (options.invalidate) {
				await runProgram(options.invalidate);
			}
			toast.ok(options.successMessage);
			await refreshData(options.refreshOptions);
		} catch (err) {
			toast.error(formatApiFailure(err));
		}
	}

	async function submitCreate(): Promise<void> {
		createLoading = true;
		const toastId = toast.loading("Creating sandbox...");
		try {
			const parseLines = (v: string) => v.split("\n").map((l) => l.trim()).filter(Boolean);
			const sandboxName = createName.trim();
			const workdir = createWorkdir.trim();
			if (sandboxName.length === 0) {
				throw new Error("Sandbox name is required.");
			}

			const resolvedImage = createExistingImage.trim();
			if (resolvedImage.length === 0) {
				throw new Error("Choose an existing image. Create one from the Images route first.");
			}

			const created = await runApiEffect(createSandbox(clientState.config, {
				name: sandboxName,
				image: resolvedImage,
				use_image_default_cmd: true,
				repo_url: createRepoUrl.trim() || undefined,
				branch: createBranch.trim() || undefined,
				workdir: workdir || undefined,
				ports: parseLines(createPorts),
				proxy_config: Object.keys(createProxyConfig).length > 0 ? createProxyConfig : undefined
			}));
			showCreateForm = false;
			createRepoUrl = "";
			createBranch = "";
			createWorkdir = "";
			createPorts = "";
			createProxyConfig = {};
			await runProgram(invalidateWorkloadCaches(clientState.config));
			await refreshData();
			toast.update(toastId, "ok", "Sandbox created.");
			await goto(`/sandboxes/${encodeURIComponent(created.id)}`);
		} catch (err) {
			toast.update(toastId, "error", formatApiFailure(err));
		} finally {
			createLoading = false;
		}
	}

	function applyPreset(name: string, image: string): void {
		createName = name;
		createExistingImage = image;
	}

	// Sandbox list actions
	async function handleRestart(id: string): Promise<void> {
		await runActionProgram(runApiProgram(restartSandbox(clientState.config, id)), {
			successMessage: "Restarted.",
			invalidate: invalidateWorkloadCaches(clientState.config)
		});
	}
	async function handleReset(id: string): Promise<void> {
		await runActionProgram(runApiProgram(resetSandbox(clientState.config, id)), {
			successMessage: "Reset.",
			invalidate: invalidateWorkloadCaches(clientState.config)
		});
	}
	async function handleResetContainer(id: string): Promise<void> {
		await runActionProgram(runApiProgram(resetContainer(clientState.config, id)), {
			successMessage: "Container reset.",
			invalidate: invalidateWorkloadCaches(clientState.config),
			afterSuccess: (result) => {
				const currentActive = activeWorkload;
				if (currentActive?.kind === "container" && currentActive.id === id) {
					activeWorkload = { kind: "container", id: result.id };
					pendingContainerActivationId = result.id;
				}
			}
		});
	}
	async function handleStop(id: string): Promise<void> {
		await runActionProgram(runApiProgram(stopSandbox(clientState.config, id)), {
			successMessage: "Stopped.",
			invalidate: invalidateWorkloadCaches(clientState.config)
		});
	}
	async function handleDelete(id: string): Promise<void> {
		await runActionProgram(runApiProgram(deleteSandbox(clientState.config, id)), {
			successMessage: "Deleted.",
			invalidate: invalidateWorkloadCaches(clientState.config),
			afterSuccess: () => {
				const currentActive = activeWorkload;
				if (currentActive?.kind === "sandbox" && currentActive.id === id) {
					activeWorkload = null;
				}
			}
		});
	}
	async function handleRestartContainer(id: string): Promise<void> {
		await runActionProgram(runApiProgram(restartContainer(clientState.config, id)), {
			successMessage: "Container restarted.",
			invalidate: invalidateWorkloadCaches(clientState.config)
		});
	}
	async function handleStopContainer(id: string): Promise<void> {
		await runActionProgram(runApiProgram(stopContainer(clientState.config, id)), {
			successMessage: "Container stopped.",
			invalidate: invalidateWorkloadCaches(clientState.config)
		});
	}
	async function handleRemoveContainer(id: string): Promise<void> {
		await runActionProgram(runApiProgram(removeContainer(clientState.config, id)), {
			successMessage: "Container removed.",
			invalidate: invalidateWorkloadCaches(clientState.config),
			afterSuccess: () => {
				const currentActive = activeWorkload;
				if (currentActive?.kind === "container" && currentActive.id === id) {
					activeWorkload = null;
					pendingContainerActivationId = null;
					activeRuntimeContainerSnapshot = null;
				}
			}
		});
	}

	function openSandbox(id: string): void {
		void goto(`/sandboxes/${encodeURIComponent(id)}`);
	}

	function openContainer(id: string): void {
		void goto(`/services/${encodeURIComponent(id)}`);
	}

	function replaceActiveContainer(id: string): void {
		activeWorkload = { kind: "container", id };
		pendingContainerActivationId = id;
	}

	// Load data after login
	$effect(() => {
		if (clientState.isAuthenticated) void refreshData();
	});

</script>

{#if !clientState.authResolved}
	<div class="auth-screen anim-fade-up">
		<div class="auth-ambient"></div>
		<div class="auth-card">
			<div class="auth-heading">
				<h1 class="auth-title">open<em>sandbox</em></h1>
				<p class="auth-desc">Checking your session...</p>
			</div>
		</div>
	</div>

{:else if !clientState.isAuthenticated}
	<!-- ── Auth Screen ──────────────────────────────────────────────────────── -->
	<div class="auth-screen anim-fade-up">
		<div class="auth-ambient"></div>
		<form class="auth-card" onsubmit={(e) => { e.preventDefault(); void submitLogin(); }}>
			<div class="auth-logo">
				<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
					<polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
					<line x1="12" y1="22.08" x2="12" y2="12"/>
				</svg>
			</div>
			<div class="auth-heading">
				<h1 class="auth-title">open<em>sandbox</em></h1>
				<p class="auth-desc">{bootstrapRequired ? "Create the first admin account for this control plane." : "Sign in with your username and password to access the control plane."}</p>
			</div>
			<div class="auth-field">
				<label class="section-label" for="endpoint">API Endpoint</label>
				<div class="auth-endpoint-row">
					<div class="auth-endpoint-input-wrap">
						<input
							id="endpoint"
							type="text"
							class="field auth-endpoint-field"
							class:auth-endpoint-field--invalid={endpointValue.length > 0 && !endpointValid}
							class:auth-endpoint-field--valid={endpointValid && endpointDirty}
							bind:value={endpointValue}
							autocapitalize="none"
							spellcheck="false"
							placeholder="http://localhost:8080"
							onkeydown={(event) => {
								if (event.key === "Enter") {
									event.preventDefault();
									applyEndpoint();
								}
							}}
						/>
						<div class="auth-endpoint-status">
							{#if endpointValue.length > 0 && !endpointValid}
								<span class="auth-endpoint-badge auth-endpoint-badge--error">Invalid URL</span>
							{:else if endpointSaved}
								<span class="auth-endpoint-badge auth-endpoint-badge--ok">Saved</span>
							{/if}
						</div>
					</div>
					<button type="button" class="btn-ghost btn-sm auth-endpoint-apply" onclick={applyEndpoint} disabled={!endpointValid || !endpointDirty}>
						Apply
					</button>
				</div>
			</div>
			<div class="auth-field">
				<label class="section-label" for="username">Username</label>
				<input id="username" type="text" class="field" bind:value={loginUsername}
					autocomplete="username" autocapitalize="none" spellcheck="false" required placeholder="admin" />
			</div>
			<div class="auth-field">
				<label class="section-label" for="password">Password</label>
				<input id="password" type="password" class="field" bind:value={loginPassword}
					autocomplete="current-password" required placeholder="••••••••" />
			</div>
			{#if loginError}<p class="alert-error anim-fade-up">{loginError}</p>{/if}
			<button type="submit" class="btn-primary auth-submit" disabled={loginLoading}>
				{loginLoading ? (bootstrapRequired ? "Creating admin..." : "Authenticating...") : (bootstrapRequired ? "Create admin account" : "Sign in")}
			</button>
			<div class="auth-footer">
				<span class="auth-version">v0.0.1</span>
				<div class="auth-health">
					<span class="auth-health-dot health-{authController.health}"></span>
					<span class="auth-health-text">{authController.healthMessage}</span>
				</div>
			</div>
		</form>
	</div>

{:else}
	<!-- ── Dashboard Shell ──────────────────────────────────────────────────── -->
	<PageShell
		health={authController.health}
		healthMessage={authController.healthMessage}
		onPing={() => void checkHealth()}
		onSignOut={() => { void signOut(); }}
		currentUsername={clientState.username}
		currentRole={clientState.role}
	>
		{#if activeSandbox || activeVisibleRuntimeContainer}
			<!-- ── Sandbox Workspace ── -->
			<SandboxWorkspace
				sandbox={activeSandbox}
				container={activeContainer}
				runtimeContainer={activeVisibleRuntimeContainer}
				config={clientState.config}
				onBack={() => { activeWorkload = null; pendingContainerActivationId = null; activeRuntimeContainerSnapshot = null; }}
				onRefresh={() => refreshData({ force: true })}
				onContainerReplaced={(id) => replaceActiveContainer(id)}
				onDeleted={() => { activeWorkload = null; pendingContainerActivationId = null; activeRuntimeContainerSnapshot = null; void refreshData({ force: true }); }}
			/>
		{:else}
			<!-- ── Sandbox List ── -->
			<SandboxesPanel
				{sandboxes}
				{containers}
				{composeProjects}
				{images}
				config={clientState.config}
				loading={dataLoading}
				onOpen={(id) => openSandbox(id)}
				onOpenContainer={(id) => openContainer(id)}
				onRestart={(id) => void handleRestart(id)}
				onReset={(id) => void handleReset(id)}
				onResetContainer={(id) => void handleResetContainer(id)}
				onStop={(id) => void handleStop(id)}
				onDelete={(id) => void handleDelete(id)}
				onRestartContainer={(id) => void handleRestartContainer(id)}
				onStopContainer={(id) => void handleStopContainer(id)}
				onRemoveContainer={(id) => void handleRemoveContainer(id)}
				onRefresh={() => void refreshData({ force: true })}
				composeHref="/compose"
				{showCreateForm}
				bind:createName
				bind:createExistingImage
				bind:createRepoUrl
				bind:createBranch
				bind:createWorkdir
				bind:createPorts
				bind:createProxyConfig
				{createLoading}
				createImageHref="/images"
				onToggleCreate={toggleCreateForm}
				onCreateSubmit={() => void submitCreate()}
				onApplyPreset={(name, image) => applyPreset(name, image)}
			/>
		{/if}
	</PageShell>
{/if}

<style>
	/* ── Auth ─────────────────────────────────────────────────────────────────── */
	.auth-screen {
		min-height: 100vh;
		display: grid;
		place-items: center;
		padding: 2rem 1rem;
		position: relative;
	}
	.auth-ambient {
		position: fixed;
		inset: 0;
		pointer-events: none;
		background: radial-gradient(ellipse 60% 50% at 50% 40%, rgba(255,255,255,0.025) 0%, transparent 70%);
	}
	.auth-card {
		position: relative;
		z-index: 1;
		width: min(100%, 22rem);
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-xl);
		padding: 2rem 1.75rem;
		box-shadow: 0 24px 64px rgba(0,0,0,0.6), 0 0 0 1px var(--border-dim);
	}
	.auth-logo {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		border: 1px solid var(--border-mid);
		color: var(--text-secondary);
	}
	.auth-heading { display: flex; flex-direction: column; gap: 0.35rem; }
	.auth-title {
		margin: 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-weight: 400;
		letter-spacing: -0.01em;
		color: var(--text-primary);
		line-height: 1.2;
	}
	.auth-title em { font-style: italic; color: var(--text-secondary); }
	.auth-desc {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
		line-height: 1.5;
	}
	.auth-field { display: flex; flex-direction: column; gap: 0.35rem; }
	.auth-endpoint-row {
		display: flex;
		align-items: flex-start;
		gap: 0.65rem;
	}
	.auth-endpoint-input-wrap {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}
	.auth-endpoint-field--invalid {
		border-color: color-mix(in srgb, var(--status-error) 60%, var(--border-mid));
	}
	.auth-endpoint-field--valid {
		border-color: color-mix(in srgb, var(--status-ok) 60%, var(--border-mid));
	}
	.auth-endpoint-status {
		min-height: 1rem;
	}
	.auth-endpoint-badge {
		display: inline-flex;
		align-items: center;
		padding: 0.12rem 0.45rem;
		border-radius: 999px;
		font-family: var(--font-mono);
		font-size: 0.6rem;
		letter-spacing: 0.04em;
	}
	.auth-endpoint-badge--error {
		background: color-mix(in srgb, var(--status-error) 16%, transparent);
		color: var(--status-error);
	}
	.auth-endpoint-badge--ok {
		background: color-mix(in srgb, var(--status-ok) 16%, transparent);
		color: var(--status-ok);
	}
	.auth-endpoint-apply {
		flex: none;
		min-width: 4.75rem;
	}
	.auth-submit { width: 100%; padding: 0.625rem 1rem; font-size: 0.72rem; }
	.auth-footer {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding-top: 0.25rem;
		border-top: 1px solid var(--border-dim);
	}
	.auth-version { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-muted); }
	.auth-health { display: flex; align-items: center; gap: 0.4rem; }
	.auth-health-dot {
		width: 5px; height: 5px; border-radius: 50%;
		background: var(--text-muted); transition: background 0.3s;
	}
	.auth-health-dot.health-ok       { background: var(--status-ok); }
	.auth-health-dot.health-error    { background: var(--status-error); }
	.auth-health-dot.health-checking { background: var(--status-warn); }
	.auth-health-text { font-family: var(--font-mono); font-size: 0.6rem; color: var(--text-muted); }

	@media (max-width: 640px) {
		.auth-endpoint-row {
			flex-direction: column;
		}

		.auth-endpoint-apply {
			width: 100%;
		}
	}

</style>
