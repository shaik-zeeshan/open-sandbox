<script lang="ts">
	import { page } from "$app/stores";
	import { writeStorageItem } from "$lib/client/browser";

	type HealthState = "unknown" | "checking" | "ok" | "error";

	let {
		health,
		healthMessage,
		onPing,
		onSignOut,
		currentUsername,
		currentRole,
		collapsed = $bindable(false)
	} = $props<{
		health: HealthState;
		healthMessage: string;
		onPing: () => void;
		onSignOut: () => void;
		currentUsername: string;
		currentRole: string;
		collapsed?: boolean;
	}>();

	const healthColor: Record<HealthState, string> = {
		ok:       "var(--status-ok)",
		error:    "var(--status-error)",
		checking: "var(--status-warn)",
		unknown:  "var(--text-muted)"
	};
	const getHealthColor = (h: HealthState): string => healthColor[h];

	const showAdminNav = $derived(currentRole === "admin");
	const userInitial = $derived(currentUsername ? currentUsername[0].toUpperCase() : "?");

	const currentPath = $derived($page.url.pathname);

	function toggleCollapsed() {
		collapsed = !collapsed;
		writeStorageItem("sidebar-collapsed", String(collapsed));
	}

	const navItems = $derived([
		{
			id: "sandboxes",
			label: "Workloads",
			href: "/",
			adminOnly: false,
			icon: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>`
		},
		{
			id: "images",
			label: "Images",
			href: "/images",
			adminOnly: false,
			icon: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>`
		},
		{
			id: "compose",
			label: "Compose",
			href: "/compose",
			adminOnly: false,
			icon: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/><path d="M7 8h2M7 12h2M11 8h6M11 12h6"/></svg>`
		},
		{
			id: "users",
			label: "Users",
			href: "/users",
			adminOnly: true,
			icon: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>`
		},
		{
			id: "settings",
			label: "Settings",
			href: "/settings",
			adminOnly: false,
			icon: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>`
		}
	].filter(item => !item.adminOnly || showAdminNav));

	const isActive = (href: string): boolean => {
		if (href === "/") {
			return currentPath === "/" || currentPath.startsWith("/sandboxes/") || currentPath.startsWith("/services/");
		}
		return currentPath.startsWith(href);
	};
</script>

<aside class="sidebar anim-slide-left" class:sidebar--collapsed={collapsed}>
	<!-- Brand + toggle -->
	<div class="sidebar-brand">
		{#if !collapsed}
			<div class="brand-icon">
				<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
					<polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
					<line x1="12" y1="22.08" x2="12" y2="12"/>
				</svg>
			</div>
			<span class="brand-name">open<span class="brand-dash">—</span>sandbox</span>
		{:else}
			<div class="brand-icon brand-icon--lg">
				<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
					<polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
					<line x1="12" y1="22.08" x2="12" y2="12"/>
				</svg>
			</div>
		{/if}
		<button class="collapse-btn" type="button" onclick={toggleCollapsed} title={collapsed ? "Expand sidebar" : "Collapse sidebar"}>
			{#if collapsed}
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<polyline points="9 18 15 12 9 6"/>
				</svg>
			{:else}
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<polyline points="15 18 9 12 15 6"/>
				</svg>
			{/if}
		</button>
	</div>

	<!-- Navigation -->
	<nav class="sidebar-nav">
		{#each navItems as item}
			<a
				class="nav-link"
				class:nav-link--active={isActive(item.href)}
				href={item.href}
				title={collapsed ? item.label : undefined}
			>
				<span class="nav-icon">{@html item.icon}</span>
				{#if !collapsed}
					<span class="nav-label">{item.label}</span>
				{/if}
			</a>
		{/each}
	</nav>

	<div class="sidebar-spacer"></div>

	<!-- Health indicator (icon only when collapsed) -->
	<div class="health-section" class:health-section--collapsed={collapsed}>
		<span class="health-dot" style="background: {getHealthColor(health)}"></span>
		{#if !collapsed}
			<span class="health-text">{healthMessage}</span>
			<button class="ping-btn" type="button" onclick={onPing}>Ping</button>
		{:else}
			<button class="ping-btn ping-btn--icon" type="button" onclick={onPing} title="Ping server">
				<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07A19.5 19.5 0 0 1 4.69 9.13 19.79 19.79 0 0 1 1.61 0.52 2 2 0 0 1 3.62 0h3a2 2 0 0 1 2 1.72c.127.96.361 1.903.7 2.81a2 2 0 0 1-.45 2.11L7.91 7.91a16 16 0 0 0 6.11 6.11l.97-.97a2 2 0 0 1 2.11-.45c.907.339 1.85.573 2.81.7A2 2 0 0 1 22 16.92z"/>
				</svg>
			</button>
		{/if}
	</div>

	<!-- Footer -->
	<div class="sidebar-footer">
		{#if !collapsed}
			<div class="user-card">
				<div class="user-avatar">{userInitial}</div>
				<div class="user-info">
					<span class="user-name">{currentUsername}</span>
					<span class="user-role">{currentRole}</span>
				</div>
			</div>
			<button class="signout-btn" type="button" onclick={onSignOut}>
				<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>
					<polyline points="16 17 21 12 16 7"/>
					<line x1="21" y1="12" x2="9" y2="12"/>
				</svg>
				Sign out
			</button>
		{:else}
			<div class="user-avatar-solo" title={currentUsername}>{userInitial}</div>
			<button class="signout-btn--icon" type="button" onclick={onSignOut} title="Sign out">
				<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>
					<polyline points="16 17 21 12 16 7"/>
					<line x1="21" y1="12" x2="9" y2="12"/>
				</svg>
			</button>
		{/if}
	</div>
</aside>

<style>
	.sidebar {
		width: var(--sidebar-width);
		min-width: var(--sidebar-width);
		max-width: var(--sidebar-width);
		height: calc(100vh - 20px);
		position: fixed;
		top: 10px;
		left: 10px;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-xl);
		box-shadow:
			0 4px 24px rgba(0, 0, 0, 0.4),
			0 1px 4px rgba(0, 0, 0, 0.2),
			inset 0 1px 0 rgba(255, 255, 255, 0.03);
		display: flex;
		flex-direction: column;
		overflow: hidden;
		flex-shrink: 0;
		z-index: 40;
		transition: width 0.22s var(--ease-snappy), min-width 0.22s var(--ease-snappy), max-width 0.22s var(--ease-snappy);
	}

	.sidebar--collapsed {
		width: 52px;
		min-width: 52px;
		max-width: 52px;
	}

	/* Brand */
	.sidebar-brand {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.75rem 0.75rem 0.625rem;
		border-bottom: 1px solid var(--border-dim);
		flex-shrink: 0;
		min-height: 48px;
	}

	.sidebar--collapsed .sidebar-brand {
		justify-content: center;
		padding: 0.75rem 0;
		gap: 0;
		flex-direction: column;
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

	.brand-icon--lg {
		width: 28px;
		height: 28px;
	}

	.brand-name {
		font-family: var(--font-mono);
		font-size: 0.7rem;
		font-weight: 500;
		color: var(--text-primary);
		letter-spacing: -0.01em;
		white-space: nowrap;
		flex: 1;
		overflow: hidden;
		opacity: 1;
		transition: opacity 0.15s;
	}

	.brand-dash { color: var(--text-muted); margin: 0 0.05em; }

	.collapse-btn {
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: 4px;
		color: var(--text-muted);
		cursor: pointer;
		flex-shrink: 0;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.collapse-btn:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	.sidebar--collapsed .collapse-btn {
		margin-top: 0.35rem;
		width: 28px;
		height: 22px;
	}

	/* Navigation */
	.sidebar-nav {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
		padding: 0.625rem 0.5rem;
		flex-shrink: 0;
	}

	.nav-link {
		display: flex;
		align-items: center;
		gap: 0.625rem;
		padding: 0.5rem 0.625rem;
		border-radius: var(--radius-sm);
		border: 1px solid transparent;
		color: var(--text-secondary);
		text-decoration: none;
		font-family: var(--font-mono);
		font-size: 0.72rem;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
		white-space: nowrap;
		overflow: hidden;
	}

	.nav-link:hover:not(.nav-link--active) {
		color: var(--text-primary);
		background: var(--accent-dim);
	}

	.nav-link--active {
		color: var(--text-primary);
		background: var(--bg-raised);
		border-color: var(--border-mid);
	}

	.nav-icon {
		display: grid;
		place-items: center;
		flex-shrink: 0;
		width: 16px;
		height: 16px;
		color: inherit;
	}

	.nav-label {
		overflow: hidden;
		white-space: nowrap;
	}

	.sidebar--collapsed .nav-link {
		justify-content: center;
		padding: 0.5rem;
		gap: 0;
	}

	.sidebar-spacer {
		flex: 1;
	}

	/* Health */
	.health-section {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.5rem 0.875rem;
		border-top: 1px solid var(--border-dim);
		flex-shrink: 0;
	}

	.health-section--collapsed {
		justify-content: center;
		padding: 0.5rem;
		gap: 0;
		flex-direction: column;
		gap: 0.35rem;
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

	.ping-btn--icon {
		display: grid;
		place-items: center;
		width: 24px;
		height: 24px;
		padding: 0;
	}

	/* Footer */
	.sidebar-footer {
		padding: 0.625rem 0.75rem;
		border-top: 1px solid var(--border-dim);
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		flex-shrink: 0;
	}

	.sidebar--collapsed .sidebar-footer {
		padding: 0.625rem 0;
		align-items: center;
	}

	.user-card {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		padding: 0.5rem 0.6rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}

	.user-avatar {
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		font-weight: 600;
		color: var(--text-primary);
		flex-shrink: 0;
	}

	.user-avatar-solo {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border-radius: 50%;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		font-family: var(--font-mono);
		font-size: 0.72rem;
		font-weight: 600;
		color: var(--text-primary);
		cursor: default;
	}

	.user-info {
		display: flex;
		flex-direction: column;
		gap: 0.1rem;
		min-width: 0;
	}

	.user-name {
		font-size: 0.78rem;
		font-weight: 500;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.user-role {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-muted);
	}

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

	.signout-btn--icon {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.signout-btn--icon:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}
</style>
