<script lang="ts">
	import { goto } from "$app/navigation";
	import { onMount } from "svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import UsersPanel from "$lib/components/UsersPanel.svelte";
	import {
		formatApiFailure,
		getSession,
		healthCheck,
		logout,
		runApiEffect
	} from "$lib/api";
	import { beginAuthCheck, clearAuth, clientState, setAuthSession } from "$lib/stores.svelte";

	type HealthState = "unknown" | "checking" | "ok" | "error";

	let health = $state<HealthState>("unknown");
	let healthMessage = $state("Waiting...");
	let healthTimer: ReturnType<typeof setTimeout> | null = null;
	let pageError = $state("");

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
			if (!message.startsWith("Unauthorized:")) pageError = message;
		}
	}

	async function signOut(revoke = true): Promise<void> {
		if (revoke) {
			try {
				await runApiEffect(logout({ baseUrl: clientState.baseUrl }), { notifyAuthError: false });
			} catch {}
		}
		clearAuth();
		await goto("/");
	}

	$effect(() => {
		clientState.baseUrl;
		if (healthTimer) clearTimeout(healthTimer);
		healthTimer = setTimeout(() => void checkHealth(), 400);
		return () => { if (healthTimer) { clearTimeout(healthTimer); healthTimer = null; } };
	});

	$effect(() => {
		if (!clientState.isAuthenticated || clientState.tokenExpiresAt === null) return;
		const delay = clientState.tokenExpiresAt * 1000 - Date.now();
		if (delay <= 0) { void signOut(false); return; }
		const timer = setTimeout(() => void signOut(false), delay);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		if (clientState.authResolved && !clientState.isAuthenticated) void goto("/");
	});

	onMount(() => {
		void restoreSession();
		const onAuthError = () => { clearAuth(); void goto("/"); };
		window.addEventListener("open-sandbox:auth-error", onAuthError);
		return () => window.removeEventListener("open-sandbox:auth-error", onAuthError);
	});
</script>

{#if !clientState.authResolved}
	<div class="auth-screen anim-fade-up">
		<div class="auth-ambient"></div>
		<div class="auth-card">
			<p class="auth-checking">Checking session...</p>
		</div>
	</div>
{:else if clientState.isAuthenticated}
	<PageShell
		{health}
		{healthMessage}
		onPing={() => void checkHealth()}
		onSignOut={() => void signOut()}
		currentUsername={clientState.username}
		currentRole={clientState.role}
	>
		<div class="users-page anim-fade-up">
			<div class="users-page-header">
				<div>
					<p class="section-label">Admin</p>
					<h1 class="users-page-title">User Access</h1>
				</div>
			</div>

			{#if pageError}
				<p class="alert-error">{pageError}</p>
			{/if}

			{#if clientState.role === "admin"}
				<UsersPanel
					config={clientState.config}
					currentUserId={clientState.userId}
					currentUsername={clientState.username}
				/>
			{:else}
				<section class="access-denied panel">
					<div class="panel-body access-denied-body">
						<p class="section-label">Access</p>
						<h2 class="access-denied-title">Admin access required</h2>
						<p class="access-denied-copy">Your account does not have permission to manage users.</p>
						<a class="btn-primary btn-sm access-denied-link" href="/">Return to sandboxes</a>
					</div>
				</section>
			{/if}
		</div>
	</PageShell>
{/if}

<style>
	.auth-screen {
		min-height: 100vh;
		display: grid;
		place-items: center;
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
		padding: 2rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-xl);
	}
	.auth-checking {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--text-muted);
	}

	.users-page {
		display: flex;
		flex-direction: column;
		gap: 1.5rem;
		padding: 1.5rem;
	}

	.users-page-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 1rem;
		padding-bottom: 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.users-page-title {
		margin: 0.2rem 0 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.access-denied {
		overflow: hidden;
	}

	.access-denied-body {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 0.75rem;
	}

	.access-denied-title {
		margin: 0;
		font-size: 1.1rem;
		color: var(--text-primary);
	}

	.access-denied-copy {
		margin: 0;
		font-size: 0.78rem;
		color: var(--text-secondary);
	}

	.access-denied-link {
		text-decoration: none;
	}
</style>
