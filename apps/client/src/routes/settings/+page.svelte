<script lang="ts">
	import { authController, checkHealth, signOut } from "$lib/auth-controller.svelte";
	import { clearScheduledTimeout, scheduleTimeout, type TimeoutHandle } from "$lib/client/browser";
	import PageShell from "$lib/components/PageShell.svelte";
	import {
		formatApiFailure,
		runApiEffect,
		updateUserPassword
	} from "$lib/api";
	import { clientState, setBaseUrl } from "$lib/stores.svelte";
	import { toast } from "$lib/toast.svelte";

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

	const isValidUrl = (v: string): boolean => {
		try { new URL(v); return true; } catch { return false; }
	};

	const endpointValid = $derived(isValidUrl(endpointValue));

	function applyEndpoint(): void {
		if (!endpointValid) return;
		setBaseUrl(endpointValue.replace(/\/$/, ""));
		endpointSaved = true;
		clearScheduledTimeout(endpointSavedTimer);
		endpointSavedTimer = scheduleTimeout(() => {
			endpointSaved = false;
			endpointSavedTimer = null;
		}, 2000);
	}

	async function changePassword(): Promise<void> {
		passwordError = "";
		passwordFieldError = "";
		passwordSuccess = false;
		clearScheduledTimeout(passwordSuccessTimer);
		if (newPassword !== confirmPassword) {
			passwordFieldError = "Passwords do not match.";
			return;
		}
		if (newPassword.length < 4) {
			passwordFieldError = "Password must be at least 4 characters.";
			return;
		}
		passwordLoading = true;
		try {
			await runApiEffect(updateUserPassword(clientState.config, clientState.userId, newPassword));
			toast.ok("Password updated successfully.");
			currentPassword = "";
			newPassword = "";
			confirmPassword = "";
			passwordSuccess = true;
			passwordSuccessTimer = scheduleTimeout(() => {
				passwordSuccess = false;
				passwordSuccessTimer = null;
			}, 3000);
		} catch (error) {
			passwordError = formatApiFailure(error);
		} finally {
			passwordLoading = false;
		}
	}


	async function pingAndToast(): Promise<void> {
		const toastId = toast.loading("Testing connection...");
		await checkHealth();
		if (authController.health === "ok") {
			toast.update(toastId, "ok", "Connected.");
		} else {
			toast.update(toastId, "error", authController.healthMessage);
		}
	}

	$effect(() => {
		clientState.baseUrl;
		endpointValue = clientState.baseUrl;
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

	@media (max-width: 640px) {
		.settings-page { padding: 1rem; }
		.password-form { grid-template-columns: 1fr; }
		.password-form .alert-error,
		.password-success,
		.form-footer { grid-column: span 1; }
		.endpoint-row { flex-direction: column; }
		.endpoint-row .btn-primary { width: 100%; }
	}
</style>
