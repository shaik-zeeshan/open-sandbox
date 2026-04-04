<script lang="ts">
	import { tick } from "svelte";

	type Option = {
		value: string;
		label: string;
		description?: string;
		badge?: string;
	};

	let {
		value = $bindable(""),
		options = [],
		placeholder = "Search...",
		loading = false,
		disabled = false,
		emptyText = "No results",
		onSearch,
		onSelect
	} = $props<{
		value?: string;
		options?: Option[];
		placeholder?: string;
		loading?: boolean;
		disabled?: boolean;
		emptyText?: string;
		onSearch?: (query: string) => void;
		onSelect?: (option: Option) => void;
	}>();

	let open = $state(false);
	let query = $state("");
	let activeIndex = $state(-1);
	let inputEl = $state<HTMLInputElement | null>(null);
	let listEl = $state<HTMLUListElement | null>(null);
	let containerEl = $state<HTMLDivElement | null>(null);

	// Selected option display label
	const selectedLabel = $derived(
		options.find((o: Option) => o.value === value)?.label ?? value
	);

	// Filter options by query (client-side if no onSearch; just show all if onSearch is provided)
	const filtered = $derived(
		onSearch
			? options
			: options.filter((o: Option) =>
				o.label.toLowerCase().includes(query.toLowerCase()) ||
				(o.description ?? "").toLowerCase().includes(query.toLowerCase())
			)
	);

	let debounceTimer: ReturnType<typeof setTimeout> | null = null;

	function handleInput(e: Event): void {
		const q = (e.currentTarget as HTMLInputElement).value;
		query = q;
		activeIndex = -1;
		open = true;

		if (onSearch) {
			if (debounceTimer) clearTimeout(debounceTimer);
			debounceTimer = setTimeout(() => {
				if (query.trim()) onSearch(query);
			}, 300);
		}
	}

	function handleFocus(): void {
		open = true;
		query = "";
	}

	function handleBlur(e: FocusEvent): void {
		// Delay to allow click on options to register
		setTimeout(() => {
			if (!containerEl?.contains(document.activeElement)) {
				open = false;
				query = "";
			}
		}, 150);
	}

	async function selectOption(option: Option): Promise<void> {
		value = option.value;
		query = "";
		open = false;
		activeIndex = -1;
		onSelect?.(option);
		await tick();
	}

	function handleKeydown(e: KeyboardEvent): void {
		if (!open) {
			if (e.key === "ArrowDown" || e.key === "Enter") {
				open = true;
				activeIndex = 0;
				e.preventDefault();
			}
			return;
		}

		switch (e.key) {
			case "ArrowDown":
				e.preventDefault();
				activeIndex = Math.min(activeIndex + 1, filtered.length - 1);
				scrollActiveIntoView();
				break;
			case "ArrowUp":
				e.preventDefault();
				activeIndex = Math.max(activeIndex - 1, 0);
				scrollActiveIntoView();
				break;
			case "Enter":
				e.preventDefault();
				if (activeIndex >= 0 && filtered[activeIndex]) {
					void selectOption(filtered[activeIndex]);
				}
				break;
			case "Escape":
				open = false;
				query = "";
				activeIndex = -1;
				inputEl?.blur();
				break;
		}
	}

	function scrollActiveIntoView(): void {
		if (!listEl) return;
		const item = listEl.children[activeIndex] as HTMLElement | undefined;
		item?.scrollIntoView({ block: "nearest" });
	}

	function clearValue(): void {
		value = "";
		query = "";
		open = false;
		inputEl?.focus();
	}
</script>

