<script lang="ts">
	import { authController, checkHealth, signOut } from "$lib/auth-controller.svelte";
	import { clearScheduledTimeout, scheduleTimeout, type TimeoutHandle } from "$lib/client/browser";
	import PageShell from "$lib/components/PageShell.svelte";
	import {
		createApiKey,
		formatApiFailure,
		listApiKeys,
		revokeApiKey,
		runApiEffect,
		type ApiKeySummary,
		updateUserPassword
	} from "$lib/api";
	import { isValidClientUrl, normalizeClientEndpointUrl } from "$lib/client/url";
	import { clientState, setBaseUrl } from "$lib/stores.svelte";
	import { toast } from "$lib/toast.svelte";
	import { Effect } from "effect";

	// Endpoint settings
	let endpointValue = $state(clientState.baseUrl);
	let endpointDirty = $derived(endpointValue !== clientState.baseUrl);
	let endpointSaved = $state(false);
	let endpointSavedTimer: TimeoutHandle | null = null;

	// Password change
	let currentPassword = $state("");
	let newPassword = $state("");
	let confirmPassword = $state("");
	let passwordLoading = $state(false);
	let passwordError = $state("");
	let passwordFieldError = $state("");
	let passwordSuccess = $state(false);
	let passwordSuccessTimer: TimeoutHandle | null = null;

	// API keys
	let apiKeys = $state<ApiKeySummary[]>([]);
	let apiKeysLoading = $state(false);
	let apiKeysError = $state("");
	let apiKeysRequestVersion = 0;
	let apiKeyName = $state("");
	let apiKeyNameError = $state("");
	let apiKeyCreateLoading = $state(false);
	let revokeConfirmId = $state<string | null>(null);
	let revokeLoadingId = $state<string | null>(null);
	let revokeConfirmTimer: TimeoutHandle | null = null;
	let createdApiKeySecret = $state("");
	let createdApiKeyName = $state("");
	let createdApiKeyCopied = $state(false);
	let createdApiKeyCopiedTimer: TimeoutHandle | null = null;

	const endpointValid = $derived(isValidClientUrl(endpointValue));

	const formatDate = (unixSeconds: number): string =>
		new Date(unixSeconds * 1000).toLocaleString(undefined, {
			year: "numeric",
			month: "short",
			day: "numeric",
			hour: "2-digit",
			minute: "2-digit"
		});

	function clearRevokeConfirm(): void {
		clearScheduledTimeout(revokeConfirmTimer);
		revokeConfirmTimer = null;
		revokeConfirmId = null;
	}

	function scheduleCopiedReset(): void {
		clearScheduledTimeout(createdApiKeyCopiedTimer);
		createdApiKeyCopiedTimer = scheduleTimeout(() => {
			createdApiKeyCopied = false;
			createdApiKeyCopiedTimer = null;
		}, 2000);
	}

	function clearCreatedApiKeyState(): void {
		createdApiKeySecret = "";
		createdApiKeyName = "";
		createdApiKeyCopied = false;
		clearScheduledTimeout(createdApiKeyCopiedTimer);
		createdApiKeyCopiedTimer = null;
	}

	function applyEndpoint(): void {
		const normalizedEndpoint = normalizeClientEndpointUrl(endpointValue);
		if (normalizedEndpoint === null) return;
		setBaseUrl(normalizedEndpoint);
		endpointSaved = true;
		clearScheduledTimeout(endpointSavedTimer);
		endpointSavedTimer = scheduleTimeout(() => {
			endpointSaved = false;
			endpointSavedTimer = null;
		}, 2000);
	}

	const changePasswordProgram = (): Effect.Effect<void, never> =>
		Effect.gen(function* () {
			yield* Effect.sync(() => {
				passwordError = "";
				passwordFieldError = "";
				passwordSuccess = false;
				clearScheduledTimeout(passwordSuccessTimer);
			});

			if (newPassword !== confirmPassword) {
				yield* Effect.sync(() => {
					passwordFieldError = "Passwords do not match.";
				});
				return;
			}

			if (newPassword.length < 4) {
				yield* Effect.sync(() => {
					passwordFieldError = "Password must be at least 4 characters.";
				});
				return;
			}

			yield* Effect.sync(() => {
				passwordLoading = true;
			});

			const result = yield* Effect.match(
				Effect.promise(() => runApiEffect(updateUserPassword(clientState.config, clientState.userId, newPassword))),
				{
					onFailure: (error) => ({ ok: false as const, error }),
					onSuccess: () => ({ ok: true as const })
				}
			);

			yield* Effect.sync(() => {
				if (!result.ok) {
					passwordError = formatApiFailure(result.error);
					passwordLoading = false;
					return;
				}

				toast.ok("Password updated successfully.");
				currentPassword = "";
				newPassword = "";
				confirmPassword = "";
				passwordSuccess = true;
				passwordSuccessTimer = scheduleTimeout(() => {
					passwordSuccess = false;
					passwordSuccessTimer = null;
				}, 3000);
				passwordLoading = false;
			});
		});

	async function changePassword(): Promise<void> {
		await Effect.runPromise(changePasswordProgram());
	}

	const loadApiKeysProgram = (options?: { silent?: boolean }): Effect.Effect<void, never> =>
		Effect.gen(function* () {
			const requestVersion = ++apiKeysRequestVersion;

			yield* Effect.sync(() => {
				apiKeysError = "";
				if (!options?.silent) {
					apiKeysLoading = true;
				}
			});

			const result = yield* Effect.match(
				Effect.promise(() => runApiEffect(listApiKeys(clientState.config))),
				{
					onFailure: (error) => ({ ok: false as const, error }),
					onSuccess: (keys) => ({ ok: true as const, keys })
				}
			);

			yield* Effect.sync(() => {
				if (requestVersion !== apiKeysRequestVersion) {
					return;
				}

				if (!result.ok) {
					apiKeysError = formatApiFailure(result.error);
					apiKeysLoading = false;
					return;
				}

				apiKeys = result.keys;
				apiKeysLoading = false;
			});
		});

	async function loadApiKeys(options?: { silent?: boolean }): Promise<void> {
		await Effect.runPromise(loadApiKeysProgram(options));
	}

	const createApiKeyProgram = (): Effect.Effect<void, never> =>
		Effect.gen(function* () {
			const name = apiKeyName.trim();

			yield* Effect.sync(() => {
				apiKeyNameError = "";
			});

			if (name.length === 0) {
				yield* Effect.sync(() => {
					apiKeyNameError = "Name is required.";
				});
				return;
			}

			yield* Effect.sync(() => {
				apiKeyCreateLoading = true;
				clearCreatedApiKeyState();
			});

			const result = yield* Effect.match(
				Effect.promise(() => runApiEffect(createApiKey(clientState.config, { name }))),
				{
					onFailure: (error) => ({ ok: false as const, error }),
					onSuccess: (response) => ({ ok: true as const, response })
				}
			);

			if (!result.ok) {
				yield* Effect.sync(() => {
					apiKeyCreateLoading = false;
					apiKeyNameError = formatApiFailure(result.error);
				});
				return;
			}

			yield* Effect.sync(() => {
				apiKeyName = "";
				createdApiKeySecret = result.response.secret;
				createdApiKeyName = result.response.api_key.name ?? name;
				apiKeyCreateLoading = false;
			});

			yield* Effect.sync(() => {
				toast.ok("API key created.");
			});

			yield* loadApiKeysProgram({ silent: true });
		});

	async function submitApiKeyCreate(): Promise<void> {
		await Effect.runPromise(createApiKeyProgram());
	}

	const revokeApiKeyProgram = (apiKeyId: string): Effect.Effect<void, never> =>
		Effect.gen(function* () {
			yield* Effect.sync(() => {
				revokeLoadingId = apiKeyId;
			});

			const result = yield* Effect.match(
				Effect.promise(() => runApiEffect(revokeApiKey(clientState.config, apiKeyId))),
				{
					onFailure: (error) => ({ ok: false as const, error }),
					onSuccess: () => ({ ok: true as const })
				}
			);

			yield* Effect.sync(() => {
				if (!result.ok) {
					revokeLoadingId = null;
					toast.error(formatApiFailure(result.error));
					return;
				}

				clearRevokeConfirm();
				revokeLoadingId = null;
				toast.ok("API key revoked.");
			});

			if (result.ok) {
				yield* loadApiKeysProgram({ silent: true });
			}
		});

	async function requestApiKeyRevoke(apiKey: ApiKeySummary): Promise<void> {
		if (apiKey.revoked_at || revokeLoadingId !== null) {
			return;
		}

		if (revokeConfirmId === apiKey.id) {
			await Effect.runPromise(revokeApiKeyProgram(apiKey.id));
			return;
		}

		clearRevokeConfirm();
		revokeConfirmId = apiKey.id;
		revokeConfirmTimer = scheduleTimeout(() => {
			revokeConfirmId = null;
			revokeConfirmTimer = null;
		}, 3000);
	}

	async function copyCreatedApiKeySecret(): Promise<void> {
		if (!createdApiKeySecret) {
			return;
		}

		try {
			if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
				await navigator.clipboard.writeText(createdApiKeySecret);
			} else if (typeof document !== "undefined") {
				const input = document.createElement("textarea");
				input.value = createdApiKeySecret;
				input.setAttribute("readonly", "true");
				input.style.position = "absolute";
				input.style.left = "-9999px";
				document.body.append(input);
				input.select();
				const copied = document.execCommand("copy");
				input.remove();

				if (!copied) {
					throw new Error("Copy command was rejected.");
				}
			}

			createdApiKeyCopied = true;
			scheduleCopiedReset();
			toast.ok("API key copied.");
		} catch {
			toast.error("Couldn't copy the API key.");
		}
	}

	const pingAndToastProgram = (): Effect.Effect<void, never> =>
		Effect.gen(function* () {
			const toastId = yield* Effect.sync(() => toast.loading("Testing connection..."));
			yield* Effect.promise(() => checkHealth());
			yield* Effect.sync(() => {
				if (authController.health === "ok") {
					toast.update(toastId, "ok", "Connected.");
					return;
				}

				toast.update(toastId, "error", authController.healthMessage);
			});
		});

	async function pingAndToast(): Promise<void> {
		await Effect.runPromise(pingAndToastProgram());
	}

	$effect(() => {
		clientState.baseUrl;
		endpointValue = clientState.baseUrl;
	});

	$effect(() => {
		if (!clientState.isAuthenticated) {
			apiKeysRequestVersion += 1;
			apiKeys = [];
			apiKeysError = "";
			apiKeysLoading = false;
			clearCreatedApiKeyState();
			clearRevokeConfirm();
			revokeLoadingId = null;
			return;
		}

		clientState.config.baseUrl;
		clientState.config.token;
		void loadApiKeys();
	});

	$effect(() => {
		return () => {
			clearRevokeConfirm();
			clearCreatedApiKeyState();
		};
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
		<div class="settings-page anim-fade-up">
			<div class="settings-header">
				<div>
					<p class="section-label">Configuration</p>
					<h1 class="settings-title">Settings</h1>
				</div>
			</div>

			<!-- Connection Settings -->
			<section class="settings-section panel">
				<div class="panel-header">
					<span class="panel-title">Connection</span>
				</div>
				<div class="panel-body">
					<div class="settings-field-group">
						<div class="field-description">
							<h3 class="settings-field-title">API Endpoint</h3>
							<p class="settings-field-copy">The base URL of your open-sandbox server. Changes take effect immediately.</p>
						</div>
						<div class="endpoint-row">
							<div class="endpoint-input-wrap">
								<input
									class="field endpoint-field"
									class:endpoint-field--invalid={endpointValue.length > 0 && !endpointValid}
									class:endpoint-field--valid={endpointValid && endpointDirty}
									bind:value={endpointValue}
									spellcheck={false}
								placeholder="http://localhost:8080"
									onkeydown={(e) => { if (e.key === "Enter") applyEndpoint(); }}
								/>
								<div class="endpoint-status">
									{#if endpointValue.length > 0 && !endpointValid}
										<span class="endpoint-badge endpoint-badge--error">Invalid URL</span>
									{:else if endpointSaved}
										<span class="endpoint-badge endpoint-badge--ok">Saved</span>
									{/if}
								</div>
							</div>
							<button class="btn-primary btn-sm" type="button" onclick={applyEndpoint} disabled={!endpointValid || !endpointDirty}>
								Apply
							</button>
						</div>

						<!-- Health -->
						<div class="health-bar">
							<div class="health-indicator">
								<span class="health-dot" style="
									background: {authController.health === 'ok' ? 'var(--status-ok)' : authController.health === 'error' ? 'var(--status-error)' : authController.health === 'checking' ? 'var(--status-warn)' : 'var(--text-muted)'};
									box-shadow: {authController.health === 'ok' ? '0 0 6px rgba(74,222,128,0.4)' : 'none'};
								"></span>
								<span class="health-label">{authController.healthMessage}</span>
							</div>
							<button class="btn-ghost btn-sm" type="button" onclick={() => void pingAndToast()}>
								{authController.health === "checking" ? "Checking..." : "Test connection"}
							</button>
						</div>
					</div>
				</div>
			</section>

			<!-- Account Settings -->
			<section class="settings-section panel">
				<div class="panel-header">
					<span class="panel-title">Account</span>
				</div>
				<div class="panel-body">
					<!-- Who am I -->
					<div class="account-identity">
						<div class="account-avatar">{clientState.username[0]?.toUpperCase() ?? "?"}</div>
						<div class="account-meta">
							<span class="account-name">{clientState.username}</span>
							<span class="account-role-badge account-role-badge--{clientState.role}">{clientState.role}</span>
						</div>
					</div>

					<div class="settings-divider"></div>

					<!-- Change password -->
					<div class="settings-field-group">
						<div class="field-description">
							<h3 class="settings-field-title">Change password</h3>
							<p class="settings-field-copy">Update the password for your account.</p>
						</div>
						<form
							class="password-form"
							onsubmit={(e) => { e.preventDefault(); void changePassword(); }}
						>
							<label class="field-col">
								<span class="section-label">New password</span>
								<input
									class="field"
									type="password"
									bind:value={newPassword}
									autocomplete="new-password"
									required
									placeholder="Min. 4 characters"
								/>
							</label>
							<label class="field-col">
								<span class="section-label">Confirm password</span>
								<input
									class="field"
									class:field--error={confirmPassword.length > 0 && confirmPassword !== newPassword}
									type="password"
									bind:value={confirmPassword}
									autocomplete="new-password"
									required
									placeholder="Repeat new password"
								/>
								{#if passwordFieldError}
									<span class="field-inline-error">{passwordFieldError}</span>
								{/if}
							</label>
						{#if passwordError}
							<p class="alert-error">{passwordError}</p>
						{/if}
						{#if passwordSuccess}
							<div class="password-success">
								<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
								<span>Password updated</span>
							</div>
						{/if}
						<div class="form-footer">
								<button class="btn-primary btn-sm" type="submit" disabled={passwordLoading || !newPassword || !confirmPassword}>
									{passwordLoading ? "Saving..." : "Update password"}
								</button>
							</div>
						</form>
					</div>
				</div>
			</section>

			<section class="settings-section panel">
				<div class="panel-header">
					<span class="panel-title">API keys</span>
				</div>
				<div class="panel-body api-keys-panel">
					<div class="settings-field-group">
						<div class="field-description">
							<h3 class="settings-field-title">Personal access keys</h3>
							<p class="settings-field-copy">Create scoped credentials for CLI or SDK access. New secrets are only shown once after creation.</p>
						</div>

						<form class="api-key-create-form" onsubmit={(e) => { e.preventDefault(); void submitApiKeyCreate(); }}>
							<label class="field-col api-key-name-field">
								<span class="section-label">Key name</span>
								<input
									class="field"
									bind:value={apiKeyName}
									maxlength={120}
									placeholder="Local CLI, CI, personal script..."
								/>
							</label>
							<div class="api-key-actions">
								<button class="btn-ghost btn-sm" type="button" onclick={() => void loadApiKeys({ silent: false })} disabled={apiKeysLoading}>
									{apiKeysLoading ? "Refreshing..." : "Refresh"}
								</button>
								<button class="btn-primary btn-sm" type="submit" disabled={apiKeyCreateLoading || apiKeyName.trim().length === 0}>
									{apiKeyCreateLoading ? "Creating..." : "Create key"}
								</button>
							</div>
						</form>

						{#if apiKeyNameError}
							<p class="alert-error">{apiKeyNameError}</p>
						{/if}

						{#if createdApiKeySecret}
							<div class="api-key-secret-card">
								<div class="api-key-secret-header">
									<div>
										<p class="section-label">New key</p>
										<p class="api-key-secret-title">{createdApiKeyName}</p>
									</div>
									<button class="btn-ghost btn-sm" type="button" onclick={() => void copyCreatedApiKeySecret()}>
										{createdApiKeyCopied ? "Copied" : "Copy secret"}
									</button>
								</div>
								<p class="settings-field-copy">Save this secret now. It will not be shown again.</p>
								<code class="api-key-secret-value">{createdApiKeySecret}</code>
							</div>
						{/if}

						{#if apiKeysError}
							<p class="alert-error">{apiKeysError}</p>
						{/if}

						<div class="api-key-list" aria-busy={apiKeysLoading}>
							{#if apiKeys.length === 0 && !apiKeysLoading && !apiKeysError}
								<div class="api-key-empty">
									<p class="api-key-empty-title">No API keys yet</p>
									<p class="settings-field-copy">Create a named key when you need a personal token outside the web UI.</p>
								</div>
							{:else}
								{#each apiKeys as apiKey (apiKey.id)}
									<div class="api-key-row">
										<div class="api-key-meta">
											<div class="api-key-name-row">
												<span class="api-key-name">{apiKey.name || "Unnamed key"}</span>
												{#if apiKey.revoked_at}
													<span class="api-key-state api-key-state--revoked">Revoked</span>
												{:else}
													<span class="api-key-state api-key-state--active">Active</span>
												{/if}
											</div>
											<div class="api-key-detail-grid">
												<span><strong>Preview:</strong> {apiKey.preview || "—"}</span>
												<span><strong>Created:</strong> {formatDate(apiKey.created_at)}</span>
												{#if apiKey.revoked_at}
													<span><strong>Revoked:</strong> {formatDate(apiKey.revoked_at)}</span>
												{/if}
											</div>
										</div>
										<div class="api-key-row-actions">
					<button
						class="delete-btn"
						class:delete-btn--confirm={revokeConfirmId === apiKey.id}
						type="button"
						onclick={() => void requestApiKeyRevoke(apiKey)}
						disabled={Boolean(apiKey.revoked_at) || revokeLoadingId !== null}
					>
												{#if revokeLoadingId === apiKey.id}
													Revoking...
												{:else if apiKey.revoked_at}
													Revoked
												{:else if revokeConfirmId === apiKey.id}
													Confirm revoke
												{:else}
													Revoke
												{/if}
											</button>
										</div>
									</div>
								{/each}
							{/if}
						</div>
					</div>
				</div>
			</section>
		</div>
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
		background: radial-gradient(ellipse 60% 50% at 50% 40%, rgba(255,255,255,0.025) 0%, transparent 70%);
	}
	.auth-card {
		position: relative;
		z-index: 1;
		padding: 2rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-xl);
	}
	.auth-checking {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--text-muted);
	}

	.settings-page {
		display: flex;
		flex-direction: column;
		gap: 1.5rem;
		padding: 1.5rem;
		max-width: 56rem;
	}

	.settings-header {
		padding-bottom: 0.875rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.settings-title {
		margin: 0.2rem 0 0;
		font-family: var(--font-display);
		font-size: 1.5rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.settings-section {
		overflow: visible;
	}

	.settings-field-group {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.field-description {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.settings-field-title {
		margin: 0;
		font-size: 0.88rem;
		font-weight: 500;
		color: var(--text-primary);
	}

	.settings-field-copy {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-muted);
		line-height: 1.5;
	}

	/* Endpoint */
	.endpoint-row {
		display: flex;
		align-items: flex-start;
		gap: 0.75rem;
	}

	.endpoint-input-wrap {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
		min-width: 0;
	}

	.endpoint-field {
		font-size: 0.78rem;
	}

	.endpoint-field--invalid {
		border-color: var(--status-error-border);
	}

	.endpoint-field--valid {
		border-color: var(--status-ok-border);
	}

	.endpoint-status {
		min-height: 1rem;
	}

	.endpoint-badge {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.12rem 0.4rem;
		border-radius: 999px;
		border: 1px solid;
	}

	.endpoint-badge--error {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}

	.endpoint-badge--ok {
		color: var(--status-ok);
		border-color: var(--status-ok-border);
		background: var(--status-ok-bg);
	}

	/* Health bar */
	.health-bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.75rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}

	.health-indicator {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.health-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		flex-shrink: 0;
		transition: background 0.3s;
	}

	.health-label {
		font-family: var(--font-mono);
		font-size: 0.7rem;
		color: var(--text-secondary);
	}

	/* Account identity */
	.account-identity {
		display: flex;
		align-items: center;
		gap: 0.875rem;
		padding: 0.75rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}

	.account-avatar {
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 50%;
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		font-family: var(--font-mono);
		font-size: 0.88rem;
		font-weight: 600;
		color: var(--text-primary);
		flex-shrink: 0;
	}

	.account-meta {
		display: flex;
		align-items: center;
		gap: 0.625rem;
	}

	.account-name {
		font-size: 0.9rem;
		font-weight: 500;
		color: var(--text-primary);
	}

	.account-role-badge {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		padding: 0.15rem 0.5rem;
		border-radius: 999px;
		border: 1px solid;
	}

	.account-role-badge--admin {
		color: #fbbf24;
		border-color: rgba(251, 191, 36, 0.25);
		background: rgba(251, 191, 36, 0.07);
	}

	.account-role-badge--member {
		color: var(--text-muted);
		border-color: var(--border-mid);
		background: transparent;
	}

	.settings-divider {
		height: 1px;
		background: var(--border-dim);
		margin: 0.25rem 0;
	}

	/* Password form */
	.password-form {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.75rem;
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

	.password-success {
		grid-column: span 2;
		display: inline-flex;
		align-items: center;
		gap: 0.45rem;
		padding: 0.5rem 0.75rem;
		background: var(--status-ok-bg);
		border: 1px solid var(--status-ok-border);
		border-radius: var(--radius-sm);
		color: var(--status-ok);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		animation: fade-success 0.2s ease;
	}

	@keyframes fade-success {
		from { opacity: 0; transform: translateY(-3px); }
		to   { opacity: 1; transform: translateY(0); }
	}

	.field-col {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.password-form .alert-error {
		grid-column: span 2;
	}

	.form-footer {
		grid-column: span 2;
		display: flex;
		justify-content: flex-end;
	}

	.api-keys-panel {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}

	.api-key-create-form {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: 0.75rem;
		align-items: end;
	}

	.api-key-name-field {
		min-width: 0;
	}

	.api-key-actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.api-key-secret-card,
	.api-key-empty,
	.api-key-row {
		padding: 0.85rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}

	.api-key-secret-card {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		border-color: var(--status-ok-border);
		background: color-mix(in srgb, var(--bg-raised) 88%, var(--status-ok-bg));
	}

	.api-key-secret-header {
		display: flex;
		justify-content: space-between;
		gap: 1rem;
		align-items: flex-start;
	}

	.api-key-secret-title,
	.api-key-empty-title,
	.api-key-name {
		margin: 0.18rem 0 0;
		font-size: 0.85rem;
		font-weight: 500;
		color: var(--text-primary);
	}

	.api-key-secret-value {
		display: block;
		padding: 0.75rem;
		border-radius: var(--radius-sm);
		background: var(--bg-overlay);
		border: 1px solid var(--border-mid);
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--text-primary);
		word-break: break-all;
	}

	.api-key-list {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.api-key-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
	}

	.api-key-meta {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
		min-width: 0;
	}

	.api-key-name-row {
		display: flex;
		align-items: center;
		gap: 0.55rem;
		flex-wrap: wrap;
	}

	.api-key-state {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		padding: 0.14rem 0.48rem;
		border-radius: 999px;
		border: 1px solid;
	}

	.api-key-state--active {
		color: var(--status-ok);
		border-color: var(--status-ok-border);
		background: var(--status-ok-bg);
	}

	.api-key-state--revoked {
		color: var(--text-muted);
		border-color: var(--border-mid);
		background: transparent;
	}

	.api-key-detail-grid {
		display: flex;
		gap: 0.85rem;
		flex-wrap: wrap;
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-muted);
	}

	.api-key-detail-grid strong {
		font-weight: 500;
		color: var(--text-secondary);
	}

	.api-key-row-actions {
		flex-shrink: 0;
	}

	.delete-btn {
		appearance: none;
		border: 1px solid rgba(248, 113, 113, 0.24);
		background: rgba(248, 113, 113, 0.08);
		color: #fca5a5;
		border-radius: var(--radius-sm);
		padding: 0.5rem 0.75rem;
		font-size: 0.72rem;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s ease, border-color 0.15s ease, color 0.15s ease;
	}

	.delete-btn:hover:enabled {
		background: rgba(248, 113, 113, 0.12);
		border-color: rgba(248, 113, 113, 0.34);
	}

	.delete-btn--confirm {
		background: rgba(248, 113, 113, 0.18);
		border-color: rgba(248, 113, 113, 0.5);
		color: #fecaca;
	}

	.delete-btn:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	@media (max-width: 640px) {
		.settings-page { padding: 1rem; }
		.password-form { grid-template-columns: 1fr; }
		.password-form .alert-error,
		.password-success,
		.form-footer { grid-column: span 1; }
		.endpoint-row { flex-direction: column; }
		.endpoint-row .btn-primary { width: 100%; }
		.api-key-create-form,
		.api-key-row { grid-template-columns: 1fr; display: flex; flex-direction: column; align-items: stretch; }
		.api-key-secret-header,
		.api-key-actions { flex-direction: column; align-items: stretch; }
		.api-key-row-actions { width: 100%; }
		.api-key-row-actions .delete-btn { width: 100%; }
	}
</style>
