<script lang="ts">
	/**
	 * ProxyConfigEditor – editable proxy settings for a single service / port.
	 * Emits config via the bindable `value` prop (null = no config).
	 * Optionally also calls `onchange(value)` after each commit.
	 */
	import type { SandboxPortProxyConfig } from "$lib/api";
	import Checkbox from "$lib/components/Checkbox.svelte";

	let {
		value = $bindable<SandboxPortProxyConfig | null>(null),
		onchange
	} = $props<{
		value?: SandboxPortProxyConfig | null;
		onchange?: (v: SandboxPortProxyConfig | null) => void;
	}>();

	// ── local editable state ───────────────────────────────────────────────────
	type KVRow = { id: number; key: string; val: string };
	let nextId = 0;

	function parseHeaders(map: Record<string, string> | undefined): KVRow[] {
		if (!map) return [];
		return Object.entries(map).map(([key, val]) => ({ id: nextId++, key, val }));
	}

	function rowsToMap(rows: KVRow[]): Record<string, string> | undefined {
		const entries = rows.filter(r => r.key.trim() !== "");
		if (entries.length === 0) return undefined;
		return Object.fromEntries(entries.map(r => [r.key.trim(), r.val]));
	}

	function applyValue(nextValue: SandboxPortProxyConfig | null): void {
		const next = nextValue ?? {};
		reqRows = parseHeaders(next.request_headers);
		respRows = parseHeaders(next.response_headers);
		pathStrip = next.path_prefix_strip ?? "";
		skipAuth = next.skip_auth ?? false;
		corsEnabled = next.cors != null;
		corsOrigins = next.cors?.allow_origins?.join(", ") ?? "";
		corsMethods = next.cors?.allow_methods?.join(", ") ?? "";
		corsHeaders = next.cors?.allow_headers?.join(", ") ?? "";
		corsCreds = next.cors?.allow_credentials ?? false;
		corsMaxAge = String(next.cors?.max_age ?? "");
	}

	let reqRows    = $state<KVRow[]>([]);
	let respRows   = $state<KVRow[]>([]);
	let pathStrip  = $state("");
	let skipAuth   = $state(false);
	let corsEnabled = $state(false);

	// CORS sub-fields
	let corsOrigins = $state("");
	let corsMethods = $state("");
	let corsHeaders = $state("");
	let corsCreds   = $state(false);
	let corsMaxAge  = $state("");

	$effect(() => {
		applyValue(value);
	});

	function splitCSV(s: string): string[] {
		return s.split(",").map(x => x.trim()).filter(Boolean);
	}

	function addRow(rows: KVRow[]): KVRow[] {
		return [...rows, { id: nextId++, key: "", val: "" }];
	}
	function removeRow(rows: KVRow[], id: number): KVRow[] {
		return rows.filter(r => r.id !== id);
	}
	function updateRowKey(rows: KVRow[], id: number, k: string): KVRow[] {
		return rows.map(r => r.id === id ? { ...r, key: k } : r);
	}
	function updateRowVal(rows: KVRow[], id: number, v: string): KVRow[] {
		return rows.map(r => r.id === id ? { ...r, val: v } : r);
	}

	function commit(): void {
		const reqMap  = rowsToMap(reqRows);
		const respMap = rowsToMap(respRows);
		const strip   = pathStrip.trim();

		let corsVal: SandboxPortProxyConfig["cors"] | undefined = undefined;
		if (corsEnabled) {
			const origins = splitCSV(corsOrigins);
			const methods = splitCSV(corsMethods);
			const headers = splitCSV(corsHeaders);
			const maxAgeNum = parseInt(corsMaxAge, 10);
			corsVal = {
				allow_origins:      origins.length > 0 ? origins : undefined,
				allow_methods:      methods.length > 0 ? methods : undefined,
				allow_headers:      headers.length > 0 ? headers : undefined,
				allow_credentials:  corsCreds || undefined,
				max_age:            Number.isFinite(maxAgeNum) && maxAgeNum > 0 ? maxAgeNum : undefined
			};
		}

		const hasContent =
			reqMap !== undefined ||
			respMap !== undefined ||
			strip !== "" ||
			skipAuth ||
			corsVal !== undefined;

		if (!hasContent) {
			value = null;
			onchange?.(null);
			return;
		}

		value = {
			request_headers:   reqMap,
			response_headers:  respMap,
			path_prefix_strip: strip || undefined,
			skip_auth:         skipAuth || undefined,
			cors:              corsVal
		};
		onchange?.(value);
	}

	function toggleCORS(): void {
		corsEnabled = !corsEnabled;
		commit();
	}
</script>

