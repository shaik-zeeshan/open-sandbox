<script lang="ts">
	import { untrack } from "svelte";
	import { normalizeSandboxEnv, parseSandboxEnvEntry, serializeSandboxEnvEntry } from "$lib/sandbox-env";

	let {
		value = $bindable<string[]>([]),
		addLabel = "Add variable",
		emptyMessage = "No environment variables configured.",
		keyPlaceholder = "NODE_ENV",
		valuePlaceholder = "production",
		valueInputType = "text"
	} = $props<{
		value?: string[];
		addLabel?: string;
		emptyMessage?: string;
		keyPlaceholder?: string;
		valuePlaceholder?: string;
		valueInputType?: "text" | "password";
	}>();

	type EnvRow = { id: number; key: string; value: string };
	let nextId = 0;

	const parseRows = (entries: string[], previousRows: EnvRow[] = []): EnvRow[] =>
		normalizeSandboxEnv(entries).map((entry, index) => {
			const parsed = parseSandboxEnvEntry(entry);
			const previous = previousRows[index];
			return {
				id: previous?.id ?? nextId++,
				key: parsed.key,
				value: parsed.value
			};
		});

	const serializeRows = (rows: EnvRow[]): string[] =>
		rows
			.map((row) => ({ key: row.key.trim(), value: row.value }))
			.filter((row) => row.key.length > 0)
			.map((row) => serializeSandboxEnvEntry(row.key, row.value));

	let rows = $state<EnvRow[]>(parseRows(value));

	$effect(() => {
		const nextValue = normalizeSandboxEnv(value);
		const previousRows = untrack(() => rows);
		if (JSON.stringify(nextValue) === JSON.stringify(serializeRows(previousRows))) {
			return;
		}
		rows = parseRows(nextValue, previousRows);
	});

	function commit(): void {
		value = serializeRows(rows);
	}

	function addRow(): void {
		rows = [...rows, { id: nextId++, key: "", value: "" }];
	}

	function removeRow(id: number): void {
		rows = rows.filter((row) => row.id !== id);
		commit();
	}

	function updateRow(id: number, field: "key" | "value", nextValue: string): void {
		rows = rows.map((row) => (row.id === id ? { ...row, [field]: nextValue } : row));
		commit();
	}

	function handleValueKeydown(event: KeyboardEvent, row: EnvRow): void {
		if (event.key === "Enter") {
			event.preventDefault();
			addRow();
			return;
		}

		if (event.key === "Backspace" && row.key === "" && row.value === "" && rows.length > 1) {
			event.preventDefault();
			removeRow(row.id);
		}
	}
</script>

<div class="env-editor">
	{#if rows.length > 0}
		<div class="env-header">
			<span class="env-col-label">Key</span>
			<span class="env-separator-spacer"></span>
			<span class="env-col-label">Value</span>
			<span class="env-remove-spacer"></span>
		</div>
		<div class="env-rows">
			{#each rows as row (row.id)}
				<div class="env-row">
					<input
						class="field env-input env-input--key"
						type="text"
						placeholder={keyPlaceholder}
						value={row.key}
						oninput={(event) => updateRow(row.id, "key", (event.currentTarget as HTMLInputElement).value)}
					/>
					<span class="env-separator">=</span>
					<input
						class="field env-input"
						type={valueInputType}
						placeholder={valuePlaceholder}
						value={row.value}
						oninput={(event) => updateRow(row.id, "value", (event.currentTarget as HTMLInputElement).value)}
						onkeydown={(event) => handleValueKeydown(event, row)}
					/>
					<button class="env-remove" type="button" onclick={() => removeRow(row.id)} aria-label="Remove environment variable">
						<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
							<line x1="18" y1="6" x2="6" y2="18"/>
							<line x1="6" y1="6" x2="18" y2="18"/>
						</svg>
					</button>
				</div>
			{/each}
		</div>
	{:else}
		<p class="env-empty">{emptyMessage}</p>
	{/if}

	<button class="env-add" type="button" onclick={addRow}>
		<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
			<line x1="12" y1="5" x2="12" y2="19"/>
			<line x1="5" y1="12" x2="19" y2="12"/>
		</svg>
		{addLabel}
	</button>
</div>

<style>
	.env-editor {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.env-header {
		display: grid;
		grid-template-columns: minmax(0, 0.95fr) 1.25rem minmax(0, 1.05fr) 1.75rem;
		gap: 0.4rem;
		padding: 0 0.1rem;
	}

	.env-col-label {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--text-muted);
	}

	.env-rows {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.env-row {
		display: grid;
		grid-template-columns: minmax(0, 0.95fr) 1.25rem minmax(0, 1.05fr) 1.75rem;
		gap: 0.4rem;
		align-items: center;
	}

	.env-separator,
	.env-separator-spacer,
	.env-remove-spacer {
		display: grid;
		place-items: center;
	}

	.env-separator {
		font-family: var(--font-mono);
		font-size: 0.8rem;
		color: var(--text-muted);
	}

	.env-input {
		padding: 0.4rem 0.55rem;
		font-size: 0.72rem;
	}

	.env-input--key {
		font-family: var(--font-mono);
	}

	.env-remove {
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		cursor: pointer;
		transition: color 0.1s, border-color 0.1s, background 0.1s;
		justify-self: center;
	}

	.env-remove:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}

	.env-empty {
		margin: 0;
		padding: 0.7rem 0.8rem;
		border: 1px dashed var(--border-mid);
		border-radius: var(--radius-md);
		font-size: 0.72rem;
		color: var(--text-muted);
		background: color-mix(in srgb, var(--bg-raised) 70%, transparent);
	}

	.env-add {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 0.4rem;
		width: 100%;
		margin-top: 0.1rem;
		padding: 0.4rem 0.625rem;
		background: transparent;
		border: 1px dashed var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.65rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}

	.env-add:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}

	@media (max-width: 640px) {
		.env-header {
			display: none;
		}

		.env-row {
			grid-template-columns: 1fr;
			gap: 0.35rem;
			padding: 0.7rem;
			border: 1px solid var(--border-dim);
			border-radius: var(--radius-md);
			background: color-mix(in srgb, var(--bg-raised) 78%, transparent);
		}

		.env-separator {
			display: none;
		}

		.env-remove {
			justify-self: end;
		}
	}
</style>
