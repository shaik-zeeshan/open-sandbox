<script lang="ts">
	import { goto } from "$app/navigation";
	import { onMount } from "svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import ImagesPanel from "$lib/components/ImagesPanel.svelte";
	import {
		formatApiFailure,
		getSession,
		healthCheck,
		logout,
		refreshSession,
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
			if (await refreshAuthSession()) {
				return;
			}

			const message = formatApiFailure(error);
			clearAuth();
			if (!message.startsWith("Unauthorized:")) {
				pageError = message;
			}
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
		return () => {
			if (healthTimer) {
				clearTimeout(healthTimer);
				healthTimer = null;
			}
		};
	});

	$effect(() => {
		if (!clientState.isAuthenticated || clientState.tokenExpiresAt === null) return;
		const delay = clientState.tokenExpiresAt * 1000 - Date.now() - 60_000;
		if (delay <= 0) {
			void (async () => {
				if (!(await refreshAuthSession())) {
					await signOut(false);
				}
			})();
			return;
		}
		const timer = setTimeout(() => {
			void (async () => {
				if (!(await refreshAuthSession())) {
					await signOut(false);
				}
			})();
		}, delay);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		if (clientState.authResolved && !clientState.isAuthenticated) void goto("/");
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
		{#if pageError}
			<div class="images-page anim-fade-up">
				<p class="alert-error">{pageError}</p>
			</div>
		{:else}
			<ImagesPanel config={clientState.config} />
		{/if}
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
		background: radial-gradient(ellipse 60% 50% at 50% 40%, rgba(255, 255, 255, 0.025) 0%, transparent 70%);
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

	.images-page {
		padding: 1.5rem;
	}
</style>
