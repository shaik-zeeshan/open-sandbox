<script lang="ts">
	import { browser } from "$app/environment";
	import { onDestroy, onMount, tick } from "svelte";
	import { FitAddon } from "@xterm/addon-fit";
	import "@xterm/xterm/css/xterm.css";
	import { resolveWebSocketUrl, type ApiConfig } from "$lib/api";
	import { Context, Effect, Fiber, Layer, type Scope } from "effect";

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
	type XtermDisposable = { dispose: () => void };

	let viewport = $state<HTMLDivElement | null>(null);
	let status = $state<TerminalStatus>("connecting");
	let statusText = $state("Connecting shell...");
	let terminalReady = $state(false);

	let terminal: XtermTerminal | null = null;
	let terminalInputDisposable: XtermDisposable | null = null;
	let terminalResizeDisposable: XtermDisposable | null = null;
	let activeSession: TerminalSession | null = null;
	let connectionFiber: Fiber.RuntimeFiber<void, unknown> | null = null;
	let resizeObserver: ResizeObserver | null = null;

	interface TerminalSession {
		send: (payload: Record<string, unknown>) => Effect.Effect<void>;
	}

	interface TerminalTransportService {
		connect: (request: {
			url: string;
			onOpen: () => void;
			onMessage: (event: MessageEvent) => void;
			onError: () => void;
			onClose: (event: CloseEvent) => void;
		}) => Effect.Effect<TerminalSession, Error, Scope.Scope>;
	}

	const TerminalTransportService = Context.GenericTag<TerminalTransportService>(
		"sandbox-terminal/TerminalTransportService"
	);

	const terminalTransportService: TerminalTransportService = {
		connect: (request) =>
			Effect.acquireRelease(
				Effect.async<WebSocket, Error>((resume) => {
					const ws = new WebSocket(request.url);
					ws.binaryType = "arraybuffer";

					const handleOpen = () => {
						cleanup();
						request.onOpen();
						resume(Effect.succeed(ws));
					};

					const handleError = () => {
						cleanup();
						request.onError();
						resume(Effect.fail(new Error("Terminal connection failed.")));
					};

					const cleanup = (): void => {
						ws.onopen = null;
						ws.onerror = null;
					};

					ws.onopen = handleOpen;
					ws.onerror = handleError;

					return Effect.sync(() => {
						cleanup();
						ws.close();
					});
				}),
				(ws) =>
					Effect.sync(() => {
						ws.onmessage = null;
						ws.onclose = null;
						ws.onerror = null;
						if (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN) {
							ws.close();
						}
					})
			).pipe(
				Effect.tap((ws) =>
					Effect.sync(() => {
						ws.onmessage = request.onMessage;
						ws.onerror = request.onError;
						ws.onclose = request.onClose;
					})
				),
				Effect.map((ws) => ({
					send: (payload) =>
						Effect.sync(() => {
							if (ws.readyState === WebSocket.OPEN) {
								ws.send(JSON.stringify(payload));
							}
						})
				}))
			)
	};

	const terminalLayer = Layer.succeed(TerminalTransportService, terminalTransportService);

	const runTerminalProgram = <A>(program: Effect.Effect<A, unknown>): Promise<A> => Effect.runPromise(program);

	const sendMessage = (payload: Record<string, unknown>): void => {
		if (!activeSession) {
			return;
		}

		Effect.runFork(activeSession.send(payload));
	};

	const sendResize = (): void => {
		if (!terminal) {
			return;
		}

		sendMessage({ type: "resize", cols: terminal.cols, rows: terminal.rows });
	};

	const disconnect = (): void => {
		if (connectionFiber) {
			Effect.runFork(Fiber.interruptFork(connectionFiber));
			connectionFiber = null;
		}
		activeSession = null;
	};

	const teardown = (): void => {
		disconnect();
		resizeObserver?.disconnect();
		resizeObserver = null;
		terminalInputDisposable?.dispose();
		terminalInputDisposable = null;
		terminalResizeDisposable?.dispose();
		terminalResizeDisposable = null;
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
		terminalInputDisposable?.dispose();
		terminalResizeDisposable?.dispose();
		terminalInputDisposable = nextTerminal.onData((data) => sendMessage({ type: "input", data }));
		terminalResizeDisposable = nextTerminal.onResize(() => sendResize());

		resizeObserver = new ResizeObserver(() => {
			nextFitAddon.fit();
		});
		resizeObserver.observe(viewport);

		terminal = nextTerminal;
		terminalReady = true;
	};

	const connectSessionProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const transport = yield* TerminalTransportService;

			if (!terminal) {
				return;
			}

			const session = yield* transport.connect({
				url: resolveWebSocketUrl(
					config,
					`/api/${targetType === "sandbox" ? "sandboxes" : "containers"}/${encodeURIComponent(targetId)}/terminal/ws`,
					{ cols: terminal.cols, rows: terminal.rows }
				),
				onOpen: () => {
					status = "connected";
					statusText = workspaceDir.trim().length > 0
						? `Live shell in ${workspaceDir}`
						: "Live shell using the container default working directory";
					terminal?.focus();
					sendResize();
				},
				onMessage: (event) => {
					if (!terminal) {
						return;
					}

					if (event.data instanceof ArrayBuffer) {
						terminal.write(new Uint8Array(event.data));
						return;
					}

					if (event.data instanceof Blob) {
						void event.data.arrayBuffer().then((buffer) => {
							if (activeSession && terminal) {
								terminal.write(new Uint8Array(buffer));
							}
						});
						return;
					}

					terminal.write(String(event.data));
				},
				onError: () => {
					status = "error";
					statusText = "Terminal connection failed.";
				},
				onClose: (event) => {
					status = event.wasClean ? "disconnected" : "error";
					statusText = event.reason.trim() || "Terminal session ended.";
					activeSession = null;
				}
			});

			yield* Effect.sync(() => {
				activeSession = session;
			});

			yield* Effect.never;
		}).pipe(Effect.provide(terminalLayer), Effect.scoped);

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

		let launchedFiber: Fiber.RuntimeFiber<void, unknown> | null = null;
		const sessionProgram = connectSessionProgram().pipe(
			Effect.catchAll((error) =>
				Effect.sync(() => {
					status = "error";
					statusText = error instanceof Error ? error.message : "Terminal connection failed.";
				})
			),
			Effect.ensuring(
				Effect.sync(() => {
					if (connectionFiber === launchedFiber) {
						connectionFiber = null;
						activeSession = null;
					}
				})
			)
		);

		launchedFiber = Effect.runFork(sessionProgram);
		connectionFiber = launchedFiber;
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
		height: 100%;
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
		display: flex;
		flex-direction: column;
		overflow: hidden;
		padding: 0.9rem;
		background: #040404;
		border-radius: var(--radius-md);
		flex: 1;
		min-height: 20rem;
	}

	.terminal-canvas {
		width: 100%;
		height: 100%;
		padding: 0;
		overflow: hidden;
	}

	:global(.terminal-canvas .xterm) {
		width: 100%;
		height: 100%;
	}

	:global(.terminal-canvas .xterm-viewport) {
		height: 100%;
		overflow-y: auto;
	}

	:global(.terminal-canvas .xterm-screen canvas) {
		border-radius: 0.35rem;
	}

	@media (max-width: 640px) {
		.terminal-shell { min-height: 16rem; }

		.terminal-actions {
			width: 100%;
		}

		.terminal-actions :global(button) {
			flex: 1;
		}
	}
</style>
