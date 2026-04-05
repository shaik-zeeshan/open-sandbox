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
	<main class="shell-main">
		{@render children()}
	</main>
</div>

<style>
	.shell {
		display: flex;
		min-height: 100vh;
		align-items: stretch;
	}

	.shell-main {
		flex: 1;
		min-width: 0;
		overflow-y: auto;
		height: 100vh;
	}
</style>
