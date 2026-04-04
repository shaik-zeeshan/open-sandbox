<script lang="ts">
	import { browser } from "$app/environment";
	import { onDestroy, onMount, tick } from "svelte";
	import { FitAddon } from "@xterm/addon-fit";
	import "@xterm/xterm/css/xterm.css";
	import { resolveWebSocketUrl, type ApiConfig } from "$lib/api";

	type XtermTerminal = import("@xterm/xterm").Terminal;

	let {
		targetId,
		targetType,
		workspaceDir,
		config
	} = $props<{
		targetId: string;
		targetType: "sandbox" | "container";
		workspaceDir: string;
		config: ApiConfig;
	}>();

	type TerminalStatus = "connecting" | "connected" | "disconnected" | "error";

	let viewport = $state<HTMLDivElement | null>(null);
	let status = $state<TerminalStatus>("connecting");
	let statusText = $state("Connecting shell...");
	let terminalReady = $state(false);

	let terminal: XtermTerminal | null = null;
	let socket: WebSocket | null = null;
	let resizeObserver: ResizeObserver | null = null;

	const sendMessage = (payload: Record<string, unknown>): void => {
		if (socket?.readyState !== WebSocket.OPEN) {
			return;
		}

		socket.send(JSON.stringify(payload));
	};

	const sendResize = (): void => {
		if (!terminal) {
			return;
		}

		sendMessage({ type: "resize", cols: terminal.cols, rows: terminal.rows });
	};

	const disconnect = (): void => {
		if (socket) {
			socket.onopen = null;
			socket.onmessage = null;
			socket.onerror = null;
			socket.onclose = null;
			socket.close();
			socket = null;
		}
	};

	const teardown = (): void => {
		disconnect();
		resizeObserver?.disconnect();
		resizeObserver = null;
		terminal?.dispose();
		terminal = null;
		terminalReady = false;
	};

	const ensureTerminal = async (): Promise<void> => {
		if (!browser || terminal || viewport === null) {
			return;
		}

		await tick();
		if (viewport === null) {
			return;
		}

		const xtermModule = await import("@xterm/xterm");
		const TerminalCtor =
			("Terminal" in xtermModule ? xtermModule.Terminal : undefined) ??
			xtermModule.default?.Terminal;
		if (!TerminalCtor) {
			throw new Error("Unable to load xterm terminal module.");
		}

		const nextTerminal = new TerminalCtor({
			fontFamily: "var(--font-mono)",
			fontSize: 13,
			lineHeight: 1.35,
			cursorBlink: true,
			convertEol: true,
			scrollback: 2000,
			theme: {
				background: "#040404",
				foreground: "#f5f5f5",
				cursor: "#f5f5f5",
				selectionBackground: "rgba(255, 255, 255, 0.16)",
				black: "#040404",
				brightBlack: "#6b7280",
				red: "#f87171",
				brightRed: "#fca5a5",
				green: "#4ade80",
				brightGreen: "#86efac",
				yellow: "#fbbf24",
				brightYellow: "#fde68a",
				blue: "#60a5fa",
				brightBlue: "#93c5fd",
				magenta: "#c084fc",
				brightMagenta: "#d8b4fe",
				cyan: "#22d3ee",
				brightCyan: "#67e8f9",
				white: "#e5e7eb",
				brightWhite: "#ffffff"
			}
		});
		const nextFitAddon = new FitAddon();

		nextTerminal.loadAddon(nextFitAddon);
		nextTerminal.open(viewport);
		nextFitAddon.fit();
		nextTerminal.focus();
		nextTerminal.onData((data) => sendMessage({ type: "input", data }));
		nextTerminal.onResize(() => sendResize());

		resizeObserver = new ResizeObserver(() => {
			nextFitAddon.fit();
		});
		resizeObserver.observe(viewport);

		terminal = nextTerminal;
		terminalReady = true;
	};

	const connect = async (): Promise<void> => {
		if (!browser) {
			return;
		}

		await ensureTerminal();
		if (!terminal) {
			return;
		}

		disconnect();
		status = "connecting";
		statusText = "Connecting shell...";

		const ws = new WebSocket(
			resolveWebSocketUrl(config, `/api/${targetType === "sandbox" ? "sandboxes" : "containers"}/${encodeURIComponent(targetId)}/terminal/ws`, {
				cols: terminal.cols,
				rows: terminal.rows
			})
		);
		ws.binaryType = "arraybuffer";
		socket = ws;

		ws.onopen = () => {
			if (socket !== ws) {
				return;
			}

			status = "connected";
			statusText = `Live shell in ${workspaceDir}`;
			terminal?.focus();
			sendResize();
		};

		ws.onmessage = (event) => {
			if (socket !== ws || !terminal) {
				return;
			}

			if (event.data instanceof ArrayBuffer) {
				terminal.write(new Uint8Array(event.data));
				return;
			}

			if (event.data instanceof Blob) {
				void event.data.arrayBuffer().then((buffer) => {
					if (socket === ws && terminal) {
						terminal.write(new Uint8Array(buffer));
					}
				});
				return;
			}

			terminal.write(String(event.data));
		};

		ws.onerror = () => {
			if (socket !== ws) {
				return;
			}

			status = "error";
			statusText = "Terminal connection failed.";
		};

		ws.onclose = (event) => {
			if (socket === ws) {
				socket = null;
			}

			status = event.wasClean ? "disconnected" : "error";
			statusText = event.reason.trim() || "Terminal session ended.";
		};
	};

	onMount(() => {
		void connect();
	});

	onDestroy(() => {
		teardown();
	});
