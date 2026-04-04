<script lang="ts">
	import type { ContainerSummary, PortSummary, Sandbox } from "$lib/api";

	let {
		sandbox,
		container,
		isSelected,
		onOpen,
		onRestart,
		onReset,
		onStop,
		onDelete
	} = $props<{
		sandbox: Sandbox;
		container: ContainerSummary | null;
		isSelected: boolean;
		onOpen: () => void;
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

	const previewLinks = (ports?: PortSummary[]): string[] =>
		(ports ?? [])
			.filter(p => typeof p.public === "number" && p.public > 0 && p.type === "tcp")
			.map(p => `http://localhost:${p.public}`);

	const st = $derived(statusInfo(sandbox.status));
	const ports = $derived(previewLinks(container?.ports));
</script>

<div class="sandbox-card {isSelected ? 'sandbox-card--selected' : ''}" role="button" tabindex="0"
	onclick={onOpen}
	onkeydown={(e) => e.key === "Enter" && onOpen()}
>
	<!-- Top row: name + status -->
	<div class="card-header">
		<div class="card-name-row">
			<div class="status-dot status-dot--{st.cls}"></div>
			<span class="card-name">{sandbox.name}</span>
		</div>
		<span class="status-badge status-badge--{st.cls}">{st.label}</span>
	</div>

	<!-- Image + meta -->
	<div class="card-body">
		<div class="card-meta-row">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
				<rect x="2" y="2" width="20" height="20" rx="5"/><path d="M16 8h.01"/><path d="M8 8h.01"/><path d="M12 8h.01"/><path d="M12 16a4 4 0 0 0 4-4H8a4 4 0 0 0 4 4z"/>
			</svg>
			<span class="card-image">{sandbox.image}</span>
		</div>
		<div class="card-meta-row">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
				<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
			</svg>
			<span class="card-date">{formatDate(sandbox.created_at)}</span>
		</div>
		{#if ports.length > 0}
			<div class="card-ports">
				{#each ports as port}
					<a
						class="port-chip"
						href={port}
						target="_blank"
						rel="noreferrer"
						onclick={(e) => e.stopPropagation()}
					>
						{port.replace("http://localhost:", ":")}
					</a>
				{/each}
			</div>
		{/if}
		<div class="card-id">{sandbox.container_id.slice(0, 12)}</div>
	</div>

	<!-- Actions (shown on hover/selected) -->
	<div class="card-actions" onclick={(e) => e.stopPropagation()} role="presentation">
		<button class="action-btn" type="button" onclick={onOpen} title="Open in terminal/files">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>
			</svg>
			Open
		</button>
		<button class="action-btn" type="button" onclick={onRestart} title="Restart container">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
			</svg>
			Restart
		</button>
		<button class="action-btn" type="button" onclick={onReset} title="Reset workspace">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/>
				<path d="M3 3v5h5"/>
			</svg>
			Reset
		</button>
		<button class="action-btn" type="button" onclick={onStop} title="Stop container">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<rect x="3" y="3" width="18" height="18" rx="2"/>
			</svg>
			Stop
		</button>
		<button class="action-btn action-btn--danger" type="button" onclick={onDelete} title="Delete sandbox">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>
			</svg>
			Delete
		</button>
	</div>
</div>

<style>
	.sandbox-card {
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
		padding: 0.875rem;
		cursor: pointer;
		transition: border-color 0.18s, background 0.18s, box-shadow 0.18s, transform 0.15s var(--ease-snappy);
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
		position: relative;
		overflow: hidden;
	}
	.sandbox-card::before {
		content: '';
		position: absolute;
		inset: 0;
		border-radius: inherit;
		opacity: 0;
		transition: opacity 0.2s;
		background: radial-gradient(ellipse at 20% 20%, rgba(255,255,255,0.025) 0%, transparent 70%);
		pointer-events: none;
	}
	.sandbox-card:hover {
		border-color: var(--border-mid);
		background: var(--bg-raised);
		transform: translateY(-1px);
		box-shadow: 0 4px 24px rgba(0,0,0,0.4);
	}
	.sandbox-card:hover::before { opacity: 1; }
	.sandbox-card--selected {
		border-color: var(--border-hi);
		background: var(--bg-raised);
		box-shadow: 0 0 0 1px var(--border-mid), 0 4px 32px rgba(0,0,0,0.5);
	}
	.sandbox-card--selected::before { opacity: 1; }

	.card-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}
	.card-name-row {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		min-width: 0;
	}
	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.status-dot--ok    { background: var(--status-ok); box-shadow: 0 0 6px rgba(74,222,128,0.5); }
	.status-dot--error { background: var(--status-error); }
	.status-dot--idle  { background: var(--status-idle); }

	.card-name {
		font-family: var(--font-mono);
		font-size: 0.78rem;
		font-weight: 500;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.status-badge {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		font-weight: 400;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		padding: 0.15rem 0.5rem;
		border-radius: 3px;
		border: 1px solid transparent;
		flex-shrink: 0;
	}
	.status-badge--ok    { color: var(--status-ok); border-color: var(--status-ok-border); background: var(--status-ok-bg); }
	.status-badge--error { color: var(--status-error); border-color: var(--status-error-border); background: var(--status-error-bg); }
	.status-badge--idle  { color: var(--text-muted); border-color: var(--border-dim); background: transparent; }

	.card-body {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}
	.card-meta-row {
		display: flex;
		align-items: center;
		gap: 0.35rem;
		color: var(--text-muted);
	}
	.card-image, .card-date {
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-secondary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.card-id {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		color: var(--text-muted);
		margin-top: 0.1rem;
	}
	.card-ports {
		display: flex;
		flex-wrap: wrap;
		gap: 0.3rem;
		margin-top: 0.1rem;
	}
	.port-chip {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-secondary);
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		padding: 0.1rem 0.4rem;
		text-decoration: none;
		transition: color 0.12s, border-color 0.12s;
	}
	.port-chip:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
	}

	/* Actions — hidden until hover or selected */
	.card-actions {
		display: flex;
		align-items: center;
		gap: 0.3rem;
		flex-wrap: wrap;
		opacity: 0;
		transform: translateY(4px);
		transition: opacity 0.18s, transform 0.18s var(--ease-snappy);
		margin-top: 0.25rem;
		padding-top: 0.625rem;
		border-top: 1px solid var(--border-dim);
	}
	.sandbox-card:hover .card-actions,
	.sandbox-card--selected .card-actions {
		opacity: 1;
		transform: translateY(0);
	}
	.action-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.2rem 0.5rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
		white-space: nowrap;
	}
	.action-btn:hover {
		color: var(--text-primary);
		border-color: var(--border-mid);
		background: var(--accent-dim);
	}
	.action-btn--danger:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}
</style>
