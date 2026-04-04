<script lang="ts">
	import type { ContainerSummary, Sandbox } from "$lib/api";

	type HealthState = "unknown" | "checking" | "ok" | "error";

	let {
		sandboxes,
		containers,
		selectedSandboxId,
		onSelectSandbox,
		onNewSandbox,
		health,
		healthMessage,
		onPing,
		onSignOut,
		currentUsername,
		currentRole,
		currentSection = "sandboxes",
		baseUrl,
		onBaseUrlChange,
		loading
	} = $props<{
		sandboxes: Sandbox[];
		containers: ContainerSummary[];
		selectedSandboxId: string;
		onSelectSandbox: (id: string) => void;
		onNewSandbox: () => void;
		health: HealthState;
		healthMessage: string;
		onPing: () => void;
		onSignOut: () => void;
		currentUsername: string;
		currentRole: string;
		currentSection?: "sandboxes" | "users";
		baseUrl: string;
		onBaseUrlChange: (url: string) => void;
		loading: boolean;
	}>();

	const statusOf = (sandbox: Sandbox): "ok" | "error" | "idle" => {
		const n = sandbox.status.toLowerCase();
		if (n.includes("up") || n.includes("running")) return "ok";
		if (n.includes("exit") || n.includes("dead") || n.includes("error")) return "error";
		return "idle";
	};

	const healthColor: Record<HealthState, string> = {
		ok:       "var(--status-ok)",
		error:    "var(--status-error)",
		checking: "var(--status-warn)",
		unknown:  "var(--text-muted)"
	};
	const getHealthColor = (h: HealthState): string => healthColor[h];

	const runningCount = $derived(sandboxes.filter((s: Sandbox) => statusOf(s) === "ok").length);
	const showAdminNav = $derived(currentRole === "admin");
</script>

