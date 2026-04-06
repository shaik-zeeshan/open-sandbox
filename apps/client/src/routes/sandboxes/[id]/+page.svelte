<script lang="ts">
	import { goto } from "$app/navigation";
	import { page } from "$app/stores";
	import { authController, checkHealth, signOut } from "$lib/auth-controller.svelte";
	import PageShell from "$lib/components/PageShell.svelte";
	import SandboxWorkspace from "$lib/components/SandboxWorkspace.svelte";
	import { clientState } from "$lib/stores.svelte";
	import { formatApiFailure, listContainers, listSandboxes, runApiEffect, type ContainerSummary, type Sandbox } from "$lib/api";
	import { toast } from "$lib/toast.svelte";

	type RefreshOptions = {
		showLoading?: boolean;
		notifyOnError?: boolean;
	};

	const sandboxId = $derived($page.params.id);
	let sandbox = $state<Sandbox | null>(null);
	let container = $state<ContainerSummary | null>(null);
	let loading = $state(false);
	let errorMessage = $state("");
	let missingSandbox = $state(false);

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
			const foundSandbox = sandboxes.find((item) => item.id === sandboxId) ?? null;
			sandbox = foundSandbox;
			container = foundSandbox ? (containers.find((item) => item.id === foundSandbox.id) ?? null) : null;
			missingSandbox = foundSandbox === null;
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
		sandboxId;
		sandbox = null;
		container = null;
		missingSandbox = false;
		errorMessage = "";
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
		{#if loading && sandbox === null && !missingSandbox && errorMessage.length === 0}
			<div class="panel-card">
				<p class="panel-title">Loading sandbox...</p>
			</div>
		{:else if missingSandbox}
			<div class="panel-card">
				<p class="panel-title">Sandbox not found</p>
				<p class="panel-subtitle">This sandbox may have been deleted or is no longer available.</p>
				<button class="btn-ghost btn-sm" type="button" onclick={() => void goto("/")}>Back to workloads</button>
			</div>
		{:else if errorMessage.length > 0 && sandbox === null}
			<div class="panel-card">
				<p class="panel-title">Unable to load sandbox</p>
				<p class="panel-subtitle">{errorMessage}</p>
				<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshData()}>Try again</button>
			</div>
		{:else if sandbox}
			<SandboxWorkspace
				{sandbox}
				{container}
				runtimeContainer={null}
				config={clientState.config}
				onBack={() => void goto("/")}
				onRefresh={() => refreshData({ showLoading: false, notifyOnError: true })}
				onContainerReplaced={() => {}}
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
