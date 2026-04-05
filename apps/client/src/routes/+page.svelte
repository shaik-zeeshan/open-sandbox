<script lang="ts">
	import { onMount } from "svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import SandboxesPanel from "$lib/components/SandboxesPanel.svelte";
	import SandboxWorkspace from "$lib/components/SandboxWorkspace.svelte";
	import {
		bootstrap,
		createSandbox,
		deleteSandbox,
		formatApiFailure,
		getSetupStatus,
		getSession,
		healthCheck,
		listContainers,
		listImages,
		listSandboxes,
		login,
		logout,
		refreshSession,
		removeContainer,
		resetContainer,
		resetSandbox,
		restartContainer,
		restartSandbox,
		runApiEffect,
		stopContainer,
		stopSandbox,
		type ContainerSummary,
		type ImageSummary,
		type Sandbox
	} from "$lib/api";
	import { beginAuthCheck, clearAuth, clientState, setAuthSession, setBaseUrl } from "$lib/stores.svelte";

	type HealthState = "unknown" | "checking" | "ok" | "error";

	// ── Sidebar collapse ───────────────────────────────────────────────────────
	// (managed in PageShell, but we need nothing here for +page.svelte)

	// ── Auth ───────────────────────────────────────────────────────────────────
	let health = $state<HealthState>("unknown");
	let healthMessage = $state("Waiting...");
	let healthTimer: ReturnType<typeof setTimeout> | null = null;
	let loginUsername = $state("");
	let loginPassword = $state("");
	let loginLoading = $state(false);
	let loginError = $state("");
	let bootstrapRequired = $state(false);
	let endpointValue = $state(clientState.baseUrl);
	let endpointSaved = $state(false);

	// ── Data ───────────────────────────────────────────────────────────────────
	let sandboxes = $state<Sandbox[]>([]);
	let containers = $state<ContainerSummary[]>([]);
	let images = $state<ImageSummary[]>([]);
	let dataLoading = $state(false);
	let dataError = $state("");
	let dataNotice = $state("");

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
	let createLoading = $state(false);

	// ── Health ─────────────────────────────────────────────────────────────────
	async function checkHealth(): Promise<void> {
		health = "checking";
		healthMessage = "Checking...";
		try {
			const r = await runApiEffect(healthCheck(clientState.config));
			health = r.status === "ok" ? "ok" : "error";
			healthMessage = r.status === "ok" ? "Reachable" : `Status: ${r.status}`;
		} catch (err) {
			health = "error";
			healthMessage = formatApiFailure(err);
		}
	}

	async function refreshAuthSession(): Promise<boolean> {
		try {
			const refreshed = await runApiEffect(refreshSession({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
			setAuthSession({
				userId: refreshed.user_id,
				username: refreshed.username,
				role: refreshed.role,
				expiresAt: refreshed.expires_at
			});
			return true;
		} catch {
			return false;
		}
	}

	$effect(() => {
		clientState.baseUrl;
		endpointValue = clientState.baseUrl;
	});

	$effect(() => {
		clientState.baseUrl;
		if (healthTimer) clearTimeout(healthTimer);
		healthTimer = setTimeout(() => void checkHealth(), 400);
		return () => { if (healthTimer) { clearTimeout(healthTimer); healthTimer = null; } };
	});

	function applyEndpoint(): void {
		if (!endpointValid) return;
		setBaseUrl(endpointValue.replace(/\/$/, ""));
		endpointSaved = true;
		setTimeout(() => {
			endpointSaved = false;
		}, 2000);
	}

	$effect(() => {
		if (!clientState.isAuthenticated || clientState.tokenExpiresAt === null) return;
		const delay = clientState.tokenExpiresAt * 1000 - Date.now() - 60_000;
		if (delay <= 0) {
			void (async () => {
				if (!(await refreshAuthSession())) {
					await signOut(false);
					loginError = "Session expired. Sign in again.";
				}
			})();
			return;
		}
		const timer = setTimeout(() => {
			void (async () => {
				if (!(await refreshAuthSession())) {
					await signOut(false);
					loginError = "Session expired. Sign in again.";
				}
			})();
		}, delay);
		return () => clearTimeout(timer);
	});

	async function restoreSession(): Promise<void> {
		beginAuthCheck();
		loginError = "";
		try {
			const session = await runApiEffect(getSession({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
			setAuthSession({
				userId: session.user_id,
				username: session.username,
				role: session.role,
				expiresAt: session.expires_at
			});
		} catch (err) {
			if (await refreshAuthSession()) {
				return;
			}

			const message = formatApiFailure(err);
			clearAuth();
			try {
				const setup = await runApiEffect(getSetupStatus({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
				bootstrapRequired = setup.bootstrap_required;
			} catch (setupErr) {
				bootstrapRequired = false;
				loginError = formatApiFailure(setupErr);
				return;
			}
			if (!message.startsWith("Unauthorized:")) {
				loginError = message;
			}
		}
	}

	onMount(() => {
		void restoreSession();
		const onAuthError = () => { clearAuth(); loginError = "Session expired. Sign in again."; };
		window.addEventListener("open-sandbox:auth-error", onAuthError);
		return () => window.removeEventListener("open-sandbox:auth-error", onAuthError);
	});

	// ── Auth actions ───────────────────────────────────────────────────────────
	async function submitLogin(): Promise<void> {
		loginLoading = true;
		loginError = "";
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
			await refreshData();
		} catch (err) {
			loginError = formatApiFailure(err);
		} finally {
			loginLoading = false;
		}
	}

	async function signOut(revoke = true): Promise<void> {
		if (revoke) {
			try {
				await runApiEffect(logout({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
			} catch {
				// Clear client state even if the server session is already gone.
			}
		}

		clearAuth();
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
		if (!showCreateForm) {
			dataError = "";
		}
	}

	// ── Data actions ───────────────────────────────────────────────────────────
	async function refreshData(): Promise<void> {
		dataLoading = true;
		dataError = "";
		try {
			const [sb, ct, img] = await Promise.all([
				runApiEffect(listSandboxes(clientState.config)),
				runApiEffect(listContainers(clientState.config)),
				runApiEffect(listImages(clientState.config))
			]);
			sandboxes = sb;
			containers = ct;
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
		} catch (err) {
			dataError = formatApiFailure(err);
		} finally {
			dataLoading = false;
		}
	}

	async function submitCreate(): Promise<void> {
		createLoading = true;
		dataError = "";
		dataNotice = "";
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
				ports: parseLines(createPorts)
			}));
			showCreateForm = false;
			createRepoUrl = "";
			createBranch = "";
			createWorkdir = "";
			createPorts = "";
			await refreshData();
			activeWorkload = { kind: "sandbox", id: created.id };
		} catch (err) {
			dataError = formatApiFailure(err);
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
		dataError = ""; dataNotice = "";
		try { await runApiEffect(restartSandbox(clientState.config, id)); dataNotice = "Restarted."; await refreshData(); }
		catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleReset(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try { await runApiEffect(resetSandbox(clientState.config, id)); dataNotice = "Reset."; await refreshData(); }
		catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleResetContainer(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try {
			const result = await runApiEffect(resetContainer(clientState.config, id));
			const currentActive = activeWorkload;
			if (currentActive?.kind === "container" && currentActive.id === id) {
				activeWorkload = { kind: "container", id: result.id };
				pendingContainerActivationId = result.id;
			}
			dataNotice = "Container reset.";
			await refreshData();
		}
		catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleStop(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try { await runApiEffect(stopSandbox(clientState.config, id)); dataNotice = "Stopped."; await refreshData(); }
		catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleDelete(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try {
			await runApiEffect(deleteSandbox(clientState.config, id));
			dataNotice = "Deleted.";
			const currentActive = activeWorkload;
			if (currentActive?.kind === "sandbox" && currentActive.id === id) activeWorkload = null;
			await refreshData();
		} catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleRestartContainer(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try { await runApiEffect(restartContainer(clientState.config, id)); dataNotice = "Container restarted."; await refreshData(); }
		catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleStopContainer(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try { await runApiEffect(stopContainer(clientState.config, id)); dataNotice = "Container stopped."; await refreshData(); }
		catch (err) { dataError = formatApiFailure(err); }
	}
	async function handleRemoveContainer(id: string): Promise<void> {
		dataError = ""; dataNotice = "";
		try {
			await runApiEffect(removeContainer(clientState.config, id));
			const currentActive = activeWorkload;
			if (currentActive?.kind === "container" && currentActive.id === id) {
				activeWorkload = null;
				pendingContainerActivationId = null;
				activeRuntimeContainerSnapshot = null;
			}
			dataNotice = "Container removed.";
			await refreshData();
		}
		catch (err) { dataError = formatApiFailure(err); }
	}

	function openSandbox(id: string): void {
		activeWorkload = { kind: "sandbox", id };
		dataError = ""; dataNotice = "";
	}

	function openContainer(id: string): void {
		activeWorkload = { kind: "container", id };
		activeRuntimeContainerSnapshot = containers.find((c) => c.id === id) ?? null;
		pendingContainerActivationId = null;
		dataError = ""; dataNotice = "";
	}

	function replaceActiveContainer(id: string): void {
		activeWorkload = { kind: "container", id };
		pendingContainerActivationId = id;
		dataError = "";
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
					<span class="auth-health-dot health-{health}"></span>
					<span class="auth-health-text">{healthMessage}</span>
				</div>
			</div>
		</form>
	</div>

{:else}
	<!-- ── Dashboard Shell ──────────────────────────────────────────────────── -->
	<PageShell
		{health}
		{healthMessage}
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
				onRefresh={() => refreshData()}
				onContainerReplaced={(id) => replaceActiveContainer(id)}
				onDeleted={() => { activeWorkload = null; pendingContainerActivationId = null; activeRuntimeContainerSnapshot = null; void refreshData(); }}
			/>
		{:else}
			<!-- ── Sandbox List ── -->
			<SandboxesPanel
				{sandboxes}
				{containers}
				{images}
				loading={dataLoading}
				errorMessage={dataError}
				notice={dataNotice}
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
				onRefresh={() => void refreshData()}
				composeHref="/compose"
				{showCreateForm}
				bind:createName
				bind:createExistingImage
				bind:createRepoUrl
				bind:createBranch
				bind:createWorkdir
				bind:createPorts
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
