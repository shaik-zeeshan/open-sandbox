<script lang="ts">
	import { onMount, onDestroy } from "svelte";
	import { EditorView, basicSetup } from "codemirror";
	import { EditorState } from "@codemirror/state";
	import { keymap } from "@codemirror/view";
	import { indentWithTab } from "@codemirror/commands";
	import { javascript } from "@codemirror/lang-javascript";
	import { css } from "@codemirror/lang-css";
	import { yaml } from "@codemirror/lang-yaml";
	import { StreamLanguage } from "@codemirror/language";
	import { dockerFile } from "@codemirror/legacy-modes/mode/dockerfile";
	import type { Extension } from "@codemirror/state";

	type Language = "dockerfile" | "shell" | "yaml" | "javascript" | "css" | "text";

	let {
		value = $bindable(""),
		language = "text",
		placeholder = "",
		minHeight = "10rem",
		disabled = false,
		onchange
	} = $props<{
		value?: string;
		language?: Language;
		placeholder?: string;
		minHeight?: string;
		disabled?: boolean;
		onchange?: (v: string) => void;
	}>();

	let containerEl: HTMLDivElement;
	let view: EditorView | undefined;
	let updatingFromProp = false;

	function getLanguageExtension(lang: Language): Extension[] {
		switch (lang) {
			case "javascript": return [javascript()];
			case "css": return [css()];
			case "yaml": return [yaml()];
			case "dockerfile": return [StreamLanguage.define(dockerFile)];
			default: return [];
		}
	}

	const voidTheme = EditorView.theme({
		"&": {
			background: "var(--bg-raised)",
			color: "var(--text-primary)",
			fontFamily: "var(--font-mono)",
			fontSize: "0.72rem",
			borderRadius: "var(--radius-sm)",
			border: "1px solid rgba(255,255,255,0.08)",
		},
		"&.cm-focused": {
			outline: "none",
			border: "1px solid rgba(255,255,255,0.28)",
			boxShadow: "0 0 0 3px rgba(255,255,255,0.04)",
		},
		".cm-scroller": {
			fontFamily: "var(--font-mono)",
			lineHeight: "1.6",
			overflow: "auto",
		},
		".cm-content": {
			padding: "0.5rem 0",
			caretColor: "rgba(255,255,255,0.9)",
		},
		".cm-line": {
			padding: "0 0.625rem",
		},
		".cm-gutters": {
			background: "var(--bg-surface)",
			borderRight: "1px solid rgba(255,255,255,0.045)",
			color: "rgba(255,255,255,0.18)",
			minWidth: "2.5rem",
		},
		".cm-activeLineGutter": {
			background: "rgba(255,255,255,0.04)",
		},
		".cm-activeLine": {
			background: "rgba(255,255,255,0.025)",
		},
		".cm-selectionBackground, ::selection": {
			background: "rgba(255,255,255,0.10) !important",
		},
		".cm-cursor": {
			borderLeftColor: "rgba(255,255,255,0.85)",
		},
		// Syntax tokens — warm minimal palette
		".tok-keyword":    { color: "#93c5fd" },   // blue
		".tok-string":     { color: "#86efac" },   // green
		".tok-comment":    { color: "rgba(255,255,255,0.28)", fontStyle: "italic" },
		".tok-number":     { color: "#fca5a5" },   // red
		".tok-operator":   { color: "rgba(255,255,255,0.60)" },
		".tok-variableName": { color: "rgba(255,255,255,0.85)" },
		".tok-typeName":   { color: "#fcd34d" },   // amber
		".tok-definition": { color: "#c4b5fd" },   // purple
		".tok-propertyName": { color: "rgba(255,255,255,0.70)" },
		".tok-punctuation": { color: "rgba(255,255,255,0.45)" },
		".tok-meta":       { color: "#fbbf24" },
		".tok-atom":       { color: "#a78bfa" },

		// Placeholder
		".cm-placeholder": {
			color: "rgba(255,255,255,0.22)",
		},
	}, { dark: true });

	onMount(() => {
		const extensions: Extension[] = [
			basicSetup,
			keymap.of([indentWithTab]),
			voidTheme,
			...getLanguageExtension(language),
			EditorView.updateListener.of((update) => {
				if (update.docChanged && !updatingFromProp) {
					const newVal = update.state.doc.toString();
					value = newVal;
					onchange?.(newVal);
				}
			}),
			EditorView.lineWrapping,
		];

		if (placeholder) {
			extensions.push(EditorView.inputHandler.of(() => false));
		}

		const startState = EditorState.create({
			doc: value,
			extensions,
		});

		view = new EditorView({
			state: startState,
			parent: containerEl,
		});

		if (disabled && view) {
			view.dom.setAttribute("aria-disabled", "true");
			view.contentDOM.setAttribute("contenteditable", "false");
		}
	});

	onDestroy(() => {
		view?.destroy();
	});

	// Sync external value changes into the editor
	$effect(() => {
		if (!view) return;
		const current = view.state.doc.toString();
		if (current !== value) {
			updatingFromProp = true;
			view.dispatch({
				changes: { from: 0, to: current.length, insert: value }
			});
			updatingFromProp = false;
		}
	});

	// Handle disabled changes
	$effect(() => {
		if (!view) return;
		view.contentDOM.setAttribute("contenteditable", disabled ? "false" : "true");
	});
</script>

<div
	class="code-editor"
	style="--min-height: {minHeight}"
	bind:this={containerEl}
></div>

<style>
	.code-editor {
		width: 100%;
		min-height: var(--min-height);
	}

	/* Override CodeMirror wrapper to respect min-height */
	:global(.code-editor .cm-editor) {
		min-height: var(--min-height);
		border-radius: var(--radius-sm);
		transition: border-color 0.15s;
	}

	:global(.code-editor .cm-scroller) {
		min-height: var(--min-height);
	}
</style>
