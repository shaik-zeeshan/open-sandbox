<script lang="ts">
	import { getCachedUsers, invalidateUsersCache, refreshCachedUsers } from "$lib/api-cache";
	import { clearScheduledTimeout, scheduleTimeout, type TimeoutHandle } from "$lib/client/browser";
	import { toast } from "$lib/toast.svelte";
	import {
		createUser as apiCreateUser,
		deleteUser as apiDeleteUser,
		formatApiFailure,
		runApiEffect,
		type ApiConfig,
		type UserSummary,
		updateUserPassword as apiUpdateUserPassword
	} from "$lib/api";
	import { Context, Effect, Layer } from "effect";

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

	// Create form
	let showCreateForm = $state(false);
	let createUsername = $state("");
	let createPassword = $state("");
	let createRole = $state("member");
	let createLoading = $state(false);

	// Create form validation
	let createUsernameError = $state("");
	let createPasswordError = $state("");
	let createFormTouched = $state(false);

	// Password reset inline
	let resetPasswords = $state<Record<string, string>>({});
	let resetLoadingUserId = $state("");
	let showResetFor = $state<string | null>(null);

	// Password reset validation
	let resetPasswordError = $state<Record<string, string>>({});

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

	interface UsersApiService {
		list: (options?: { force?: boolean }) => Effect.Effect<UserSummary[], unknown>;
		create: (input: { username: string; password: string; role: string }) => Effect.Effect<void, unknown>;
		resetPassword: (userId: string, password: string) => Effect.Effect<void, unknown>;
		remove: (userId: string) => Effect.Effect<void, unknown>;
	}

	interface UsersFeedbackService {
		error: (error: unknown) => Effect.Effect<void>;
		ok: (message: string) => Effect.Effect<void>;
	}

	interface DeleteConfirmationService {
		request: (userId: string) => Effect.Effect<boolean>;
		clear: Effect.Effect<void>;
	}

	const UsersApiService = Context.GenericTag<UsersApiService>("users-panel/UsersApiService");
	const UsersFeedbackService = Context.GenericTag<UsersFeedbackService>("users-panel/UsersFeedbackService");
	const DeleteConfirmationService = Context.GenericTag<DeleteConfirmationService>("users-panel/DeleteConfirmationService");

	const usersApiService: UsersApiService = {
		list: (options) => (options?.force ? refreshCachedUsers(config) : getCachedUsers(config)),
		create: (input) =>
			Effect.promise(() => runApiEffect(apiCreateUser(config, input))).pipe(Effect.asVoid),
		resetPassword: (userId, password) =>
			Effect.promise(() => runApiEffect(apiUpdateUserPassword(config, userId, password))).pipe(Effect.asVoid),
		remove: (userId) =>
			Effect.promise(() => runApiEffect(apiDeleteUser(config, userId))).pipe(Effect.asVoid)
	};

	const usersFeedbackService: UsersFeedbackService = {
		error: (error) => Effect.sync(() => {
			toast.error(formatApiFailure(error));
		}),
		ok: (message) => Effect.sync(() => {
			toast.ok(message);
		})
	};

	const deleteConfirmationService: DeleteConfirmationService = (() => {
		let pendingUserId: string | null = null;
		let timerHandle: TimeoutHandle | null = null;

		const clearPending = (): void => {
			clearScheduledTimeout(timerHandle);
			timerHandle = null;
			pendingUserId = null;
			deleteConfirmId = null;
		};

		return {
			request: (userId) =>
				Effect.sync(() => {
					if (pendingUserId === userId) {
						clearPending();
						return true;
					}

					clearScheduledTimeout(timerHandle);
					pendingUserId = userId;
					deleteConfirmId = userId;
					timerHandle = scheduleTimeout(() => {
						pendingUserId = null;
						deleteConfirmId = null;
						timerHandle = null;
					}, 3000);
					return false;
				}),
			clear: Effect.sync(() => {
				clearPending();
			})
		};
	})();

	const usersPanelLayer = Layer.mergeAll(
		Layer.succeed(UsersApiService, usersApiService),
		Layer.succeed(UsersFeedbackService, usersFeedbackService),
		Layer.succeed(DeleteConfirmationService, deleteConfirmationService)
	);

	const runUsersProgram = <A>(program: Effect.Effect<A, unknown>): Promise<A> => Effect.runPromise(program);

	const refreshUsersProgram = (options?: { force?: boolean }): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* UsersApiService;
			const feedback = yield* UsersFeedbackService;

			yield* Effect.sync(() => {
				loading = true;
			});

			try {
				const listedUsers = yield* api.list(options);
				yield* Effect.sync(() => {
					users = listedUsers;
				});
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					loading = false;
				});
			}
		}).pipe(Effect.provide(usersPanelLayer));

	const createUserProgram = (input: { username: string; password: string; role: string }): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* UsersApiService;
			const feedback = yield* UsersFeedbackService;

			yield* Effect.sync(() => {
				createLoading = true;
			});

			try {
				yield* api.create(input);
				yield* invalidateUsersCache(config);
				yield* Effect.sync(() => {
					createUsername = "";
					createPassword = "";
					createRole = "member";
					showCreateForm = false;
				});
				yield* feedback.ok("User created.");
				yield* refreshUsersProgram();
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					createLoading = false;
				});
			}
		}).pipe(Effect.provide(usersPanelLayer));

	const resetPasswordProgram = (userId: string, password: string): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* UsersApiService;
			const feedback = yield* UsersFeedbackService;

			yield* Effect.sync(() => {
				resetLoadingUserId = userId;
			});

			try {
				yield* api.resetPassword(userId, password);
				yield* Effect.sync(() => {
					resetPasswords = { ...resetPasswords, [userId]: "" };
					showResetFor = null;
				});
				yield* feedback.ok("Password updated.");
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					resetLoadingUserId = "";
				});
			}
		}).pipe(Effect.provide(usersPanelLayer));

	const removeUserProgram = (userId: string): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const api = yield* UsersApiService;
			const feedback = yield* UsersFeedbackService;
			const deleteConfirmation = yield* DeleteConfirmationService;

			try {
				yield* api.remove(userId);
				yield* invalidateUsersCache(config);
				yield* deleteConfirmation.clear;
				yield* feedback.ok("User deleted.");
				yield* refreshUsersProgram();
			} catch (error) {
				yield* feedback.error(error);
			}
		}).pipe(Effect.provide(usersPanelLayer));

	const requestDeleteProgram = (userId: string): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const deleteConfirmation = yield* DeleteConfirmationService;
			const confirmed = yield* deleteConfirmation.request(userId);
			if (confirmed) {
				yield* removeUserProgram(userId);
			}
		}).pipe(Effect.provide(usersPanelLayer));

	const refreshUsers = (options?: { force?: boolean }): Promise<void> => runUsersProgram(refreshUsersProgram(options));

	const submitCreate = (): Promise<void> =>
		runUsersProgram(createUserProgram({
			username: createUsername,
			password: createPassword,
			role: createRole
		}));

	const submitPasswordReset = (userId: string): Promise<void> =>
		runUsersProgram(resetPasswordProgram(userId, resetPasswords[userId] ?? ""));

	const requestDelete = (userId: string): Promise<void> => runUsersProgram(requestDeleteProgram(userId));

	const getInitial = (name: string): string => name ? name[0].toUpperCase() : "?";

	// Reset create form validation when the form is hidden
	$effect(() => {
		if (!showCreateForm) {
			createUsernameError = "";
			createPasswordError = "";
			createFormTouched = false;
		}
	});

	// Clear create form errors as user types (only after first submit attempt)
	$effect(() => {
		if (createFormTouched) {
			if (createUsername.trim().length >= 2) createUsernameError = "";
			if (createPassword.length >= 4) createPasswordError = "";
		}
	});

	$effect(() => {
		return () => {
			void runUsersProgram(deleteConfirmationService.clear);
		};
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
			<button class="btn-ghost btn-sm" type="button" onclick={() => void refreshUsers({ force: true })} disabled={loading}>
				{#if loading}
					<svg class="spin" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.22-8.56"/></svg>
				{/if}
				{loading ? "Refreshing..." : "Refresh"}
			</button>
			<button
				class="btn-primary btn-sm"
				type="button"
				onclick={() => { showCreateForm = !showCreateForm; }}
			>
				{showCreateForm ? "Cancel" : "+ New user"}
			</button>
		</div>
	</div>

	<!-- Create user form (collapsible) -->
	{#if showCreateForm}
		<div class="create-card panel anim-fade-up">
			<div class="panel-header">
				<span class="panel-title">New user</span>
			</div>
			<div class="panel-body">
			<form class="create-form" onsubmit={(e) => {
					e.preventDefault();
					createFormTouched = true;
					createUsernameError = "";
					createPasswordError = "";
					let valid = true;
					if (createUsername.trim().length === 0) { createUsernameError = "Username is required."; valid = false; }
					else if (createUsername.trim().length < 2) { createUsernameError = "Username must be at least 2 characters."; valid = false; }
					if (createPassword.length === 0) { createPasswordError = "Password is required."; valid = false; }
					else if (createPassword.length < 4) { createPasswordError = "Password must be at least 4 characters."; valid = false; }
					if (valid) void submitCreate();
				}}>
				<label class="field-col">
					<span class="section-label">Username</span>
					<input class="field" class:field--error={!!createUsernameError} type="text" bind:value={createUsername} autocomplete="off" autocapitalize="none" spellcheck="false" required placeholder="username" />
					{#if createUsernameError}<span class="field-inline-error">{createUsernameError}</span>{/if}
				</label>
				<label class="field-col">
					<span class="section-label">Temporary password</span>
					<input class="field" class:field--error={!!createPasswordError} type="password" bind:value={createPassword} autocomplete="new-password" required placeholder="password" />
					{#if createPasswordError}<span class="field-inline-error">{createPasswordError}</span>{/if}
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
					<button class="btn-primary btn-sm" type="submit" disabled={createLoading || (createFormTouched && (!!createUsernameError || !!createPasswordError))}>
						{createLoading ? "Creating..." : "Create user"}
					</button>
				</div>
				</form>
			</div>
		</div>
	{/if}

	<!-- User list -->
	{#if loading && users.length === 0}
		<div class="user-list">
			{#each { length: 4 } as _, i}
				<div class="user-row skeleton-user-row" style="animation-delay: {i * 0.04}s">
					<div class="user-identity">
						<div class="skel-avatar"></div>
						<div class="user-info">
							<div class="skel-line skel-line--uname"></div>
							<div class="skel-line skel-line--role"></div>
						</div>
					</div>
					<div class="user-actions">
						<div class="skel-btn"></div>
						<div class="skel-btn skel-btn--narrow"></div>
					</div>
				</div>
			{/each}
		</div>
	{:else if users.length === 0 && !loading}
		<div class="empty-state">
			<div class="empty-icon">
				<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
					<circle cx="12" cy="8" r="4"/><path d="M4 20c0-4 3.6-7 8-7s8 3 8 7"/>
				</svg>
			</div>
			<p class="empty-title">No users found</p>
			<p class="empty-sub">User accounts will appear here once created.</p>
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
							<div class="inline-reset-field">
								<input
									class="field inline-reset-input"
									class:field--error={!!resetPasswordError[user.id]}
									type="password"
									value={resetPasswords[user.id] ?? ""}
									oninput={(e) => {
										const val = (e.currentTarget as HTMLInputElement).value;
										resetPasswords = { ...resetPasswords, [user.id]: val };
										if (val.length >= 4) resetPasswordError = { ...resetPasswordError, [user.id]: "" };
									}}
									placeholder="new password"
								/>
								{#if resetPasswordError[user.id]}<span class="field-inline-error">{resetPasswordError[user.id]}</span>{/if}
							</div>
							<button
								class="btn-ghost btn-sm"
								type="button"
								onclick={() => {
									const pw = resetPasswords[user.id] ?? "";
									if (pw.length === 0) {
										resetPasswordError = { ...resetPasswordError, [user.id]: "Password is required." };
										return;
									}
									if (pw.length < 4) {
										resetPasswordError = { ...resetPasswordError, [user.id]: "Password must be at least 4 characters." };
										return;
									}
									resetPasswordError = { ...resetPasswordError, [user.id]: "" };
									void submitPasswordReset(user.id);
								}}
								disabled={resetLoadingUserId === user.id}
							>
								{resetLoadingUserId === user.id ? "Saving..." : "Save"}
							</button>
							<button class="btn-ghost btn-sm" type="button" onclick={() => { showResetFor = null; resetPasswordError = { ...resetPasswordError, [user.id]: "" }; }}>
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

	.field--error {
		border-color: var(--status-error-border) !important;
	}

	.field-inline-error {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--status-error);
		margin-top: 0.1rem;
	}

	/* Empty state */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.625rem;
		padding: 4rem 2rem;
		text-align: center;
	}

	.empty-icon {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border-radius: var(--radius-md);
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		color: var(--text-muted);
		margin-bottom: 0.5rem;
	}

	.empty-title {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.82rem;
		color: var(--text-secondary);
	}

	.empty-sub {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
	}

	/* ── Skeleton loading rows ────────────────────────────────────────────────── */
	@keyframes shimmer {
		0%   { background-position: 200% 0; }
		100% { background-position: -200% 0; }
	}

	.skeleton-user-row {
		pointer-events: none;
	}

	.skel-avatar {
		width: 34px;
		height: 34px;
		border-radius: 50%;
		flex-shrink: 0;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-line {
		height: 9px;
		border-radius: 4px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-line--uname { width: 8rem; }
	.skel-line--role  { width: 3.5rem; height: 7px; margin-top: 4px; opacity: 0.6; }

	.skel-btn {
		height: 28px;
		width: 90px;
		border-radius: 4px;
		background: linear-gradient(90deg, var(--bg-raised) 25%, var(--bg-overlay) 50%, var(--bg-raised) 75%);
		background-size: 200% 100%;
		animation: shimmer 1.5s ease-in-out infinite;
	}

	.skel-btn--narrow { width: 56px; }

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
		align-items: flex-start;
		gap: 0.4rem;
	}

	.inline-reset-field {
		display: flex;
		flex-direction: column;
		gap: 0.1rem;
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