<div class="pce">
	<!-- Request headers -->
	<div class="pce-section">
		<div class="pce-section-head">
			<span class="pce-label">Request headers</span>
			<button class="pce-add" type="button" onclick={() => { reqRows = addRow(reqRows); }}>
				<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
				Add
			</button>
		</div>
		{#if reqRows.length > 0}
			<div class="pce-kv-list">
				{#each reqRows as row (row.id)}
					<div class="pce-kv-row">
						<input class="field pce-field" type="text" placeholder="X-Header" value={row.key}
							oninput={(e) => { reqRows = updateRowKey(reqRows, row.id, (e.currentTarget as HTMLInputElement).value); commit(); }} />
						<span class="pce-kv-sep">:</span>
						<input class="field pce-field" type="text" placeholder="value" value={row.val}
							oninput={(e) => { reqRows = updateRowVal(reqRows, row.id, (e.currentTarget as HTMLInputElement).value); commit(); }} />
						<button class="pce-remove" type="button" onclick={() => { reqRows = removeRow(reqRows, row.id); commit(); }} aria-label="Remove">
							<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
						</button>
					</div>
				{/each}
			</div>
		{:else}
			<p class="pce-empty">No request headers configured.</p>
		{/if}
	</div>

	<!-- Response headers -->
	<div class="pce-section">
		<div class="pce-section-head">
			<span class="pce-label">Response headers</span>
			<button class="pce-add" type="button" onclick={() => { respRows = addRow(respRows); }}>
				<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
				Add
			</button>
		</div>
		{#if respRows.length > 0}
			<div class="pce-kv-list">
				{#each respRows as row (row.id)}
					<div class="pce-kv-row">
						<input class="field pce-field" type="text" placeholder="X-Header" value={row.key}
							oninput={(e) => { respRows = updateRowKey(respRows, row.id, (e.currentTarget as HTMLInputElement).value); commit(); }} />
						<span class="pce-kv-sep">:</span>
						<input class="field pce-field" type="text" placeholder="value" value={row.val}
							oninput={(e) => { respRows = updateRowVal(respRows, row.id, (e.currentTarget as HTMLInputElement).value); commit(); }} />
						<button class="pce-remove" type="button" onclick={() => { respRows = removeRow(respRows, row.id); commit(); }} aria-label="Remove">
							<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
						</button>
					</div>
				{/each}
			</div>
		{:else}
			<p class="pce-empty">No response headers configured.</p>
		{/if}
	</div>

	<!-- Strip prefix + skip auth -->
	<div class="pce-section pce-section--inline">
		<label class="pce-inline-field">
			<span class="pce-label">Strip path prefix</span>
			<input class="field pce-field" type="text" placeholder="/api" bind:value={pathStrip}
				oninput={commit} />
		</label>
		<div class="pce-checkbox-row">
			<Checkbox bind:checked={skipAuth} onchange={commit} label="Skip auth" />
		</div>
	</div>

	<!-- CORS -->
	<div class="pce-section">
		<div class="pce-section-head">
			<span class="pce-label">CORS</span>
			<button class="pce-toggle" type="button" onclick={toggleCORS}>
				{corsEnabled ? "Disable" : "Enable"}
			</button>
		</div>
		{#if corsEnabled}
			<div class="pce-cors-grid">
				<label class="pce-cors-field">
					<span class="pce-sublabel">Allow origins <span class="pce-opt">(comma-separated)</span></span>
					<input class="field pce-field" type="text" placeholder="https://app.example.com, *"
						bind:value={corsOrigins} oninput={commit} />
				</label>
				<label class="pce-cors-field">
					<span class="pce-sublabel">Allow methods</span>
					<input class="field pce-field" type="text" placeholder="GET, POST, OPTIONS"
						bind:value={corsMethods} oninput={commit} />
				</label>
				<label class="pce-cors-field">
					<span class="pce-sublabel">Allow headers</span>
					<input class="field pce-field" type="text" placeholder="Authorization, Content-Type"
						bind:value={corsHeaders} oninput={commit} />
				</label>
				<div class="pce-cors-bottom">
					<Checkbox bind:checked={corsCreds} onchange={commit} label="Allow credentials" />
					<label class="pce-cors-field pce-cors-field--narrow">
						<span class="pce-sublabel">Max age (s)</span>
						<input class="field pce-field" type="number" min="0" placeholder="3600"
							bind:value={corsMaxAge} oninput={commit} />
					</label>
				</div>
			</div>
		{/if}
	</div>
</div>

<style>
	.pce {
		display: flex;
		flex-direction: column;
		gap: 0;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		overflow: hidden;
	}

	.pce-section {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
		padding: 0.65rem 0.75rem;
		border-bottom: 1px solid var(--border-dim);
	}

	.pce-section:last-child {
		border-bottom: none;
	}

	.pce-section--inline {
		flex-direction: row;
		align-items: flex-start;
		flex-wrap: wrap;
		gap: 0.75rem;
	}

	.pce-section-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}

	.pce-label {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		text-transform: uppercase;
		letter-spacing: 0.07em;
		white-space: nowrap;
	}

	.pce-sublabel {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
	}

	.pce-opt {
		font-size: 0.58rem;
		opacity: 0.7;
	}

	.pce-empty {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		opacity: 0.5;
	}

	.pce-add {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		background: transparent;
		border: 1px dashed var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.18rem 0.45rem;
		cursor: pointer;
		transition: color 0.1s, border-color 0.1s;
	}
	.pce-add:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		border-style: solid;
	}

	.pce-toggle {
		display: inline-flex;
		align-items: center;
		background: transparent;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.6rem;
		padding: 0.18rem 0.5rem;
		cursor: pointer;
		transition: color 0.1s, border-color 0.1s, background 0.1s;
	}
	.pce-toggle:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}

	.pce-kv-list {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.pce-kv-row {
		display: grid;
		grid-template-columns: 1fr 0.6rem 1.2fr 1.5rem;
		gap: 0.3rem;
		align-items: center;
	}

	.pce-kv-sep {
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-muted);
		text-align: center;
	}

	.pce-field {
		padding: 0.28rem 0.5rem;
		font-size: 0.68rem;
	}

	.pce-remove {
		display: grid;
		place-items: center;
		width: 20px;
		height: 20px;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		cursor: pointer;
		transition: color 0.1s, border-color 0.1s, background 0.1s;
	}
	.pce-remove:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}

	.pce-inline-field {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		flex: 1 1 9rem;
	}

	.pce-checkbox-row {
		display: flex;
		align-items: flex-end;
		padding-top: 1.1rem;
	}

	.pce-cors-grid {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
	}

	.pce-cors-field {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
	}

	.pce-cors-field--narrow {
		max-width: 8rem;
	}

	.pce-cors-bottom {
		display: flex;
		align-items: flex-start;
		gap: 1rem;
		flex-wrap: wrap;
	}
</style>
