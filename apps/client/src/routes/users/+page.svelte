<script lang="ts">
	import { authController, checkHealth, signOut } from "$lib/auth-controller.svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import UsersPanel from "$lib/components/UsersPanel.svelte";
	import { clientState } from "$lib/stores.svelte";
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
		<div class="users-page anim-fade-up">
			<div class="users-page-header">
				<div>
					<p class="section-label">Admin</p>
					<h1 class="users-page-title">User Access</h1>
				</div>
			</div>

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
