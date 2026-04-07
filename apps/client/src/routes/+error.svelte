<script lang="ts">
	import { page } from "$app/state";
	import { invalidateAll, goto } from "$app/navigation";

	let retrying = $state(false);

	async function retry() {
		retrying = true;
		try {
			await invalidateAll();
		} finally {
			retrying = false;
		}
	}
</script>

<div class="error-shell">
	<div class="error-card anim-fade-up">
		<div class="error-icon anim-fade-up anim-delay-1" aria-hidden="true">
			<svg width="40" height="40" viewBox="0 0 40 40" fill="none" xmlns="http://www.w3.org/2000/svg">
				<circle cx="20" cy="20" r="17" stroke="currentColor" stroke-width="1.5" stroke-dasharray="4 2.5"/>
				<line x1="20" y1="11" x2="20" y2="22" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
				<circle cx="20" cy="27.5" r="1.25" fill="currentColor"/>
			</svg>
		</div>

		<div class="error-meta anim-fade-up anim-delay-2">
			<span class="status-badge">
				<span class="status-dot"></span>
				HTTP {page.status}
			</span>
		</div>

		<h1 class="error-title anim-fade-up anim-delay-3">
			{page.status === 404 ? "Page not found" : page.status === 403 ? "Access denied" : page.status === 500 ? "Server error" : "Something went wrong"}
		</h1>

		<p class="error-message anim-fade-up anim-delay-4">
			{page.error?.message ?? "An unexpected error occurred. The page may have moved or be temporarily unavailable."}
		</p>

		<div class="error-actions anim-fade-up anim-delay-5">
			<button
				class="btn-primary btn-sm"
				onclick={retry}
				disabled={retrying}
				aria-label="Retry loading the page"
			>
				{retrying ? "Retrying..." : "Try again"}
			</button>
			<button
				class="btn-ghost btn-sm"
				onclick={() => goto("/")}
				aria-label="Go to dashboard"
			>
				Go to dashboard
			</button>
		</div>

		<p class="error-hint anim-fade-up anim-delay-6">
			If the problem persists, check the server health or contact your administrator.
		</p>
	</div>
</div>

<style>
	.error-shell {
		min-height: calc(100vh - 48px);
		display: grid;
		place-items: center;
		padding: 1.5rem;
	}

	.error-card {
		max-width: 32rem;
		width: 100%;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-xl);
		padding: 2rem 2rem 1.75rem;
		display: flex;
		flex-direction: column;
		gap: 0;
	}

	.error-icon {
		color: var(--status-error);
		opacity: 0.7;
		margin-bottom: 1.25rem;
		display: flex;
		align-items: center;
	}

	.error-meta {
		margin-bottom: 0.625rem;
	}

	.status-badge {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		font-family: var(--font-mono);
		font-size: 0.65rem;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--status-error);
		background: var(--status-error-bg);
		border: 1px solid var(--status-error-border);
		border-radius: var(--radius-sm);
		padding: 0.2rem 0.5rem;
	}

	.status-dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		background: currentColor;
		flex-shrink: 0;
	}

	.error-title {
		margin: 0 0 0.625rem;
		font-family: var(--font-display);
		font-size: 1.6rem;
		font-weight: 400;
		font-style: italic;
		line-height: 1.2;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.error-message {
		margin: 0 0 1.5rem;
		font-family: var(--font-mono);
		font-size: 0.72rem;
		line-height: 1.7;
		color: var(--text-secondary);
	}

	.error-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-bottom: 1.25rem;
	}

	.error-hint {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.63rem;
		line-height: 1.6;
		color: var(--text-muted);
		padding-top: 1.25rem;
		border-top: 1px solid var(--border-dim);
	}
</style>
