<script lang="ts">
	import { onMount } from "svelte";
	import {
		createUser,
		deleteUser,
		formatApiFailure,
		listUsers,
		runApiEffect,
		type ApiConfig,
		type UserSummary,
		updateUserPassword
	} from "$lib/api";

	let {
		config,
		currentUserId,
		currentUsername
	} = $props<{
		config: ApiConfig;
		currentUserId: string;
		currentUsername: string;
	}>();

	let users = $state<UserSummary[]>([]);
	let loading = $state(false);
	let errorMessage = $state("");
	let notice = $state("");

	let createUsername = $state("");
	let createPassword = $state("");
	let createRole = $state("member");
	let createLoading = $state(false);

	let resetPasswords = $state<Record<string, string>>({});
	let resetLoadingUserId = $state("");

	async function refreshUsers(): Promise<void> {
		loading = true;
		errorMessage = "";
		try {
			users = await runApiEffect(listUsers(config));
		} catch (error) {
			errorMessage = formatApiFailure(error);
		} finally {
			loading = false;
		}
	}

	async function submitCreate(): Promise<void> {
		createLoading = true;
		errorMessage = "";
		notice = "";
		try {
			await runApiEffect(createUser(config, {
				username: createUsername,
				password: createPassword,
				role: createRole
			}));
			createUsername = "";
			createPassword = "";
			createRole = "member";
			notice = "User created.";
			await refreshUsers();
		} catch (error) {
			errorMessage = formatApiFailure(error);
		} finally {
			createLoading = false;
		}
	}

	async function submitPasswordReset(userId: string): Promise<void> {
		resetLoadingUserId = userId;
		errorMessage = "";
		notice = "";
		try {
			await runApiEffect(updateUserPassword(config, userId, resetPasswords[userId] ?? ""));
			resetPasswords = { ...resetPasswords, [userId]: "" };
			notice = "Password updated.";
		} catch (error) {
			errorMessage = formatApiFailure(error);
		} finally {
			resetLoadingUserId = "";
		}
	}

	async function removeUser(userId: string): Promise<void> {
		errorMessage = "";
		notice = "";
		try {
			await runApiEffect(deleteUser(config, userId));
			notice = "User deleted.";
			await refreshUsers();
		} catch (error) {
			errorMessage = formatApiFailure(error);
		}
	}

	onMount(() => {
		void refreshUsers();
	});

	$effect(() => {
		config.baseUrl;
		void refreshUsers();
	});
</script>

<section class="users-panel panel anim-fade-up">
	<div class="users-panel__header">
		<div>
			<p class="section-label">User Access</p>
			<h2 class="users-panel__title">Accounts</h2>
		</div>
		<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshUsers()} disabled={loading}>
			{loading ? "Refreshing..." : "Refresh"}
		</button>
	</div>

	<form class="users-panel__create" onsubmit={(event) => { event.preventDefault(); void submitCreate(); }}>
		<input class="field" type="text" bind:value={createUsername} autocomplete="off" autocapitalize="none" spellcheck="false" required placeholder="new username" />
		<input class="field" type="password" bind:value={createPassword} autocomplete="new-password" required placeholder="temporary password" />
		<select class="field users-panel__select" bind:value={createRole}>
			<option value="member">member</option>
			<option value="admin">admin</option>
		</select>
		<button class="btn-primary btn-sm" type="submit" disabled={createLoading}>
			{createLoading ? "Creating..." : "Create user"}
		</button>
	</form>

	{#if errorMessage}
		<p class="alert-error">{errorMessage}</p>
	{/if}
	{#if notice}
		<p class="users-panel__notice">{notice}</p>
	{/if}

	<div class="users-panel__list">
		{#each users as user (user.id)}
			<div class="users-panel__row">
				<div class="users-panel__identity">
					<div class="users-panel__name-row">
						<span class="users-panel__name">{user.username}</span>
						{#if user.id === currentUserId}
							<span class="users-panel__badge">You</span>
						{/if}
					</div>
					<span class="users-panel__meta">{user.role}</span>
				</div>
				<div class="users-panel__actions">
					<input
						class="field users-panel__password"
						type="password"
						value={resetPasswords[user.id] ?? ""}
						oninput={(event) => { resetPasswords = { ...resetPasswords, [user.id]: (event.currentTarget as HTMLInputElement).value }; }}
						placeholder={user.id === currentUserId ? `reset ${currentUsername}` : "new password"}
					/>
					<button class="btn-ghost btn-sm" type="button" onclick={() => void submitPasswordReset(user.id)} disabled={resetLoadingUserId === user.id}>
						{resetLoadingUserId === user.id ? "Saving..." : "Reset password"}
					</button>
					{#if user.id !== currentUserId}
						<button class="btn-ghost btn-sm users-panel__danger" type="button" onclick={() => void removeUser(user.id)}>
							Delete
						</button>
					{/if}
				</div>
			</div>
		{/each}
	</div>
</section>

<style>
	.users-panel {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		padding: 1rem;
		margin-bottom: 1rem;
	}
	.users-panel__header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
	}
	.users-panel__title {
		margin: 0.15rem 0 0;
		font-size: 1.05rem;
		font-weight: 600;
		color: var(--text-primary);
	}
	.users-panel__create {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 8rem auto;
		gap: 0.75rem;
	}
	.users-panel__select {
		text-transform: lowercase;
	}
	.users-panel__notice {
		margin: 0;
		font-size: 0.82rem;
		color: var(--status-ok);
	}
	.users-panel__list {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}
	.users-panel__row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.85rem 0.95rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}
	.users-panel__identity {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
		min-width: 0;
	}
	.users-panel__name-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}
	.users-panel__name {
		font-size: 0.9rem;
		font-weight: 500;
		color: var(--text-primary);
	}
	.users-panel__badge {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.12rem 0.35rem;
		border-radius: 999px;
		background: var(--accent-dim);
		color: var(--text-secondary);
	}
	.users-panel__meta {
		font-family: var(--font-mono);
		font-size: 0.66rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-muted);
	}
	.users-panel__actions {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		flex-wrap: wrap;
		justify-content: flex-end;
	}
	.users-panel__password {
		min-width: 12rem;
	}
	.users-panel__danger {
		border-color: var(--status-error-border);
		color: var(--status-error);
	}
	@media (max-width: 920px) {
		.users-panel__create {
			grid-template-columns: 1fr;
		}
		.users-panel__row {
			flex-direction: column;
			align-items: stretch;
		}
		.users-panel__actions {
			justify-content: stretch;
		}
		.users-panel__password {
			min-width: 0;
			width: 100%;
		}
	}
</style>