<aside class="sidebar anim-slide-left">
	<!-- Brand -->
	<div class="sidebar-brand">
		<div class="brand-icon">
			<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
				<polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
				<line x1="12" y1="22.08" x2="12" y2="12"/>
			</svg>
		</div>
		<span class="brand-name">open<span class="brand-dash">—</span>sandbox</span>
	</div>

	<!-- Sandboxes list -->
	<div class="sandbox-list-header">
		<span class="list-label">Sandboxes</span>
		<div class="list-header-right">
			{#if sandboxes.length > 0}
				<span class="running-count">{runningCount} running</span>
			{/if}
			<button class="new-btn" type="button" onclick={onNewSandbox} title="New sandbox">
				<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
					<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
				</svg>
			</button>
		</div>
	</div>

	<nav class="sandbox-nav">
		{#if loading && sandboxes.length === 0}
			<div class="sidebar-loading">
				<div class="loading-dots">
					<span></span><span></span><span></span>
				</div>
			</div>
		{:else if sandboxes.length === 0}
			<div class="sidebar-empty">
				<p class="sidebar-empty-text">No sandboxes yet</p>
				<button class="sidebar-empty-cta" type="button" onclick={onNewSandbox}>
					Create one
				</button>
			</div>
		{:else}
			{#each sandboxes as sandbox (sandbox.id)}
				{@const st = statusOf(sandbox)}
				<button
					type="button"
					class="sandbox-item {selectedSandboxId === sandbox.id ? 'sandbox-item--active' : ''}"
					onclick={() => onSelectSandbox(sandbox.id)}
				>
					<span class="sandbox-item-dot sandbox-item-dot--{st}"></span>
					<span class="sandbox-item-name">{sandbox.name}</span>
					<span class="sandbox-item-image">{sandbox.image.split(":")[0]}</span>
				</button>
			{/each}
		{/if}
	</nav>

	<div class="sidebar-spacer"></div>

	{#if showAdminNav}
		<div class="sidebar-section-nav">
			<a class="section-link {currentSection === 'sandboxes' ? 'section-link--active' : ''}" href="/">
				Sandboxes
			</a>
			<a class="section-link {currentSection === 'users' ? 'section-link--active' : ''}" href="/users">
				Users
			</a>
		</div>
	{/if}

	<!-- Endpoint -->
	<div class="sidebar-endpoint">
		<span class="endpoint-label">Endpoint</span>
		<input
			class="endpoint-input"
			value={baseUrl}
			oninput={(e) => onBaseUrlChange((e.currentTarget as HTMLInputElement).value)}
			spellcheck={false}
		/>
	</div>

	<!-- Footer -->
	<div class="sidebar-footer">
		<div class="sidebar-user">
			<span class="sidebar-user-name">{currentUsername}</span>
			<span class="sidebar-user-role">{currentRole}</span>
		</div>
		<div class="health-row">
			<span class="health-dot" style="background: {getHealthColor(health)}"></span>
			<span class="health-text">{healthMessage}</span>
			<button class="ping-btn" type="button" onclick={onPing}>Ping</button>
		</div>
		<button class="signout-btn" type="button" onclick={onSignOut}>
			<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
				<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>
				<polyline points="16 17 21 12 16 7"/>
				<line x1="21" y1="12" x2="9" y2="12"/>
			</svg>
			Sign out
		</button>
	</div>
</aside>

<style>
	.sidebar {
		width: var(--sidebar-width);
		min-width: var(--sidebar-width);
		max-width: var(--sidebar-width);
		height: 100vh;
		position: sticky;
		top: 0;
		background: var(--bg-surface);
		border-right: 1px solid var(--border-dim);
		display: flex;
		flex-direction: column;
		overflow: hidden;
		flex-shrink: 0;
	}

	/* Brand */
	.sidebar-brand {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.875rem 0.875rem 0.75rem;
		border-bottom: 1px solid var(--border-dim);
		flex-shrink: 0;
	}
	.brand-icon {
		display: grid;
		place-items: center;
		width: 24px;
		height: 24px;
		border-radius: 4px;
		background: var(--bg-raised);
		border: 1px solid var(--border-mid);
		color: var(--text-secondary);
		flex-shrink: 0;
	}
	.brand-name {
		font-family: var(--font-mono);
		font-size: 0.7rem;
		font-weight: 500;
		color: var(--text-primary);
		letter-spacing: -0.01em;
		white-space: nowrap;
	}
	.brand-dash { color: var(--text-muted); margin: 0 0.05em; }

	/* Sandbox list header */
	.sandbox-list-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 0.875rem 0.4rem;
		flex-shrink: 0;
	}
	.list-label {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-muted);
	}
	.list-header-right {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}
	.running-count {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		color: var(--status-ok);
		opacity: 0.8;
	}
	.new-btn {
		display: grid;
		place-items: center;
		width: 20px;
		height: 20px;
		background: transparent;
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		color: var(--text-muted);
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}
	.new-btn:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	/* Sandbox nav list */
	.sandbox-nav {
		flex: 1;
		overflow-y: auto;
		padding: 0.25rem 0.5rem;
		display: flex;
		flex-direction: column;
		gap: 0.125rem;
	}
	.sidebar-loading {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 1.5rem;
	}
	.loading-dots {
		display: flex;
		gap: 0.3rem;
	}
	.loading-dots span {
		width: 4px;
		height: 4px;
		border-radius: 50%;
		background: var(--text-muted);
		animation: dotPulse 1.2s ease-in-out infinite;
	}
	.loading-dots span:nth-child(2) { animation-delay: 0.2s; }
	.loading-dots span:nth-child(3) { animation-delay: 0.4s; }
	@keyframes dotPulse {
		0%, 80%, 100% { opacity: 0.2; transform: scale(0.8); }
		40%            { opacity: 1;   transform: scale(1);   }
	}
	.sidebar-empty {
		padding: 1.5rem 0.5rem;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.5rem;
		text-align: center;
	}
	.sidebar-empty-text {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-muted);
	}
	.sidebar-empty-cta {
		background: transparent;
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.2rem 0.6rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s;
	}
	.sidebar-empty-cta:hover { color: var(--text-primary); border-color: var(--border-hi); }

	.sandbox-item {
		width: 100%;
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.45rem 0.5rem;
		border-radius: 4px;
		border: 1px solid transparent;
		background: transparent;
		cursor: pointer;
		text-align: left;
		transition: background 0.1s, border-color 0.1s;
		min-width: 0;
	}
	.sandbox-item:hover:not(.sandbox-item--active) {
		background: var(--accent-dim);
	}
	.sandbox-item--active {
		background: var(--bg-raised);
		border-color: var(--border-mid);
	}
	.sandbox-item-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.sandbox-item-dot--ok    { background: var(--status-ok); box-shadow: 0 0 5px rgba(74,222,128,0.4); }
	.sandbox-item-dot--error { background: var(--status-error); }
	.sandbox-item-dot--idle  { background: var(--status-idle); }
	.sandbox-item-name {
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		flex: 1;
	}
	.sandbox-item--active .sandbox-item-name { color: var(--text-primary); }
	.sandbox-item-image {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		color: var(--text-muted);
		white-space: nowrap;
		flex-shrink: 0;
	}

	.sidebar-spacer {
		flex: 1;
	}

	.sidebar-section-nav {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 0.35rem;
		padding: 0 0.875rem 0.875rem;
		flex-shrink: 0;
	}

	.section-link {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-height: 2rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		letter-spacing: 0.05em;
		text-decoration: none;
		text-transform: uppercase;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.section-link:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	.section-link--active {
		background: var(--bg-raised);
		border-color: var(--border-hi);
		color: var(--text-primary);
	}

	/* Endpoint */
	.sidebar-endpoint {
		padding: 0.625rem 0.875rem;
		border-top: 1px solid var(--border-dim);
		flex-shrink: 0;
	}
	.endpoint-label {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-muted);
		display: block;
		margin-bottom: 0.35rem;
	}
	.endpoint-input {
		width: 100%;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.3rem 0.45rem;
		outline: none;
		transition: border-color 0.15s, color 0.15s;
	}
	.endpoint-input:focus {
		border-color: var(--border-focus);
		color: var(--text-primary);
	}

	/* Footer */
	.sidebar-footer {
		padding: 0.625rem 0.875rem;
		border-top: 1px solid var(--border-dim);
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
		flex-shrink: 0;
	}
	.sidebar-user {
		display: flex;
		flex-direction: column;
		gap: 0.15rem;
		padding: 0.65rem 0.7rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}
	.sidebar-user-name {
		font-size: 0.78rem;
		font-weight: 500;
		color: var(--text-primary);
	}
	.sidebar-user-role {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-muted);
	}
	.health-row {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
	.health-dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		flex-shrink: 0;
		transition: background 0.3s;
	}
	.health-text {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-muted);
		flex: 1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ping-btn {
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.58rem;
		padding: 0.12rem 0.4rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s;
		flex-shrink: 0;
	}
	.ping-btn:hover { color: var(--text-primary); border-color: var(--border-hi); }

	.signout-btn {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.3rem 0.55rem;
		cursor: pointer;
		width: 100%;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}
	.signout-btn:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}
</style>
