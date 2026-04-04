<script lang="ts">
	import { goto } from "$app/navigation";
	import { onMount } from "svelte";
	import Sidebar from "$lib/components/Sidebar.svelte";
	import UsersPanel from "$lib/components/UsersPanel.svelte";
	import {
		formatApiFailure,
		getSession,
		healthCheck,
		listContainers,
		listSandboxes,
		logout,
		runApiEffect,
		type ContainerSummary,
		type Sandbox
	} from "$lib/api";
	import { beginAuthCheck, clearAuth, clientState, setAuthSession, setBaseUrl } from "$lib/stores.svelte";

	type HealthState = "unknown" | "checking" | "ok" | "error";

	let health = $state<HealthState>("unknown");
	let healthMessage = $state("Waiting...");
	let healthTimer: ReturnType<typeof setTimeout> | null = null;
	let pageError = $state("");

	let sandboxes = $state<Sandbox[]>([]);
	let containers = $state<ContainerSummary[]>([]);
	let dataLoading = $state(false);

	async function checkHealth(): Promise<void> {
		health = "checking";
		healthMessage = "Checking...";
		try {
			const result = await runApiEffect(healthCheck(clientState.config));
			health = result.status === "ok" ? "ok" : "error";
			healthMessage = result.status === "ok" ? "Reachable" : `Status: ${result.status}`;
		} catch (error) {
			health = "error";
			healthMessage = formatApiFailure(error);
		}
	}

	async function restoreSession(): Promise<void> {
		beginAuthCheck();
		pageError = "";
		try {
			const session = await runApiEffect(getSession({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
			setAuthSession({
				userId: session.user_id,
				username: session.username,
				role: session.role,
				expiresAt: session.expires_at
			});
		} catch (error) {
			const message = formatApiFailure(error);
			clearAuth();
			if (!message.startsWith("Unauthorized:")) {
				pageError = message;
			}
		}
	}

	async function refreshData(): Promise<void> {
		dataLoading = true;
		pageError = "";
		try {
			const [sandboxList, containerList] = await Promise.all([
				runApiEffect(listSandboxes(clientState.config)),
				runApiEffect(listContainers(clientState.config))
			]);
			sandboxes = sandboxList;
			containers = containerList;
		} catch (error) {
			pageError = formatApiFailure(error);
		} finally {
			dataLoading = false;
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
		sandboxes = [];
		containers = [];
		await goto("/");
	}

	$effect(() => {
		clientState.baseUrl;
		if (healthTimer) clearTimeout(healthTimer);
		healthTimer = setTimeout(() => void checkHealth(), 400);
		return () => {
			if (healthTimer) {
				clearTimeout(healthTimer);
				healthTimer = null;
			}
		};
	});

	$effect(() => {
		if (!clientState.isAuthenticated || clientState.tokenExpiresAt === null) {
			return;
		}

		const delay = clientState.tokenExpiresAt * 1000 - Date.now();
		if (delay <= 0) {
			void signOut(false);
			return;
		}

		const timer = setTimeout(() => {
			void signOut(false);
		}, delay);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		if (clientState.authResolved && !clientState.isAuthenticated) {
			void goto("/");
		}
	});

	$effect(() => {
		if (clientState.isAuthenticated) {
			void refreshData();
		}
	});

	onMount(() => {
		void restoreSession();
		const onAuthError = () => {
			clearAuth();
			void goto("/");
		};
		window.addEventListener("open-sandbox:auth-error", onAuthError);
		return () => window.removeEventListener("open-sandbox:auth-error", onAuthError);
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

{:else if clientState.isAuthenticated}
	<div class="dashboard-shell">
		<Sidebar
			{sandboxes}
			{containers}
			selectedSandboxId=""
			onSelectSandbox={() => void goto("/")}
			onNewSandbox={() => void goto("/")}
			{health}
			{healthMessage}
			onPing={() => void checkHealth()}
			onSignOut={() => { void signOut(); }}
			currentUsername={clientState.username}
			currentRole={clientState.role}
			currentSection="users"
			baseUrl={clientState.baseUrl}
			onBaseUrlChange={(url) => setBaseUrl(url)}
			loading={dataLoading}
		/>

		<main class="dashboard-main">
			<section class="users-page anim-fade-up">
				<div class="users-page__header">
					<div>
						<p class="section-label">Admin</p>
						<h1 class="users-page__title">User Access</h1>
						<p class="users-page__copy">Manage accounts on a dedicated page without mixing user operations into the sandbox dashboard.</p>
					</div>
					<a class="btn-ghost btn-sm users-page__back" href="/">Back to sandboxes</a>
				</div>

				{#if pageError}
					<p class="alert-error">{pageError}</p>
				{/if}

				{#if clientState.role === "admin"}
					<UsersPanel config={clientState.config} currentUserId={clientState.userId} currentUsername={clientState.username} />
				{:else}
					<section class="users-page__locked panel">
						<div class="panel-body users-page__locked-body">
							<p class="section-label">Access</p>
							<h2 class="users-page__locked-title">Admin access required</h2>
							<p class="users-page__locked-copy">Your account does not have permission to manage users.</p>
							<a class="btn-primary btn-sm users-page__locked-link" href="/">Return to sandboxes</a>
						</div>
					</section>
				{/if}
			</section>
		</main>
	</div>
{/if}

<style>
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

	.auth-heading {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.auth-title {
		margin: 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-weight: 400;
		letter-spacing: -0.01em;
		color: var(--text-primary);
		line-height: 1.2;
	}

	.auth-title em {
		font-style: italic;
		color: var(--text-secondary);
	}

	.auth-desc {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
		line-height: 1.5;
	}

	.dashboard-shell {
		display: flex;
		min-height: 100vh;
		align-items: stretch;
	}

	.dashboard-main {
		flex: 1;
		min-width: 0;
		overflow-y: auto;
		height: 100vh;
	}

	.users-page {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
		padding: 1.25rem;
	}

	.users-page__header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 1rem;
	}

	.users-page__title {
		margin: 0.2rem 0 0.5rem;
		font-size: 1.5rem;
		font-weight: 600;
		color: var(--text-primary);
	}

	.users-page__copy {
		margin: 0;
		max-width: 42rem;
		font-size: 0.78rem;
		line-height: 1.6;
		color: var(--text-secondary);
	}

	.users-page__back {
		white-space: nowrap;
	}

	.users-page__locked {
		overflow: hidden;
	}

	.users-page__locked-body {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 0.75rem;
	}

	.users-page__locked-title {
		margin: 0;
		font-size: 1.1rem;
		color: var(--text-primary);
	}

	.users-page__locked-copy {
		margin: 0;
		font-size: 0.78rem;
		color: var(--text-secondary);
	}

	.users-page__locked-link {
		text-decoration: none;
	}

	@media (max-width: 768px) {
		.dashboard-shell {
			flex-direction: column;
		}

		.dashboard-main {
			height: auto;
		}

		.users-page__header {
			flex-direction: column;
			align-items: stretch;
		}

		.users-page__back {
			width: fit-content;
		}
	}
</style>
