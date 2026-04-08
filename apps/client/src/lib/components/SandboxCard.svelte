<script lang="ts">
	import { onDestroy } from "svelte";
	import type { PreviewUrl } from "$lib/api";

	let {
		name,
		image,
		status,
		containerId,
		previewUrls = [],
		createdAt = null,
		metaLabel = "",
		metaValue = "",
		isSelected,
		showReset = true,
		showActions = true,
		showDuplicate = true,
		deleteLabel = "Delete",
		deleteTitle = "Delete workload",
		animDelay = 0,
		onOpen,
		onDuplicate,
		onRestart,
		onReset,
		onStop,
		onDelete
	} = $props<{
		name: string;
		image: string;
		status: string;
		containerId: string;
		previewUrls?: PreviewUrl[];
		createdAt?: number | null;
		metaLabel?: string;
		metaValue?: string;
		isSelected: boolean;
		showReset?: boolean;
		showActions?: boolean;
		showDuplicate?: boolean;
		deleteLabel?: string;
		deleteTitle?: string;
		animDelay?: number;
		onOpen: () => void;
		onDuplicate: () => void;
		onRestart: () => void;
		onReset: () => void;
		onStop: () => void;
		onDelete: () => void;
	}>();

	const formatDate = (unixSeconds: number): string => {
		const d = new Date(unixSeconds * 1000);
		return d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) +
			" " + d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
	};

	const statusInfo = (status: string): { label: string; cls: string } => {
		const n = status.toLowerCase();
		if (n.includes("up") || n.includes("running")) return { label: "running", cls: "ok" };
		if (n.includes("exit") || n.includes("dead") || n.includes("error")) return { label: "stopped", cls: "error" };
		return { label: "idle", cls: "idle" };
	};

	type PreviewLink = {
		url: string;
		privatePort: number;
	};

	const previewLinks = (entries?: PreviewUrl[]): PreviewLink[] =>
		Array.from(
			new Map(
				(entries ?? [])
					.filter((entry) => entry.private_port > 0 && entry.url.trim().length > 0)
					.sort((a, b) => a.private_port - b.private_port)
					.map((entry) => [entry.url, { url: entry.url, privatePort: entry.private_port }])
			).values()
		);

	const st = $derived(statusInfo(status));
	const isRunning = $derived(st.cls === "ok");

	const previewPorts = $derived(previewLinks(previewUrls));

	// ── Per-action loading state ───────────────────────────────────────────────
	// Tracks which action is currently in progress. Only one at a time.
	let actionInProgress = $state<"stop" | "start" | "restart" | "reset" | "delete" | null>(null);

	// ── Delete confirmation state (two-click with 3s timeout) ─────────────────
	let deleteConfirmPending = $state(false);
	let deleteConfirmTimer = $state<ReturnType<typeof setTimeout> | null>(null);

	function armDeleteConfirm(e: MouseEvent) {
		e.stopPropagation();
		if (actionInProgress !== null) return;
		deleteConfirmPending = true;
		deleteConfirmTimer = setTimeout(() => {
			deleteConfirmPending = false;
			deleteConfirmTimer = null;
		}, 3000);
	}

	async function confirmDelete(e: MouseEvent) {
		e.stopPropagation();
		if (deleteConfirmTimer !== null) {
			clearTimeout(deleteConfirmTimer);
			deleteConfirmTimer = null;
		}
		deleteConfirmPending = false;
		menuOpen = false;
		actionInProgress = "delete";
		try {
			await Promise.resolve(onDelete());
		} finally {
			actionInProgress = null;
		}
	}

	// Unified action runner: wraps a callback with loading state tracking.
	function makeActionHandler(
		key: "stop" | "start" | "restart" | "reset",
		fn: () => void,
		closeMenuOnRun = false
	) {
		return async (e: MouseEvent) => {
			e.stopPropagation();
			if (actionInProgress !== null) return;
			if (closeMenuOnRun) menuOpen = false;
			actionInProgress = key;
			try {
				await Promise.resolve(fn());
			} finally {
				actionInProgress = null;
			}
		};
	}

	// ── Dropdown menu state ────────────────────────────────────────────────────
	let menuOpen = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);
	let menuStyle = $state('');

	function openMenu(e: MouseEvent) {
		e.stopPropagation();
		if (!triggerEl) return;
		const r = triggerEl.getBoundingClientRect();
		menuStyle = `top:${r.bottom + 4}px;right:${window.innerWidth - r.right}px`;
		menuOpen = true;
	}

	function closeMenu() {
		menuOpen = false;
	}

	onDestroy(() => {
		if (deleteConfirmTimer !== null) {
			clearTimeout(deleteConfirmTimer);
			deleteConfirmTimer = null;
		}
	});

	// Svelte action: mounts content into document.body so it's outside the table
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return {
			destroy() { node.remove(); }
		};
	}
