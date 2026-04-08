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

	function onKeydown(event: KeyboardEvent): void {
		if (event.key === " " || event.key === "Enter") {
			event.preventDefault();
			toggle();
		}
	}
</script>

<span class="cb-root {labelClass}">
	<input
		type="checkbox"
		bind:checked
		{disabled}
		class="cb-native"
		tabindex="-1"
		aria-hidden="true"
		onchange={() => {}}
	/>

	<span
		class="cb-ui"
		class:cb-disabled={disabled}
		role="checkbox"
		aria-checked={checked}
		aria-disabled={disabled}
		tabindex={disabled ? -1 : 0}
		onclick={toggle}
		onkeydown={onKeydown}
	>
		<span class="cb-box" aria-hidden="true">
			<span class="cb-mark"></span>
		</span>

		{#if label || children}
			<span class="cb-content">
				{#if label}
					<span class="cb-label">{label}</span>
				{/if}

				{#if children}
					{@render children()}
				{/if}
			</span>
		{/if}
	</span>
</span>

<style>
	.cb-root {
		display: inline-flex;
		position: relative;
		font-size: var(--cb-font-size, 0.7rem);
		line-height: 1.35;
	}

	.cb-ui {
		display: inline-flex;
		align-items: center;
		gap: var(--cb-gap, 1.05em);
		cursor: pointer;
		user-select: none;
		outline: none;
		font-family: var(--font-mono);
		font-size: inherit;
		line-height: inherit;
		font-weight: 400;
		color: var(--cb-label-color, var(--text-secondary));
		transition: color 0.12s var(--ease-out);
	}

	.cb-ui:hover:not(.cb-disabled) {
		color: var(--cb-label-hover, var(--text-primary));
	}

	.cb-disabled {
		opacity: 0.45;
		cursor: not-allowed;
	}

	.cb-native {
		position: absolute;
		left: 0;
		top: 0;
		opacity: 0;
		width: 1px;
		height: 1px;
		margin: 0;
		pointer-events: none;
	}

	.cb-box {
		flex-shrink: 0;
		width: var(--cb-box-size, 1.45em);
		height: var(--cb-box-size, 1.45em);
		border: 0.125em solid var(--cb-border, #4a4a4a);
		border-radius: var(--cb-radius, 0.54em);
		background: var(--cb-bg, #151515);
		display: inline-flex;
		align-items: center;
		justify-content: center;
		box-shadow: 0 0.04rem 0.18rem rgba(0, 0, 0, 0.16);
		transition:
			border-color 0.14s var(--ease-out),
			background 0.14s var(--ease-out),
			box-shadow 0.14s var(--ease-out),
			transform 0.14s var(--ease-out);
	}

	.cb-ui:hover:not(.cb-disabled) .cb-box {
		border-color: var(--cb-border-hover, #5c5c5c);
		background: var(--cb-bg-hover, #1a1a1a);
	}

	.cb-ui:focus-visible .cb-box {
		box-shadow:
			0 0 0 3px rgba(255, 255, 255, 0.12),
			0 0.04rem 0.18rem rgba(0, 0, 0, 0.16);
	}

	.cb-native:checked + .cb-ui .cb-box {
		background: var(--cb-checked-bg, #ededed);
		border-color: var(--cb-checked-bg, #ededed);
		box-shadow: 0 0.04rem 0.18rem rgba(0, 0, 0, 0.12);
	}

	.cb-native:checked + .cb-ui:hover:not(.cb-disabled) .cb-box {
		background: var(--cb-checked-hover, #f4f4f4);
		border-color: var(--cb-checked-hover, #f4f4f4);
	}

	.cb-mark {
		width: 0.4em;
		height: 0.8em;
		border-right: 0.2em solid #1f1f1f;
		border-bottom: 0.2em solid #1f1f1f;
		border-radius: 0.08em;
		opacity: 0;
		transform: translate(-4%, -12%) rotate(45deg) scale(0.62);
		transition:
			opacity 0.12s var(--ease-out),
			transform 0.16s var(--ease-out);
	}

	.cb-native:checked + .cb-ui .cb-mark {
		opacity: 1;
		transform: translate(-4%, -12%) rotate(45deg) scale(1);
	}

	.cb-content {
		display: flex;
		flex-direction: column;
		gap: 0.18rem;
		min-width: 0;
	}

	.cb-label {
		line-height: 1.35;
	}
</style>
