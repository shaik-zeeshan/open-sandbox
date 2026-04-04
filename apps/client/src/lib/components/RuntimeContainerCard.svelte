<script lang="ts">
	import type { ContainerSummary } from "$lib/api";

	let {
		container,
		onOpen,
		onRestart,
		onStop,
		onRemove
	} = $props<{
		container: ContainerSummary;
		onOpen: () => void;
		onRestart: () => void;
		onStop: () => void;
		onRemove: () => void;
	}>();

	const statusInfo = (status: string): { label: string; cls: string } => {
		const n = status.toLowerCase();
		if (n.includes("up") || n.includes("running")) return { label: "running", cls: "ok" };
		if (n.includes("exit") || n.includes("dead") || n.includes("error")) return { label: "stopped", cls: "error" };
		return { label: "idle", cls: "idle" };
	};

	const previewLinks = (ports: ContainerSummary["ports"] = []): string[] =>
		(ports ?? [])
			.filter((p) => typeof p.public === "number" && p.public > 0 && p.type === "tcp")
			.map((p) => `http://localhost:${p.public}`);

	const displayName = $derived(container.names[0] ?? container.id.slice(0, 12));
	const composeProject = $derived(container.labels["com.docker.compose.project"] ?? "");
	const composeService = $derived(container.labels["com.docker.compose.service"] ?? "");
	const ports = $derived(previewLinks(container.ports));
	const st = $derived(statusInfo(container.status));
</script>

<div class="runtime-card" role="button" tabindex="0" onclick={onOpen} onkeydown={(e) => e.key === "Enter" && onOpen()}>
	<div class="card-header">
		<div class="card-name-row">
			<div class="status-dot status-dot--{st.cls}"></div>
			<span class="card-name">{displayName}</span>
		</div>
		<span class="status-badge status-badge--{st.cls}">{st.label}</span>
	</div>

	<div class="card-body">
		<div class="card-meta-row">
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
				<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>
			</svg>
			<span class="card-image">{container.image}</span>
		</div>
		{#if composeProject}
			<div class="card-meta-row">
				<span class="meta-label">Compose</span>
				<span class="card-compose">{composeProject}{composeService ? ` / ${composeService}` : ""}</span>
			</div>
		{/if}
		{#if ports.length > 0}
			<div class="card-ports">
				{#each ports as port}
					<a class="port-chip" href={port} target="_blank" rel="noreferrer" onclick={(e) => e.stopPropagation()}>
						{port.replace("http://localhost:", ":")}
					</a>
				{/each}
			</div>
		{/if}
		<div class="card-id">{container.id.slice(0, 12)}</div>
	</div>

	<div class="card-actions" onclick={(e) => e.stopPropagation()} role="presentation">
		<button class="action-btn" type="button" onclick={onOpen}>
			Open
		</button>
		<button class="action-btn" type="button" onclick={onRestart}>
			Restart
		</button>
		<button class="action-btn" type="button" onclick={onStop}>
			Stop
		</button>
		<button class="action-btn action-btn--danger" type="button" onclick={onRemove}>
			Remove
		</button>
	</div>
</div>

<style>
	.runtime-card {
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
		padding: 0.875rem;
		display: flex;
		flex-direction: column;
		gap: 0.625rem;
	}

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

	.status-dot--ok { background: var(--status-ok); box-shadow: 0 0 6px rgba(74,222,128,0.5); }
	.status-dot--error { background: var(--status-error); }
	.status-dot--idle { background: var(--status-idle); }

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

	.status-badge--ok { color: var(--status-ok); border-color: var(--status-ok-border); background: var(--status-ok-bg); }
	.status-badge--error { color: var(--status-error); border-color: var(--status-error-border); background: var(--status-error-bg); }
	.status-badge--idle { color: var(--text-muted); border-color: var(--border-dim); background: transparent; }

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

	.card-image,
	.card-compose,
	.card-id,
	.meta-label {
		font-family: var(--font-mono);
		font-size: 0.65rem;
	}

	.card-image,
	.card-compose {
		color: var(--text-secondary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.meta-label {
		color: var(--text-muted);
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	.card-id {
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
	}

	.card-actions {
		display: flex;
		gap: 0.45rem;
		padding-top: 0.25rem;
	}

	.action-btn {
		appearance: none;
		border: 1px solid var(--border-mid);
		background: var(--bg-raised);
		color: var(--text-secondary);
		border-radius: 8px;
		padding: 0.4rem 0.6rem;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		cursor: pointer;
	}

	.action-btn--danger {
		color: var(--status-error);
		border-color: var(--status-error-border);
	}
</style>
