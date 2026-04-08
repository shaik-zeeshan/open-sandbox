<script lang="ts">
	import type { Snippet } from "svelte";

	/**
	 * Checkbox — custom checkbox matching the Void Minimalism design system.
	 *
	 * Props:
	 *   checked    – bindable boolean state
	 *   label      – optional visible label text
	 *   onchange   – optional callback fired after toggle
	 *   disabled   – disables interaction
	 *   labelClass – extra classes applied to the root element
	 *   children   – optional snippet for custom label content
	 */
	let {
		checked    = $bindable(false),
		label      = "",
		onchange,
		disabled   = false,
		labelClass = "",
		children
	} = $props<{
		checked?:    boolean;
		label?:      string;
		onchange?:   (v: boolean) => void;
		disabled?:   boolean;
		labelClass?: string;
		children?:   Snippet;
	}>();

	function toggle(): void {
		if (disabled) return;
		checked = !checked;
		onchange?.(checked);
	}

	function onKeydown(e: KeyboardEvent): void {
		if (e.key === " " || e.key === "Enter") {
			e.preventDefault();
			toggle();
		}
	}
</script>

<span
	class="cb-root {labelClass}"
	class:cb-disabled={disabled}
	role="checkbox"
	aria-checked={checked}
	aria-disabled={disabled}
	tabindex={disabled ? -1 : 0}
	onclick={toggle}
	onkeydown={onKeydown}
>
	<!-- hidden native input keeps form semantics & accessibility tree correct -->
	<input
		type="checkbox"
		{checked}
		{disabled}
		tabindex="-1"
		aria-hidden="true"
		class="cb-native"
		onchange={() => {}}
	/>

	<!-- visual box -->
	<span class="cb-box" class:cb-checked={checked}>
		{#if checked}
			<svg
				class="cb-mark"
				viewBox="0 0 10 8"
				fill="none"
				xmlns="http://www.w3.org/2000/svg"
				aria-hidden="true"
			>
				<polyline
					points="1,4.2 3.8,7 9,1"
					stroke="currentColor"
					stroke-width="1.6"
					stroke-linecap="round"
					stroke-linejoin="round"
				/>
			</svg>
		{/if}
	</span>

	{#if label}
		<span class="cb-label">{label}</span>
	{/if}

	{#if children}
		{@render children()}
	{/if}
</span>

<style>
	/* ── Root span ──────────────────────────────────────────────────────── */
	.cb-root {
		display: inline-flex;
		align-items: center;
		gap: 0.45rem;
		cursor: pointer;
		user-select: none;
		outline: none;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		font-weight: 400;
		color: var(--text-secondary);
		transition: color 0.1s;
	}

	.cb-root:hover:not(.cb-disabled) {
		color: var(--text-primary);
	}

	/* keyboard focus ring – same treatment as .field */
	.cb-root:focus-visible .cb-box {
		border-color: var(--border-focus);
		box-shadow: 0 0 0 3px rgba(255, 255, 255, 0.05);
	}

	.cb-disabled {
		opacity: 0.3;
		cursor: not-allowed;
	}

	/* ── Hide the native input completely ───────────────────────────────── */
	.cb-native {
		position: absolute;
		opacity: 0;
		width: 0;
		height: 0;
		pointer-events: none;
	}

	/* ── Visual box ─────────────────────────────────────────────────────── */
	.cb-box {
		position: relative;
		flex-shrink: 0;
		width: 12px;
		height: 12px;
		border: 1px solid var(--border-hi);
		border-radius: var(--radius-sm);
		background: var(--bg-raised);
		display: grid;
		place-items: center;
		transition:
			border-color 0.12s var(--ease-out),
			background   0.12s var(--ease-out),
			box-shadow   0.12s var(--ease-out);
	}

	.cb-root:hover:not(.cb-disabled) .cb-box {
		border-color: var(--border-focus);
		background: var(--bg-overlay);
	}

	/* checked state */
	.cb-box.cb-checked {
		background: var(--text-primary);
		border-color: var(--text-primary);
		box-shadow: 0 0 0 2px rgba(255, 255, 255, 0.06);
	}

	.cb-root:hover:not(.cb-disabled) .cb-box.cb-checked {
		background: var(--accent-solid);
		border-color: var(--accent-solid);
	}

	/* ── SVG checkmark ──────────────────────────────────────────────────── */
	.cb-mark {
		display: block;
		width: 10px;
		height: 8px;
		color: var(--bg-base);
		animation: cb-pop 0.18s var(--ease-spring) both;
	}

	@keyframes cb-pop {
		from {
			opacity: 0;
			transform: scale(0.5);
		}
		to {
			opacity: 1;
			transform: scale(1);
		}
	}

	/* ── Label text ─────────────────────────────────────────────────────── */
	.cb-label {
		line-height: 1;
	}
</style>