</script>

<div class="terminal-view anim-fade-up">
	<div class="terminal-toolbar">
		<div class="terminal-status terminal-status--{status}">
			<span class="terminal-status-dot"></span>
			<span>{statusText}</span>
		</div>
		<div class="terminal-actions">
			<button class="btn-ghost btn-sm" type="button" onclick={() => terminal?.clear()} disabled={!terminalReady}>
				Clear
			</button>
			<button class="btn-primary btn-sm" type="button" onclick={() => void connect()}>
				{status === "connecting" ? "Connecting..." : "Reconnect"}
			</button>
		</div>
	</div>

	<div class="terminal-shell panel">
		<div bind:this={viewport} class="terminal-canvas"></div>
	</div>
</div>

<style>
	.terminal-view {
		display: flex;
		flex-direction: column;
		gap: 0.85rem;
		min-height: 0;
	}

	.terminal-toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		padding: 0.8rem 0.95rem;
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		flex-wrap: wrap;
	}

	.terminal-status {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		font-family: var(--font-mono);
		font-size: 0.66rem;
		color: var(--text-secondary);
	}

	.terminal-status-dot {
		width: 0.45rem;
		height: 0.45rem;
		border-radius: 999px;
		background: var(--status-idle);
		box-shadow: 0 0 0.6rem transparent;
	}

	.terminal-status--connected .terminal-status-dot {
		background: var(--status-ok);
		box-shadow: 0 0 0.8rem rgba(74, 222, 128, 0.45);
	}

	.terminal-status--connecting .terminal-status-dot {
		background: var(--status-warn);
		box-shadow: 0 0 0.8rem rgba(251, 191, 36, 0.35);
	}

	.terminal-status--error .terminal-status-dot {
		background: var(--status-error);
		box-shadow: 0 0 0.8rem rgba(248, 113, 113, 0.4);
	}

	.terminal-actions {
		display: flex;
		gap: 0.5rem;
	}

	.terminal-shell {
		overflow: hidden;
		padding: 0;
		background: #040404;
		border-radius: var(--radius-md);
		min-height: 26rem;
	}

	.terminal-canvas {
		height: 26rem;
		padding: 0.9rem;
	}

	:global(.terminal-canvas .xterm) {
		height: 100%;
	}

	:global(.terminal-canvas .xterm-viewport) {
		overflow-y: auto;
	}

	:global(.terminal-canvas .xterm-screen canvas) {
		border-radius: 0.35rem;
	}

	@media (max-width: 640px) {
		.terminal-shell,
		.terminal-canvas {
			min-height: 22rem;
			height: 22rem;
		}

		.terminal-actions {
			width: 100%;
		}

		.terminal-actions :global(button) {
			flex: 1;
		}
	}
</style>
