<script lang="ts">
	import { scheduleTimeout } from "$lib/client/browser";
	import { toast, type ToastKind } from "$lib/toast.svelte";

	let dismissing = $state<Set<string>>(new Set());

	function dismiss(id: string) {
		dismissing = new Set([...dismissing, id]);
		scheduleTimeout(() => {
			toast.remove(id);
			dismissing = new Set([...dismissing].filter((x) => x !== id));
		}, 280);
	}

	const icons: Record<ToastKind, string> = {
		error: `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`,
		ok:      `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`,
		warn:    `<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`,
		loading: `<svg class="toast-spin" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>`
	};
</script>

{#if toast.list.length > 0}
	<div class="toaster" role="region" aria-label="Notifications" aria-live="polite">
		{#each toast.list as t (t.id)}
			<div
				class="toast toast--{t.kind}"
				class:toast--out={dismissing.has(t.id)}
				role={t.kind === "error" ? "alert" : "status"}
			>
				<span class="toast-icon" aria-hidden="true">{@html icons[t.kind]}</span>
				<span class="toast-msg">{t.message}</span>
				{#if t.kind !== "loading"}
					<button class="toast-close" type="button" onclick={() => dismiss(t.id)} aria-label="Dismiss">
						<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
					</button>
				{/if}
			</div>
		{/each}
	</div>
{/if}

<style>
	.toaster {
		position: fixed;
		bottom: 1.5rem;
		left: 50%;
		transform: translateX(-50%);
		z-index: 9999;
		display: flex;
		flex-direction: column-reverse;
		gap: 0.55rem;
		width: min(480px, calc(100vw - 2rem));
		pointer-events: none;
	}

	.toast {
		display: flex;
		align-items: flex-start;
		gap: 0.65rem;
		padding: 0.8rem 0.9rem;
		border-radius: var(--radius-md);
		border: 1px solid;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		line-height: 1.55;
		backdrop-filter: blur(12px);
		pointer-events: auto;
		animation: toastIn 0.3s var(--ease-snappy) both;
		max-width: 100%;
		box-shadow: 0 8px 32px rgba(0, 0, 0, 0.45);
	}

	.toast--out {
		animation: toastOut 0.28s var(--ease-out) both;
	}

	.toast--error {
		background: rgba(12, 5, 5, 0.94);
		border-color: var(--status-error-border);
		color: var(--status-error);
	}

	.toast--ok {
		background: rgba(4, 12, 7, 0.94);
		border-color: var(--status-ok-border);
		color: var(--status-ok);
	}

	.toast--warn {
		background: rgba(12, 10, 4, 0.94);
		border-color: var(--status-warn-border);
		color: var(--status-warn);
	}

	.toast--loading {
		background: rgba(6, 6, 10, 0.94);
		border-color: var(--border-mid);
		color: var(--text-secondary);
	}

	.toast-icon {
		display: flex;
		align-items: center;
		flex-shrink: 0;
		margin-top: 0.05rem;
		opacity: 0.9;
	}

	.toast-msg {
		flex: 1;
		word-break: break-word;
	}

	.toast-close {
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		background: none;
		border: none;
		cursor: pointer;
		padding: 0.1rem;
		margin-top: 0.05rem;
		color: inherit;
		opacity: 0.5;
		border-radius: var(--radius-sm);
		transition: opacity 0.15s;
	}

	.toast-close:hover {
		opacity: 1;
	}

	:global(.toast-spin) {
		animation: toastSpinAnim 0.75s linear infinite;
	}

	@keyframes toastSpinAnim {
		from { transform: rotate(0deg); }
		to   { transform: rotate(360deg); }
	}

	@keyframes toastIn {
		from {
			opacity: 0;
			transform: translateY(12px) scale(0.97);
		}
		to {
			opacity: 1;
			transform: translateY(0) scale(1);
		}
	}

	@keyframes toastOut {
		from {
			opacity: 1;
			transform: translateY(0) scale(1);
		}
		to {
			opacity: 0;
			transform: translateY(8px) scale(0.97);
		}
	}
</style>