<div class="combobox" bind:this={containerEl} role="none">
	<div class="combobox-input-wrap">
		<input
			bind:this={inputEl}
			class="field combobox-input"
			type="text"
			{placeholder}
			{disabled}
			value={open ? query : (value ? selectedLabel : "")}
			oninput={handleInput}
			onfocus={handleFocus}
			onblur={handleBlur}
			onkeydown={handleKeydown}
			role="combobox"
			aria-expanded={open}
			aria-controls="combobox-listbox"
			aria-autocomplete="list"
			aria-haspopup="listbox"
			autocomplete="off"
			spellcheck={false}
		/>

		<div class="combobox-trailing">
			{#if loading}
				<svg class="combobox-spinner" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<path d="M21 12a9 9 0 1 1-6.22-8.56"/>
				</svg>
			{:else if value}
				<button class="combobox-clear" type="button" onmousedown={(e) => { e.preventDefault(); clearValue(); }} tabindex="-1" aria-label="Clear">
					<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
						<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
					</svg>
				</button>
			{:else}
				<svg class="combobox-chevron" class:combobox-chevron--open={open} width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<polyline points="6 9 12 15 18 9"/>
				</svg>
			{/if}
		</div>
	</div>

	{#if open}
		<ul
			bind:this={listEl}
			id="combobox-listbox"
			class="combobox-dropdown"
			role="listbox"
		>
			{#if loading}
				<li class="combobox-state">
					<svg class="combobox-spinner" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
						<path d="M21 12a9 9 0 1 1-6.22-8.56"/>
					</svg>
					<span>Searching...</span>
				</li>
			{:else if filtered.length === 0}
				<li class="combobox-state combobox-empty">
					<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
						<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
					</svg>
					{emptyText}
				</li>
			{:else}
				{#each filtered as option, i (option.value)}
					<li
						class="combobox-option"
						class:combobox-option--active={activeIndex === i}
						class:combobox-option--selected={value === option.value}
						role="option"
						aria-selected={value === option.value}
						onmousedown={(e) => { e.preventDefault(); void selectOption(option); }}
						onmouseenter={() => { activeIndex = i; }}
					>
						<!-- Text column: label+badge row, then description below -->
						<div class="combobox-option-body">
							<div class="combobox-option-top">
								<span class="combobox-option-label">{option.label}</span>
								{#if option.badge}
									<span class="combobox-option-badge">{option.badge}</span>
								{/if}
							</div>
							{#if option.description}
								<span class="combobox-option-desc">{option.description}</span>
							{/if}
						</div>
						<!-- Check icon aligned to the right of the row -->
						{#if value === option.value}
							<svg class="combobox-check" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
								<polyline points="20 6 9 17 4 12"/>
							</svg>
						{/if}
					</li>
				{/each}
			{/if}
		</ul>
	{/if}
</div>

<style>
	.combobox {
		position: relative;
		width: 100%;
	}

	.combobox-input-wrap {
		position: relative;
		display: flex;
		align-items: center;
	}

	.combobox-input {
		padding-right: 2rem;
		cursor: default;
	}

	.combobox-input:focus {
		cursor: text;
	}

	.combobox-trailing {
		position: absolute;
		right: 0.5rem;
		display: grid;
		place-items: center;
		pointer-events: none;
	}

	.combobox-clear {
		pointer-events: all;
		background: transparent;
		border: none;
		color: var(--text-muted);
		cursor: pointer;
		display: grid;
		place-items: center;
		width: 16px;
		height: 16px;
		border-radius: 2px;
		transition: color 0.12s, background 0.12s;
	}

	.combobox-clear:hover {
		color: var(--text-primary);
		background: var(--accent-dim);
	}

	.combobox-chevron {
		color: var(--text-muted);
		transition: transform 0.15s var(--ease-snappy);
	}

	.combobox-chevron--open {
		transform: rotate(180deg);
	}

	.combobox-spinner {
		color: var(--text-muted);
		animation: spin 0.8s linear infinite;
	}

	@keyframes spin { to { transform: rotate(360deg); } }

	/* Dropdown */
	.combobox-dropdown {
		position: absolute;
		top: calc(100% + 4px);
		left: 0;
		right: 0;
		z-index: 100;
		background: var(--bg-overlay);
		border: 1px solid var(--border-hi);
		border-radius: var(--radius-md);
		list-style: none;
		margin: 0;
		padding: 0.25rem;
		max-height: 14rem;
		overflow-y: auto;
		box-shadow: 0 8px 24px rgba(0,0,0,0.5), 0 0 0 1px var(--border-dim);
		animation: dropIn 0.12s var(--ease-snappy) both;
	}

	@keyframes dropIn {
		from { opacity: 0; transform: translateY(-4px); }
		to   { opacity: 1; transform: translateY(0); }
	}

	.combobox-state {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.6rem 0.625rem;
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-muted);
	}

	.combobox-empty {
		color: var(--text-muted);
	}

	.combobox-option {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		padding: 0.5rem 0.625rem;
		border-radius: var(--radius-sm);
		cursor: pointer;
		transition: background 0.08s;
	}

	.combobox-option--active {
		background: var(--accent-dim);
	}

	.combobox-option--selected {
		background: var(--bg-raised);
	}

	/* Option body: column — top row (label + badge), then description */
	.combobox-option-body {
		display: flex;
		flex-direction: column;
		gap: 0.18rem;
		min-width: 0;
		flex: 1;
	}

	.combobox-option-top {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		min-width: 0;
	}

	.combobox-option-label {
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		flex: 1;
	}

	.combobox-option-badge {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		padding: 0.08rem 0.35rem;
		border-radius: 999px;
		background: var(--accent-dim);
		border: 1px solid var(--border-mid);
		color: var(--text-secondary);
		flex-shrink: 0;
		white-space: nowrap;
	}

	.combobox-option-desc {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.combobox-check {
		color: var(--status-ok);
		flex-shrink: 0;
		margin-top: 0.1rem;
	}
</style>
