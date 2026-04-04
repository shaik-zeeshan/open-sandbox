<script lang="ts">
	import { page } from "$app/state";
</script>

<div class="error-shell">
	<div class="error-card">
		<p class="code">{page.status}</p>
		<h1 class="title">Something went wrong</h1>
		<p class="message">{page.error?.message ?? "Unexpected route error."}</p>
	</div>
</div>

<style>
	.error-shell {
		min-height: calc(100vh - 48px);
		display: grid;
		place-items: center;
		padding: 1rem;
	}

	.error-card {
		max-width: 34rem;
		width: 100%;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: 10px;
		padding: 1.25rem;
	}

	.code {
		margin: 0 0 0.25rem;
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--status-error);
	}

	.title {
		margin: 0 0 0.35rem;
		font-family: var(--font-sans);
		font-size: 1rem;
		color: var(--text-primary);
	}

	.message {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.74rem;
		line-height: 1.6;
		color: var(--text-secondary);
	}
</style>
