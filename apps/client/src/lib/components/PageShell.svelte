<script lang="ts">
	import Sidebar from "$lib/components/Sidebar.svelte";
	import { readStorageItem } from "$lib/client/browser";

	type HealthState = "unknown" | "checking" | "ok" | "error";

	let {
		health,
		healthMessage,
		onPing,
		onSignOut,
		currentUsername,
		currentRole,
		children
	} = $props<{
		health: HealthState;
		healthMessage: string;
		onPing: () => void;
		onSignOut: () => void;
		currentUsername: string;
		currentRole: string;
		children: import("svelte").Snippet;
	}>();

	let collapsed = $state(
		readStorageItem("sidebar-collapsed") === "true"
	);
</script>

<div class="shell">
	<Sidebar
		{health}
		{healthMessage}
		{onPing}
		{onSignOut}
		{currentUsername}
		{currentRole}
		bind:collapsed
	/>
	<main class="shell-main" class:shell-main--collapsed={collapsed}>
		{@render children()}
	</main>
</div>

<style>
	.shell {
		min-height: 100vh;
	}

	.shell-main {
		min-width: 0;
		overflow-y: auto;
		height: 100vh;
		margin-left: calc(var(--sidebar-width) + 20px);
		transition: margin-left 0.22s var(--ease-snappy);
	}

	.shell-main--collapsed {
		margin-left: calc(52px + 20px);
	}
</style>
