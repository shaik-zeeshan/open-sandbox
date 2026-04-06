<script lang="ts">
	import { goto } from "$app/navigation";
	import { page } from "$app/stores";
	import { authController, checkHealth, signOut } from "$lib/auth-controller.svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import SandboxWorkspace from "$lib/components/SandboxWorkspace.svelte";
	import { clientState } from "$lib/stores.svelte";
	import {
		formatApiFailure,
		listContainers,
		listSandboxes,
		runApiEffect,
		type ContainerSummary,
		type Sandbox
	} from "$lib/api";
	import { toast } from "$lib/toast.svelte";

	type RefreshOptions = {
		showLoading?: boolean;
		notifyOnError?: boolean;
	};

	const containerId = $derived($page.params.id);
	let sandbox = $state<Sandbox | null>(null);
	let runtimeContainer = $state<ContainerSummary | null>(null);
	let loading = $state(false);
	let errorMessage = $state("");
	let missingContainer = $state(false);
	let expectedRuntimeLabel = $state<"compose service" | "container">("container");

	const runtimeKind = $derived.by(() => {
		if (runtimeContainer === null) {
			return expectedRuntimeLabel;
		}
		const composeProject = (runtimeContainer.project_name ?? runtimeContainer.labels?.["com.docker.compose.project"] ?? "").trim();
		const workloadKind = (runtimeContainer.workload_kind ?? "").trim().toLowerCase();
		if (composeProject.length > 0 || workloadKind === "compose") {
			return "compose service" as const;
		}
		return "container" as const;
	});

	const loadingTitle = $derived(runtimeKind === "compose service" ? "Loading compose service..." : "Loading container...");
	const missingTitle = $derived(runtimeKind === "compose service" ? "Compose service not found" : "Container not found");
	const missingSubtitle = $derived(
		runtimeKind === "compose service"
			? "This compose service may have been removed."
			: "This container may have been removed."
	);
	const errorTitle = $derived(runtimeKind === "compose service" ? "Unable to load compose service" : "Unable to load container");

	async function refreshData(options?: RefreshOptions): Promise<void> {
		const showLoading = options?.showLoading ?? true;
		const notifyOnError = options?.notifyOnError ?? true;
		if (!clientState.isAuthenticated) {
			return;
		}
		if (showLoading) {
			loading = true;
		}
		errorMessage = "";
		try {
			const [sandboxes, containers] = await Promise.all([
				runApiEffect(listSandboxes(clientState.config)),
				runApiEffect(listContainers(clientState.config))
			]);
			const foundContainer = containers.find((item) => item.id === containerId) ?? null;
			runtimeContainer = foundContainer;
			if (foundContainer !== null) {
				const composeProject = (foundContainer.project_name ?? foundContainer.labels?.["com.docker.compose.project"] ?? "").trim();
				const workloadKind = (foundContainer.workload_kind ?? "").trim().toLowerCase();
				expectedRuntimeLabel = composeProject.length > 0 || workloadKind === "compose" ? "compose service" : "container";
			}
			sandbox = foundContainer ? (sandboxes.find((item) => item.id === foundContainer.id) ?? null) : null;
			missingContainer = foundContainer === null;
		} catch (error) {
			errorMessage = formatApiFailure(error);
			if (notifyOnError) {
				toast.error(errorMessage);
			}
		} finally {
			if (showLoading) {
				loading = false;
			}
		}
	}

	$effect(() => {
		containerId;
		sandbox = null;
		runtimeContainer = null;
		missingContainer = false;
		errorMessage = "";
		expectedRuntimeLabel = "container";
		if (clientState.isAuthenticated) {
			void refreshData();
		}
	});

</script>

{#if !clientState.authResolved}
	<div class="auth-screen anim-fade-up">
		<div class="auth-ambient"></div>
		<div class="auth-card">
			<p class="auth-checking">Checking session...</p>
		</div>
	</div>
{:else if clientState.isAuthenticated}
	<PageShell
		health={authController.health}
		healthMessage={authController.healthMessage}
		onPing={() => void checkHealth()}
		onSignOut={() => void signOut()}
		currentUsername={clientState.username}
		currentRole={clientState.role}
	>
		{#if loading && runtimeContainer === null && !missingContainer && errorMessage.length === 0}
			<div class="panel-card">
				<p class="panel-title">{loadingTitle}</p>
			</div>
		{:else if missingContainer}
			<div class="panel-card">
				<p class="panel-title">{missingTitle}</p>
				<p class="panel-subtitle">{missingSubtitle}</p>
				<button class="btn-ghost btn-sm" type="button" onclick={() => void goto("/")}>Back to workloads</button>
			</div>
		{:else if errorMessage.length > 0 && runtimeContainer === null}
			<div class="panel-card">
				<p class="panel-title">{errorTitle}</p>
				<p class="panel-subtitle">{errorMessage}</p>
				<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshData()}>Try again</button>
			</div>
		{:else if runtimeContainer}
			<SandboxWorkspace
				{sandbox}
				container={runtimeContainer}
				runtimeContainer={runtimeContainer}
				showTerminal={false}
				config={clientState.config}
				onBack={() => void goto("/")}
				onRefresh={() => refreshData({ showLoading: false, notifyOnError: true })}
				onContainerReplaced={(id) => { void goto(`/services/${encodeURIComponent(id)}`); }}
				onDeleted={() => { void goto("/"); }}
			/>
		{/if}
	</PageShell>
{/if}

<style>
	.auth-screen {
		min-height: 100vh;
		display: grid;
		place-items: center;
	}

	.auth-ambient {
		position: fixed;
		inset: 0;
		pointer-events: none;
		background: radial-gradient(ellipse 60% 50% at 50% 40%, rgba(255, 255, 255, 0.025) 0%, transparent 70%);
	}

	.auth-card,
	.panel-card {
		position: relative;
		z-index: 1;
		padding: 1.5rem;
		margin: 1rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-xl);
		display: flex;
		flex-direction: column;
		gap: 0.6rem;
	}

	.auth-checking,
	.panel-title,
	.panel-subtitle {
		margin: 0;
	}

	.auth-checking {
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--text-muted);
	}

	.panel-title {
		font-family: var(--font-mono);
		font-size: 0.75rem;
		color: var(--text-primary);
	}

	.panel-subtitle {
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-muted);
	}
</style>
