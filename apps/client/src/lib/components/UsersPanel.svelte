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

	// Create form
	let showCreateForm = $state(false);
	let createUsername = $state("");
	let createPassword = $state("");
	let createRole = $state("member");
	let createLoading = $state(false);

	// Password reset inline
	let resetPasswords = $state<Record<string, string>>({});
	let resetLoadingUserId = $state("");
	let showResetFor = $state<string | null>(null);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);
	let deleteTimer: ReturnType<typeof setTimeout> | null = null;

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
			showCreateForm = false;
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
			showResetFor = null;
			notice = "Password updated.";
		} catch (error) {
			errorMessage = formatApiFailure(error);
		} finally {
			resetLoadingUserId = "";
		}
	}

	function requestDelete(userId: string): void {
		if (deleteConfirmId === userId) {
			// Second click — execute
			if (deleteTimer) { clearTimeout(deleteTimer); deleteTimer = null; }
			void removeUser(userId);
		} else {
			deleteConfirmId = userId;
			if (deleteTimer) clearTimeout(deleteTimer);
			deleteTimer = setTimeout(() => { deleteConfirmId = null; deleteTimer = null; }, 3000);
		}
	}

	async function removeUser(userId: string): Promise<void> {
		errorMessage = "";
		notice = "";
		try {
			await runApiEffect(deleteUser(config, userId));
			notice = "User deleted.";
			deleteConfirmId = null;
			await refreshUsers();
		} catch (error) {
			errorMessage = formatApiFailure(error);
		}
	}

	const getInitial = (name: string): string => name ? name[0].toUpperCase() : "?";

	onMount(() => {
		void refreshUsers();
	});

	$effect(() => {
		config.baseUrl;
		void refreshUsers();
	});
</script>