</script>

<svelte:document onclick={closeMenu} />

{#if menuOpen}
	<!-- Menu appended to <body> via portal action — completely outside the table,
	     so no row/cell onclick can ever intercept it. -->
	<div
		use:portal
		class="menu-portal"
		role="menu"
		tabindex="-1"
		style={menuStyle}
		onclick={(e) => e.stopPropagation()}
		onkeydown={(e) => e.key === 'Escape' && closeMenu()}
	>
		<button class="menu-item" role="menuitem" type="button" onclick={(e) => { e.stopPropagation(); menuOpen = false; onOpen(); }}>
			<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
			Open
		</button>
		{#if showDuplicate}
			<button
				class="menu-item"
				role="menuitem"
				type="button"
				disabled={actionInProgress !== null}
				onclick={(e) => {
					e.stopPropagation();
					menuOpen = false;
					onDuplicate();
				}}
			>
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
				Duplicate
			</button>
		{/if}
		<button
			class="menu-item"
			role="menuitem"
			type="button"
			disabled={actionInProgress !== null}
			onclick={makeActionHandler("restart", onRestart, true)}
		>
			{#if actionInProgress === "restart"}
				<svg class="spinner" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				Restarting...
			{:else}
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
				Restart
			{/if}
		</button>
		{#if showReset}
			<button
				class="menu-item"
				role="menuitem"
				type="button"
				disabled={actionInProgress !== null}
				onclick={makeActionHandler("reset", onReset, true)}
			>
				{#if actionInProgress === "reset"}
					<svg class="spinner" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
					Resetting...
				{:else}
					<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/></svg>
					Reset
				{/if}
			</button>
		{/if}
		<div class="menu-sep" role="separator"></div>
		{#if deleteConfirmPending}
			<button
				class="menu-item menu-item--danger menu-item--confirm"
				role="menuitem"
				type="button"
				disabled={actionInProgress === "delete"}
				onclick={confirmDelete}
			>
				{#if actionInProgress === "delete"}
					<svg class="spinner" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
					Deleting...
				{:else}
					<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/></svg>
					Confirm?
				{/if}
			</button>
		{:else}
			<button
				class="menu-item menu-item--danger"
				role="menuitem"
				type="button"
				disabled={actionInProgress !== null}
				onclick={armDeleteConfirm}
			>
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/></svg>
				{deleteLabel}
			</button>
		{/if}
	</div>
{/if}

<tr
	class="row {isSelected ? 'row--selected' : ''} anim-fade-up"
	style="animation-delay: {animDelay}s"
>
	<!-- Name — clickable -->
	<td class="cell cell-name cell-open" role="button" tabindex="0"
		onclick={onOpen} onkeydown={(e) => e.key === 'Enter' && onOpen()}>
		<div class="name-cell">
			<div class="dot-wrap">
				<span class="dot dot--{st.cls}"></span>
				{#if st.cls === 'ok'}<span class="dot-ring"></span>{/if}
			</div>
			<div class="name-stack">
				<span class="name-text">{name}</span>
				{#if metaLabel && metaValue}
					<span class="name-sub"><span class="name-sub-key">{metaLabel}</span>{metaValue}</span>
				{/if}
			</div>
		</div>
	</td>

	<!-- Image — clickable -->
	<td class="cell cell-image cell-open" role="button" tabindex="-1"
		onclick={onOpen}>
		<span class="image-text" title={image}>{image}</span>
	</td>

	<!-- Status — clickable -->
	<td class="cell cell-status cell-open" role="button" tabindex="-1"
		onclick={onOpen}>
		<span class="badge badge--{st.cls}">{st.label}</span>
	</td>

	<!-- Ports — NOT clickable (links inside) -->
	<td class="cell cell-ports">
		<div class="chips-row">
			{#each previewPorts as preview}
				<a
					class="port-chip"
					href={preview.url}
					target="_blank"
					rel="noreferrer"
					title={preview.url}
				>:{preview.privatePort}</a>
			{/each}
			{#if previewPorts.length === 0}
				<span class="nil">—</span>
			{/if}
		</div>
	</td>

	<!-- Created — clickable -->
	<td class="cell cell-created cell-open" role="button" tabindex="-1"
		onclick={onOpen}>
		{#if createdAt !== null && createdAt > 0}
			<span class="date-text">{formatDate(createdAt)}</span>
		{:else}
			<span class="nil">—</span>
		{/if}
	</td>

	<!-- Container ID — clickable -->
	<td class="cell cell-id cell-open" role="button" tabindex="-1"
		onclick={onOpen}>
		<span class="id-text">{containerId.slice(0, 12)}</span>
	</td>

	<!-- Actions -->
	<td class="cell cell-actions">
		{#if showActions}
		<div class="actions">

			<!-- Stop (when running) / Start (when stopped/idle) -->
			{#if isRunning}
				<button
					class="act {actionInProgress === 'stop' ? 'act--loading' : ''}"
					type="button"
					disabled={actionInProgress !== null}
					onclick={makeActionHandler("stop", onStop)}
					title="Stop"
				>
					{#if actionInProgress === "stop"}
						<svg class="spinner" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
						Stopping...
					{:else}
						<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
							<rect x="3" y="3" width="18" height="18" rx="2"/>
						</svg>
						Stop
					{/if}
				</button>
			{:else}
				<button
					class="act {actionInProgress === 'start' ? 'act--loading' : ''}"
					type="button"
					disabled={actionInProgress !== null}
					onclick={makeActionHandler("start", onRestart)}
					title="Start"
				>
					{#if actionInProgress === "start"}
						<svg class="spinner" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
						Starting...
					{:else}
						<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
							<polygon points="5 3 19 12 5 21 5 3"/>
						</svg>
						Start
					{/if}
				</button>
			{/if}

				<!-- More menu trigger -->
			<button
				bind:this={triggerEl}
				class="act act--icon {menuOpen ? 'act--icon-active' : ''}"
				type="button"
				title="More actions"
				aria-label="More actions"
				aria-haspopup="menu"
				aria-expanded={menuOpen}
				onclick={openMenu}
			>
				<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
					<circle cx="12" cy="5" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="12" cy="19" r="1"/>
				</svg>
			</button>

		</div>
		{:else}
			<span class="nil">—</span>
		{/if}
	</td>
</tr>

<style>
	/* ── Row ──────────────────────────────────────────────────────────────────── */
	.row {
		background: var(--bg-surface);
	}

	.row--selected {
		background: var(--bg-raised);
	}

	/* Cells that open the sandbox on click */
	.cell-open {
		cursor: pointer;
	}

	/* ── Cells ────────────────────────────────────────────────────────────────── */
	.cell {
		padding: 0.7rem 0.875rem;
		vertical-align: middle;
		border-bottom: 1px solid var(--border-dim);
	}

	/* Accent stripe via inset box-shadow on first cell */
	.cell-name {
		box-shadow: inset 2px 0 0 transparent;
		transition: box-shadow 0.15s;
	}
	.row:hover .cell-name {
		box-shadow: inset 2px 0 0 var(--border-hi);
	}
	.row--selected .cell-name {
		box-shadow: inset 2px 0 0 var(--status-ok);
	}

	.cell-image   { max-width: 14rem; overflow: hidden; }
	.cell-status  { white-space: nowrap; }
	.cell-ports   { white-space: nowrap; }
	.cell-created { white-space: nowrap; }
	.cell-id      { white-space: nowrap; }
	.cell-actions {
		text-align: right;
		white-space: nowrap;
		padding-right: 0.75rem;
	}

	/* ── Actions row ──────────────────────────────────────────────────────────── */
	.actions {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		justify-content: flex-end;
	}

	/* ── Name cell ────────────────────────────────────────────────────────────── */
	.name-cell {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		min-width: 0;
	}

	/* Status dot */
	.dot-wrap {
		position: relative;
		width: 10px;
		height: 10px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
	}

	.dot {
		display: block;
		width: 7px;
		height: 7px;
		border-radius: 50%;
		position: relative;
		z-index: 1;
	}
	.dot--ok    { background: var(--status-ok); }
	.dot--error { background: var(--status-error); }
	.dot--idle  { background: var(--status-idle); }

	.dot-ring {
		position: absolute;
		inset: -2px;
		border-radius: 50%;
		border: 1.5px solid rgba(74, 222, 128, 0.35);
		animation: pulse-ring 2.4s ease-out infinite;
	}

	@keyframes pulse-ring {
		0%   { transform: scale(0.6); opacity: 1; }
		70%  { transform: scale(1.6); opacity: 0; }
		100% { transform: scale(1.6); opacity: 0; }
	}

	/* Name stack */
	.name-stack {
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
		min-width: 0;
	}

	.name-text {
		font-family: var(--font-mono);
		font-size: 0.74rem;
		font-weight: 500;
		color: var(--text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		line-height: 1.3;
	}

	.name-sub {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		line-height: 1.2;
	}

	.name-sub-key {
		font-size: 0.52rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		opacity: 0.6;
		margin-right: 0.28rem;
	}

	/* ── Image ────────────────────────────────────────────────────────────────── */
	.image-text {
		font-family: var(--font-mono);
		font-size: 0.67rem;
		color: var(--text-secondary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		display: block;
	}

	/* ── Status badge ─────────────────────────────────────────────────────────── */
	.badge {
		display: inline-block;
		font-family: var(--font-mono);
		font-size: 0.58rem;
		font-weight: 400;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		padding: 0.18rem 0.5rem;
		border-radius: var(--radius-sm);
		border: 1px solid transparent;
		line-height: 1.4;
	}

	.badge--ok    { color: var(--status-ok);    border-color: var(--status-ok-border);    background: var(--status-ok-bg); }
	.badge--error { color: var(--status-error); border-color: var(--status-error-border); background: var(--status-error-bg); }
	.badge--idle  { color: var(--text-muted);   border-color: var(--border-dim);          background: transparent; }

	/* ── Chips row ────────────────────────────────────────────────────────────── */
	.chips-row {
		display: flex;
		flex-wrap: nowrap;
		gap: 0.25rem;
		align-items: center;
	}

	.port-chip {
		display: inline-block;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-secondary);
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		padding: 0.12rem 0.4rem;
		text-decoration: none;
		white-space: nowrap;
		transition: color 0.1s, border-color 0.1s, background 0.1s;
		line-height: 1.5;
	}

	.port-chip:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	/* ── Date / ID ────────────────────────────────────────────────────────────── */
	.date-text {
		font-family: var(--font-mono);
		font-size: 0.63rem;
		color: var(--text-secondary);
		white-space: nowrap;
	}

	.id-text {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		letter-spacing: 0.03em;
	}

	.nil {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		opacity: 0.3;
	}

	/* ── Action buttons ───────────────────────────────────────────────────────── */
	.act {
		display: inline-flex;
		align-items: center;
		gap: 0.28rem;
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.22rem 0.5rem;
		cursor: pointer;
		transition: color 0.1s, border-color 0.1s, background 0.1s, opacity 0.1s;
		white-space: nowrap;
		line-height: 1;
	}

	.act:hover:not(:disabled) {
		color: var(--text-primary);
		border-color: var(--border-mid);
		background: var(--accent-dim);
	}

	.act:disabled {
		opacity: 0.45;
		cursor: not-allowed;
	}

	/* Loading state on action button */
	.act--loading {
		color: var(--text-muted);
		border-color: var(--border-dim);
	}

	/* Icon-only more button */
	.act--icon {
		padding: 0.22rem 0.35rem;
		color: var(--text-muted);
	}
	.act--icon:hover,
	.act--icon-active {
		color: var(--text-primary);
		border-color: var(--border-mid);
		background: var(--accent-dim);
	}

	/* ── Spinner ──────────────────────────────────────────────────────────────── */
	.spinner {
		animation: spin-action 0.7s linear infinite;
		flex-shrink: 0;
	}

	@keyframes spin-action {
		to { transform: rotate(360deg); }
	}

	/* ── Dropdown menu portal ─────────────────────────────────────────────────── */
	/* Rendered outside the table, positioned with fixed + getBoundingClientRect  */
	:global(.menu-portal) {
		position: fixed;
		z-index: 9999;
		min-width: 9rem;
		background: var(--bg-overlay);
		border: 1px solid var(--border-hi);
		border-radius: var(--radius-md);
		box-shadow: 0 8px 32px rgba(0, 0, 0, 0.55), 0 2px 8px rgba(0, 0, 0, 0.3);
		padding: 0.25rem;
		display: flex;
		flex-direction: column;
		gap: 0.05rem;
		color: inherit;
		animation: menu-in 0.1s var(--ease-snappy) both;
	}

	@keyframes menu-in {
		from { opacity: 0; transform: translateY(-4px) scale(0.97); }
		to   { opacity: 1; transform: translateY(0) scale(1); }
	}



	:global(.menu-portal .menu-item) {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		width: 100%;
		padding: 0.38rem 0.55rem;
		background: transparent;
		border: none;
		border-radius: var(--radius-sm);
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.65rem;
		text-align: left;
		cursor: pointer;
		transition: color 0.08s, background 0.08s;
		white-space: nowrap;
	}

	:global(.menu-portal .menu-item:hover:not(:disabled)) {
		color: var(--text-primary);
		background: var(--accent-dim);
	}

	:global(.menu-portal .menu-item:disabled) {
		opacity: 0.45;
		cursor: not-allowed;
	}

	:global(.menu-portal .menu-item--danger) {
		color: var(--text-muted);
	}

	:global(.menu-portal .menu-item--danger:hover:not(:disabled)) {
		color: var(--status-error);
		background: var(--status-error-bg);
	}

	:global(.menu-portal .menu-item--confirm) {
		color: var(--status-error);
	}

	:global(.menu-portal .menu-item--confirm:hover:not(:disabled)) {
		color: var(--status-error);
		background: var(--status-error-bg);
	}

	:global(.menu-portal .menu-sep) {
		height: 1px;
		background: var(--border-dim);
		margin: 0.2rem 0.25rem;
	}
</style>
