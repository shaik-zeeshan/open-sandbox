<script lang="ts">
	// Accepts/emits the same "host:container\nhost:container" string format
	// that the API expects, but renders as a visual mapping table.
	let {
		value = $bindable("")
	} = $props<{
		value?: string;
	}>();

	type PortRow = {
		id: number;
		host: string;
		container: string;
	};

	let nextId = 0;

	// Parse incoming string into rows
	function parseValue(v: string): PortRow[] {
		const lines = v.split("\n").map(l => l.trim()).filter(Boolean);
		if (lines.length === 0) return [];
		return lines.map(line => {
			const parts = line.split(":").map(s => s.trim());
			const [host, container] = parts.length === 1 ? ["", parts[0] ?? ""] : [parts[0] ?? "", parts[1] ?? ""];
			return { id: nextId++, host, container };
		});
	}

	function serializeRows(items: PortRow[]): string {
		return items
			.filter(r => r.host.trim() || r.container.trim())
			.map(r => {
				const host = r.host.trim();
				const container = r.container.trim();
				return host === "" && container !== "" ? container : `${host}:${container}`;
			})
			.join("\n");
	}

	let rows = $state<PortRow[]>(parseValue(value));

	$effect(() => {
		if (value === serializeRows(rows)) {
			return;
		}
		rows = parseValue(value);
	});

	// Sync rows → value string
	function commit(): void {
		value = serializeRows(rows);
	}

	function addRow(): void {
		rows = [...rows, { id: nextId++, host: "", container: "" }];
	}

	function removeRow(id: number): void {
		rows = rows.filter(r => r.id !== id);
		commit();
	}

	function updateHost(id: number, v: string): void {
		rows = rows.map(r => r.id === id ? { ...r, host: v } : r);
		commit();
	}

	function updateContainer(id: number, v: string): void {
		rows = rows.map(r => r.id === id ? { ...r, container: v } : r);
		commit();
	}

	// Handle paste of "host:container\n..." bulk input
	function handlePaste(e: ClipboardEvent, id: number): void {
		const text = e.clipboardData?.getData("text") ?? "";
		if (!text.includes("\n") && !text.includes(" ")) return; // single value, let default paste handle
		e.preventDefault();
		const pasted = parseValue(text);
		if (pasted.length === 0) return;
		// Replace current row with first pasted, append rest
		const idx = rows.findIndex(r => r.id === id);
		const before = rows.slice(0, idx);
		const after = rows.slice(idx + 1);
		rows = [...before, ...pasted, ...after];
		commit();
	}

	// Allow Tab to jump host → container within same row
	function handleHostKeydown(e: KeyboardEvent, id: number): void {
		if (e.key === "Enter") {
			e.preventDefault();
			addRow();
		}
	}

	function handleContainerKeydown(e: KeyboardEvent, id: number): void {
		if (e.key === "Enter") {
			e.preventDefault();
			addRow();
		}
		if (e.key === "Backspace") {
			const row = rows.find(r => r.id === id);
			if (row && row.container === "" && row.host === "" && rows.length > 1) {
				e.preventDefault();
				removeRow(id);
			}
		}
	}
</script>

<div class="ports-editor">
	{#if rows.length > 0}
		<div class="ports-header">
			<span class="ports-col-label">Host port</span>
			<span class="ports-arrow-spacer"></span>
			<span class="ports-col-label">Container port</span>
			<span class="ports-remove-spacer"></span>
		</div>
		<div class="ports-rows">
			{#each rows as row (row.id)}
				<div class="port-row">
					<input
						class="field port-input"
						type="text"
						inputmode="numeric"
						value={row.host}
						placeholder="8080"
						oninput={(e) => updateHost(row.id, (e.currentTarget as HTMLInputElement).value)}
						onkeydown={(e) => handleHostKeydown(e, row.id)}
						onpaste={(e) => handlePaste(e, row.id)}
					/>
					<span class="port-arrow">
						<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
							<line x1="5" y1="12" x2="19" y2="12"/>
							<polyline points="13 6 19 12 13 18"/>
						</svg>
					</span>
					<input
						class="field port-input"
						type="text"
						inputmode="numeric"
						value={row.container}
						placeholder="8080"
						oninput={(e) => updateContainer(row.id, (e.currentTarget as HTMLInputElement).value)}
						onkeydown={(e) => handleContainerKeydown(e, row.id)}
					/>
					<button
						class="port-remove"
						type="button"
						onclick={() => removeRow(row.id)}
						aria-label="Remove port mapping"
						tabindex="-1"
					>
						<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
							<line x1="18" y1="6" x2="6" y2="18"/>
							<line x1="6" y1="6" x2="18" y2="18"/>
						</svg>
					</button>
				</div>
			{/each}
		</div>
	{/if}

	<button class="add-port-btn" type="button" onclick={addRow}>
		<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
			<line x1="12" y1="5" x2="12" y2="19"/>
			<line x1="5" y1="12" x2="19" y2="12"/>
		</svg>
		Add port mapping
	</button>
</div>

<style>
	.ports-editor {
		display: flex;
		flex-direction: column;
		gap: 0.35rem;
	}

	.ports-header {
		display: grid;
		grid-template-columns: 1fr 1.5rem 1fr 1.75rem;
		gap: 0.4rem;
		padding: 0 0.1rem;
	}

	.ports-col-label {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--text-muted);
	}

	.ports-arrow-spacer { display: block; }
	.ports-remove-spacer { display: block; }

	.ports-rows {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.port-row {
		display: grid;
		grid-template-columns: 1fr 1.5rem 1fr 1.75rem;
		gap: 0.4rem;
		align-items: center;
	}

	.port-input {
		/* tighter than default .field */
		padding: 0.35rem 0.5rem;
		font-size: 0.72rem;
		text-align: center;
		font-variant-numeric: tabular-nums;
	}

	.port-arrow {
		display: grid;
		place-items: center;
		color: var(--text-muted);
	}

	.port-remove {
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

	.port-remove:hover {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}

	.add-port-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		background: transparent;
		border: 1px dashed var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.65rem;
		padding: 0.4rem 0.625rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
		width: 100%;
		justify-content: center;
		margin-top: 0.1rem;
	}

	.add-port-btn:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		border-style: solid;
		background: var(--accent-dim);
	}
</style>