<div class="users-panel anim-fade-up">
	<!-- Header -->
	<div class="users-header">
		<div>
			<p class="section-label">Admin</p>
			<h2 class="users-title">User Accounts</h2>
		</div>
		<div class="users-header-actions">
			<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshUsers()} disabled={loading}>
				{#if loading}
					<svg class="spin" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{/if}
				{loading ? "Refreshing..." : "Refresh"}
			</button>
			<button
				class="btn-primary btn-sm"
				type="button"
				onclick={() => { showCreateForm = !showCreateForm; errorMessage = ""; notice = ""; }}
			>
				{showCreateForm ? "Cancel" : "+ New user"}
			</button>
		</div>
	</div>

	<!-- Alerts -->
	{#if errorMessage}<p class="alert-error anim-fade-up">{errorMessage}</p>{/if}
	{#if notice}<p class="alert-ok anim-fade-up">{notice}</p>{/if}

	<!-- Create user form (collapsible) -->
	{#if showCreateForm}
		<div class="create-card panel anim-fade-up">
			<div class="panel-header">
				<span class="panel-title">New user</span>
			</div>
			<div class="panel-body">
				<form class="create-form" onsubmit={(e) => { e.preventDefault(); void submitCreate(); }}>
					<label class="field-col">
						<span class="section-label">Username</span>
						<input class="field" type="text" bind:value={createUsername} autocomplete="off" autocapitalize="none" spellcheck="false" required placeholder="username" />
					</label>
					<label class="field-col">
						<span class="section-label">Temporary password</span>
						<input class="field" type="password" bind:value={createPassword} autocomplete="new-password" required placeholder="password" />
					</label>
					<label class="field-col">
						<span class="section-label">Role</span>
						<div class="role-toggle">
							{#each ["member", "admin"] as role}
								<button
									type="button"
									class="role-btn"
									class:role-btn--active={createRole === role}
									onclick={() => createRole = role}
								>{role}</button>
							{/each}
						</div>
					</label>
					<div class="create-form-footer">
						<button class="btn-primary btn-sm" type="submit" disabled={createLoading}>
							{createLoading ? "Creating..." : "Create user"}
						</button>
					</div>
				</form>
			</div>
		</div>
	{/if}

	<!-- User list -->
	{#if users.length === 0 && !loading}
		<div class="empty-state">
			<p class="empty-text">No users found.</p>
		</div>
	{:else}
		<div class="user-list">
			{#each users as user, i (user.id)}
				<div class="user-row anim-fade-up" style="animation-delay: {i * 0.04}s">
					<!-- Identity -->
					<div class="user-identity">
						<div class="user-avatar user-avatar--{user.role}">{getInitial(user.username)}</div>
						<div class="user-info">
							<div class="user-name-row">
								<span class="user-name">{user.username}</span>
								{#if user.id === currentUserId}
									<span class="you-badge">You</span>
								{/if}
							</div>
							<span class="user-role-badge user-role-badge--{user.role}">{user.role}</span>
						</div>
					</div>

					<!-- Actions -->
					<div class="user-actions">
						<!-- Password reset -->
						{#if showResetFor === user.id}
							<div class="inline-reset anim-fade-up">
								<input
									class="field inline-reset-input"
									type="password"
									value={resetPasswords[user.id] ?? ""}
									oninput={(e) => { resetPasswords = { ...resetPasswords, [user.id]: (e.currentTarget as HTMLInputElement).value }; }}
									placeholder="new password"
								/>
								<button
									class="btn-ghost btn-sm"
									type="button"
									onclick={() => void submitPasswordReset(user.id)}
									disabled={resetLoadingUserId === user.id || !(resetPasswords[user.id]?.length > 0)}
								>
									{resetLoadingUserId === user.id ? "Saving..." : "Save"}
								</button>
								<button class="btn-ghost btn-sm" type="button" onclick={() => { showResetFor = null; }}>
									Cancel
								</button>
							</div>
						{:else}
							<button
								class="btn-ghost btn-sm"
								type="button"
								onclick={() => { showResetFor = user.id; resetPasswords = { ...resetPasswords, [user.id]: "" }; }}
							>
								Reset password
							</button>
						{/if}

						<!-- Delete -->
						{#if user.id !== currentUserId}
							<button
								class="delete-btn"
								class:delete-btn--confirm={deleteConfirmId === user.id}
								type="button"
								onclick={() => requestDelete(user.id)}
							>
								{deleteConfirmId === user.id ? "Confirm delete" : "Delete"}
							</button>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.users-panel {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}

	/* Header */
	.users-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 1rem;
		padding-bottom: 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.users-title {
		margin: 0.2rem 0 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.users-header-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-shrink: 0;
	}

	/* Create form */
	.create-card {
		overflow: visible;
	}

	.create-form {
		display: grid;
		grid-template-columns: 1fr 1fr 1fr auto;
		gap: 0.75rem;
		align-items: end;
	}

	.field-col {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.role-toggle {
		display: flex;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		overflow: hidden;
		height: 2.1rem;
	}

	.role-btn {
		flex: 1;
		background: transparent;
		border: none;
		border-right: 1px solid var(--border-mid);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		cursor: pointer;
		transition: background 0.12s, color 0.12s;
	}

	.role-btn:last-child { border-right: none; }

	.role-btn--active {
		background: var(--bg-overlay);
		color: var(--text-primary);
	}

	.role-btn:hover:not(.role-btn--active) {
		background: var(--accent-dim);
		color: var(--text-secondary);
	}

	.create-form-footer {
		display: flex;
		justify-content: flex-end;
	}

	/* Empty state */
	.empty-state {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 3rem 2rem;
	}

	.empty-text {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		color: var(--text-muted);
	}

	/* User list */
	.user-list {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.user-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.75rem 1rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		transition: border-color 0.12s, background 0.12s;
	}

	.user-row:hover {
		border-color: var(--border-mid);
		background: var(--bg-raised);
	}

	/* Identity */
	.user-identity {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		min-width: 0;
	}

	.user-avatar {
		display: grid;
		place-items: center;
		width: 34px;
		height: 34px;
		border-radius: 50%;
		font-family: var(--font-mono);
		font-size: 0.78rem;
		font-weight: 600;
		flex-shrink: 0;
		border: 1px solid;
	}

	.user-avatar--admin {
		background: rgba(251, 191, 36, 0.08);
		border-color: rgba(251, 191, 36, 0.22);
		color: #fbbf24;
	}

	.user-avatar--member {
		background: var(--bg-overlay);
		border-color: var(--border-mid);
		color: var(--text-secondary);
	}

	.user-info {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		min-width: 0;
	}

	.user-name-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.user-name {
		font-size: 0.88rem;
		font-weight: 500;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.you-badge {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.1rem 0.35rem;
		border-radius: 999px;
		background: var(--accent-dim);
		border: 1px solid var(--border-mid);
		color: var(--text-secondary);
		flex-shrink: 0;
	}

	.user-role-badge {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		padding: 0.1rem 0.4rem;
		border-radius: 999px;
		border: 1px solid;
		width: fit-content;
	}

	.user-role-badge--admin {
		color: #fbbf24;
		border-color: rgba(251, 191, 36, 0.25);
		background: rgba(251, 191, 36, 0.07);
	}

	.user-role-badge--member {
		color: var(--text-muted);
		border-color: var(--border-dim);
		background: transparent;
	}

	/* Actions */
	.user-actions {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-shrink: 0;
	}

	.inline-reset {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}

	.inline-reset-input {
		width: 11rem;
		font-size: 0.7rem;
	}

	/* Delete button */
	.delete-btn {
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		padding: 0.3rem 0.65rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.delete-btn:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}

	.delete-btn--confirm {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
		animation: pulse-confirm 0.3s var(--ease-spring);
	}

	@keyframes pulse-confirm {
		0%   { transform: scale(1); }
		50%  { transform: scale(1.05); }
		100% { transform: scale(1); }
	}

	.spin { animation: rotate 0.8s linear infinite; }
	@keyframes rotate { to { transform: rotate(360deg); } }

	@media (max-width: 860px) {
		.create-form {
			grid-template-columns: 1fr 1fr;
		}

		.create-form-footer {
			grid-column: span 2;
		}
	}

	@media (max-width: 640px) {
		.create-form {
			grid-template-columns: 1fr;
		}

		.create-form-footer {
			grid-column: span 1;
		}

		.user-row {
			flex-direction: column;
			align-items: stretch;
		}

		.user-actions {
			flex-wrap: wrap;
		}

		.inline-reset {
			flex-wrap: wrap;
		}

		.inline-reset-input {
			width: 100%;
		}
	}
</style>
