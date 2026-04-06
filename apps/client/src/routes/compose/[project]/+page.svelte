<script lang="ts">
	import { page } from "$app/stores";
	import { authController, checkHealth, signOut } from "$lib/auth-controller.svelte";
	import ComposePanel from "$lib/components/ComposePanel.svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import { clientState } from "$lib/stores.svelte";

	const projectName = $derived($page.params.project);
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
		health={authController.health}
		healthMessage={authController.healthMessage}
		onPing={() => void checkHealth()}
		onSignOut={() => void signOut()}
		currentUsername={clientState.username}
		currentRole={clientState.role}
	>
		<ComposePanel config={clientState.config} initialProjectName={projectName} />
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
</style>
