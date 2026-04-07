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

	const missingTitle = $derived(runtimeKind === "compose service" ? "Compose service not found" : `${runtimeKind[0].toUpperCase()}${runtimeKind.slice(1)} not found`);
	const missingSubtitle = $derived(
		runtimeKind === "compose service"
			? "This compose service may have been removed."
			: `This ${runtimeKind} may have been removed.`
	);
	const errorTitle = $derived(
		runtimeKind === "compose service"
			? "Unable to load compose service"
			: `Unable to load ${runtimeKind}`
	);

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
			<div class="skeleton-page anim-fade-up">
				<div class="skel-header">
					<div class="skel-block" style="width:1.5rem;height:1.5rem;border-radius:var(--radius-sm);flex-shrink:0"></div>
					<div class="skel-block" style="width:11rem;height:1rem"></div>
					<div class="skel-block" style="width:4.5rem;height:1rem;margin-left:auto"></div>
				</div>
				<div class="skel-tabs">
					{#each [5.5, 4.5, 3.5, 3] as w}
						<div class="skel-block" style="width:{w}rem;height:0.7rem"></div>
					{/each}
				</div>
				<div class="skel-panel">
					<div class="skel-row">
						<div class="skel-block" style="width:5rem;height:0.65rem"></div>
						<div class="skel-block" style="width:9rem;height:0.65rem;margin-left:auto"></div>
					</div>
					<div class="skel-row">
						<div class="skel-block" style="width:4rem;height:0.65rem"></div>
						<div class="skel-block" style="width:7rem;height:0.65rem;margin-left:auto"></div>
					</div>
					<div class="skel-row">
						<div class="skel-block" style="width:6rem;height:0.65rem"></div>
						<div class="skel-block" style="width:5rem;height:0.65rem;margin-left:auto"></div>
					</div>
				</div>
				<div class="skel-panel">
					<div class="skel-row">
						<div class="skel-block" style="width:8rem;height:0.65rem"></div>
					</div>
					<div class="skel-row">
						<div class="skel-block" style="width:100%;height:2rem;border-radius:var(--radius-sm)"></div>
					</div>
				</div>
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

	@keyframes shimmer {
		from { background-position: -200% center; }
		to   { background-position:  200% center; }
	}

	.skeleton-page {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		padding: 1rem;
	}

	.skel-header {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.75rem 1rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-xl);
	}

	.skel-tabs {
		display: flex;
		align-items: center;
		gap: 1.25rem;
		padding: 0.6rem 1rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
	}

	.skel-panel {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		padding: 0.875rem 1rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
	}

	.skel-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.skel-block {
		border-radius: var(--radius-sm);
		background: linear-gradient(
			90deg,
			var(--bg-raised) 0%,
			var(--bg-overlay) 50%,
			var(--bg-raised) 100%
		);
		background-size: 200% 100%;
		animation: shimmer 1.6s ease-in-out infinite;
		flex-shrink: 0;
	}
</style>
