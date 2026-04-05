<script lang="ts">
	import { invalidateWorkloadCaches } from "$lib/api-cache";
	import { onDestroy, onMount, tick } from "svelte";
	import {
		clearScheduledTimeout,
		dispatchAuthErrorEvent,
		scheduleTimeout,
		type TimeoutHandle
	} from "$lib/client/browser";
	import { toast } from "$lib/toast.svelte";
	import SandboxTerminal from "$lib/components/SandboxTerminal.svelte";
	import AnsiToHtml from "ansi-to-html";
	import {
		deleteSandbox,
		formatApiFailure,
		readContainerFile,
		readSandboxFile,
		removeContainer,
		restartContainer,
		resetContainer,
		resetSandbox,
		resolveApiUrl,
		restartSandbox,
		runApiEffect,
		saveContainerFile,
		saveSandboxFile,
		stopContainer,
		stopSandbox,
		uploadContainerFile,
		uploadSandboxFile,
		type ApiConfig,
		type ContainerSummary,
		type FileReadResponse,
		type PortSummary,
		type Sandbox
	} from "$lib/api";
	import { Context, Effect, Fiber, Layer, type Scope } from "effect";

	type WorkspaceTab = "overview" | "terminal" | "files" | "logs";

	let {
		sandbox = null,
		container,
		runtimeContainer = null,
		config,
		onBack,
		onRefresh,
		onContainerReplaced,
		onDeleted
	} = $props<{
		sandbox?: Sandbox | null;
		container: ContainerSummary | null;
		runtimeContainer?: ContainerSummary | null;
		config: ApiConfig;
		onBack: () => void;
		onRefresh: () => Promise<void> | void;
		onContainerReplaced: (id: string) => void;
		onDeleted: () => void;
	}>();

	let activeTab = $state<WorkspaceTab>("overview");

	const defaultUploadPath = (workspaceDir: string): string => {
		const normalized = workspaceDir === "/" ? "" : workspaceDir.replace(/\/+$/, "");
		return `${normalized}/upload.txt`;
	};

	// Files
	let browsePath = $state<string>("/");
	let filePayload = $state<FileReadResponse | null>(null);
	let readLoading = $state(false);
	let saveLoading = $state(false);
	let editorContent = $state("");
	let uploadPath = $state<string>(defaultUploadPath("/"));
	let uploadFile = $state<File | null>(null);
	let uploadLoading = $state(false);

	const ansiConverter = new AnsiToHtml({
		fg: "rgba(255,255,255,0.85)",
		bg: "#040404",
		escapeXML: true,
		newline: false,
		stream: false
	});

	function ansiToHtml(text: string): string {
		try { return ansiConverter.toHtml(text); }
		catch { return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
	}

	// Logs
	let logTail = $state("100");
	let logFollow = $state(true);
	let logEntries = $state<Array<{ stream: string; line: string; html: string }>>([]);
	let logsViewport = $state<HTMLDivElement | null>(null);
	let logsFiber: Fiber.RuntimeFiber<void, unknown> | null = null;
	let streaming = $state(false);

	// Actions
	let actionLoading = $state<string | null>(null);
	let deleteConfirm = $state(false);
	let deleteConfirmTimer: TimeoutHandle | null = null;

	function requestDelete(): void {
		if (deleteConfirm) {
			if (deleteConfirmTimer) {
				clearScheduledTimeout(deleteConfirmTimer);
				deleteConfirmTimer = null;
			}
			void handleAction("delete");
		} else {
			deleteConfirm = true;
			clearScheduledTimeout(deleteConfirmTimer);
			deleteConfirmTimer = scheduleTimeout(() => {
				deleteConfirm = false;
				deleteConfirmTimer = null;
			}, 3000);
		}
	}

	type LogEntry = { stream: string; line: string };

	interface WorkspaceFileIoService {
		read: (path: string) => Effect.Effect<FileReadResponse, unknown>;
		save: (path: string, content: string) => Effect.Effect<void, unknown>;
		upload: (path: string, file: File) => Effect.Effect<void, unknown>;
	}

	interface WorkspaceLogStreamService {
		stream: (request: {
			follow: boolean;
			tail: string;
			onEntry: (entry: LogEntry) => void;
		}) => Effect.Effect<void, unknown, Scope.Scope>;
	}

	interface WorkspaceFeedbackService {
		error: (error: unknown) => Effect.Effect<void>;
		ok: (message: string) => Effect.Effect<void>;
	}

	const WorkspaceFileIoService = Context.GenericTag<WorkspaceFileIoService>(
		"sandbox-workspace/WorkspaceFileIoService"
	);
	const WorkspaceLogStreamService = Context.GenericTag<WorkspaceLogStreamService>(
		"sandbox-workspace/WorkspaceLogStreamService"
	);
	const WorkspaceFeedbackService = Context.GenericTag<WorkspaceFeedbackService>(
		"sandbox-workspace/WorkspaceFeedbackService"
	);

	const workspaceFileIoService: WorkspaceFileIoService = {
		read: (path) =>
			Effect.promise(() =>
				workloadKind === "sandbox"
					? runApiEffect(readSandboxFile(config, targetId, path))
					: runApiEffect(readContainerFile(config, backingContainerId, path))
			),
		save: (path, content) =>
			Effect.promise(() =>
				workloadKind === "sandbox"
					? runApiEffect(saveSandboxFile(config, targetId, path, content))
					: runApiEffect(saveContainerFile(config, backingContainerId, path, content))
			).pipe(Effect.asVoid),
		upload: (path, file) =>
			Effect.promise(() =>
				workloadKind === "sandbox"
					? runApiEffect(uploadSandboxFile(config, targetId, path, file))
					: runApiEffect(uploadContainerFile(config, backingContainerId, path, file))
			).pipe(Effect.asVoid)
	};

	const workspaceLogStreamService: WorkspaceLogStreamService = {
		stream: ({ follow, tail, onEntry }) =>
			Effect.gen(function* () {
				const controller = yield* Effect.acquireRelease(
					Effect.sync(() => new AbortController()),
					(value) => Effect.sync(() => value.abort())
				);

				const headers = new Headers();
				const token = config.token?.trim() ?? "";
				if (token.length > 0) {
					headers.set("Authorization", `Bearer ${token}`);
				}

				const response = yield* Effect.tryPromise({
					try: () =>
						fetch(
							resolveApiUrl(
								config,
								`/api/${workloadKind === "sandbox" ? "sandboxes" : "containers"}/${encodeURIComponent(targetId)}/logs`,
								{ follow, tail: tail.trim() || "100" }
							),
							{ credentials: "include", headers, signal: controller.signal }
						),
					catch: (error) => (error instanceof Error ? error : new Error("Failed to stream logs."))
				});

				if (response.status === 401) {
					yield* Effect.sync(() => {
						dispatchAuthErrorEvent();
					});
				}

				if (!response.ok || response.body === null) {
					return yield* Effect.fail(new Error(`Unable to stream logs: HTTP ${response.status}`));
				}

				const reader = response.body.getReader();
				const decoder = new TextDecoder();
				let buffer = "";

				const parseSseBlock = (block: string): LogEntry | null => {
					const lines = block
						.split("\n")
						.map((line) => line.trimEnd())
						.filter((line) => line.length > 0);
					if (lines.length === 0) {
						return null;
					}

					let eventName = "info";
					const data: string[] = [];
					for (const line of lines) {
						if (line.startsWith("event:")) {
							eventName = line.slice(6).trim() || "info";
							continue;
						}
						if (line.startsWith("data:")) {
							data.push(line.slice(5).trimStart());
						}
					}

					const message = data.join("\n");
					return message.length > 0 ? { stream: eventName, line: message } : null;
				};

				const consumeSseBuffer = (pending: string): string => {
					const normalized = pending.replace(/\r\n/g, "\n");
					const chunks = normalized.split("\n\n");
					const remainder = chunks.pop() ?? "";
					for (const chunk of chunks) {
						const parsed = parseSseBlock(chunk);
						if (parsed !== null) {
							onEntry(parsed);
						}
					}
					return remainder;
				};

				while (true) {
					const chunk = yield* Effect.tryPromise({
						try: () => reader.read(),
						catch: (error) => (error instanceof Error ? error : new Error("Failed to read logs stream."))
					});

					if (chunk.done) {
						break;
					}

					buffer += decoder.decode(chunk.value, { stream: true });
					buffer = consumeSseBuffer(buffer);
				}

				if (buffer.trim().length > 0) {
					const parsed = parseSseBlock(buffer);
					if (parsed !== null) {
						onEntry(parsed);
					}
				}
			})
	};

	const workspaceFeedbackService: WorkspaceFeedbackService = {
		error: (error) =>
			Effect.sync(() => {
				toast.error(formatApiFailure(error));
			}),
		ok: (message) =>
			Effect.sync(() => {
				toast.ok(message);
			})
	};

	const workspaceLayer = Layer.mergeAll(
		Layer.succeed(WorkspaceFileIoService, workspaceFileIoService),
		Layer.succeed(WorkspaceLogStreamService, workspaceLogStreamService),
		Layer.succeed(WorkspaceFeedbackService, workspaceFeedbackService)
	);

	const runWorkspaceProgram = <A>(program: Effect.Effect<A, unknown>): Promise<A> => Effect.runPromise(program);

	const directoryEntries = $derived(filePayload?.kind === "directory" ? filePayload.entries ?? [] : []);

	const previewLinks = (ports?: PortSummary[]): string[] =>
		(ports ?? [])
			.filter((p) => typeof p.public === "number" && p.public > 0 && p.type === "tcp")
			.map((p) => `http://localhost:${p.public}`);

	const statusInfo = (status: string): { label: string; cls: "ok" | "error" | "idle" } => {
		const n = status.toLowerCase();
		if (n.includes("up") || n.includes("running")) return { label: "running", cls: "ok" };
		if (n.includes("exit") || n.includes("dead") || n.includes("error")) return { label: "stopped", cls: "error" };
		return { label: "idle", cls: "idle" };
	};

	const workloadKind = $derived(runtimeContainer ? "container" : "sandbox");
	const workloadLabel = $derived(workloadKind === "sandbox" ? "sandbox" : "container");
	const activeContainer = $derived(runtimeContainer ?? container);
	const targetId = $derived(workloadKind === "sandbox" ? (sandbox?.id ?? "") : (runtimeContainer?.id ?? ""));
	const backingContainerId = $derived(runtimeContainer?.container_id ?? sandbox?.container_id ?? container?.container_id ?? "");
	const sandboxWorkspaceDir = $derived(sandbox?.workspace_dir ?? "");
	const workspaceDirValue = $derived(sandboxWorkspaceDir.trim().length > 0 ? sandboxWorkspaceDir : "/");
	const workspaceMetaValue = $derived(workloadKind === "sandbox" && sandboxWorkspaceDir.trim().length === 0 ? "container default" : workspaceDirValue);
	const terminalWorkspaceDir = $derived(workloadKind === "sandbox" ? sandboxWorkspaceDir : workspaceDirValue);
	const workloadName = $derived(runtimeContainer?.names[0] ?? sandbox?.name ?? runtimeContainer?.id.slice(0, 12) ?? "Container");
	const workloadImage = $derived(runtimeContainer?.image ?? sandbox?.image ?? activeContainer?.image ?? "");
	const workloadStatus = $derived(activeContainer?.status ?? sandbox?.status ?? "");
	const workloadCreatedAt = $derived(sandbox?.created_at ?? 0);
	const st = $derived(statusInfo(workloadStatus));
	const ports = $derived(previewLinks(activeContainer?.ports));
	const canReset = $derived.by(() => {
		if (workloadKind === "sandbox") {
			return true;
		}
		return activeContainer?.resettable ?? false;
	});
	const canBrowseFiles = $derived(backingContainerId.length > 0);

	const formatDate = (unixSeconds: number): string =>
		new Date(unixSeconds * 1000).toLocaleString(undefined, {
			year: "numeric", month: "short", day: "numeric",
			hour: "2-digit", minute: "2-digit"
		});

	const parentPath = (value: string): string => {
		if (value === "/") return "/";
		const t = value.endsWith("/") ? value.slice(0, -1) : value;
		const i = t.lastIndexOf("/");
		return i <= 0 ? "/" : t.slice(0, i);
	};

	const joinPath = (base: string, child: string): string => {
		const nb = base.endsWith("/") ? base.slice(0, -1) : base;
		return `${nb || ""}/${child}`.replace(/\/+/g, "/");
	};

	const uploadBasePath = (): string => {
		return filePayload?.kind === "directory" ? browsePath : parentPath(browsePath);
	};

	$effect(() => {
		logEntries.length;
		if (!logsViewport) return;
		void tick().then(() => {
			if (logsViewport) logsViewport.scrollTop = logsViewport.scrollHeight;
		});
	});

	const loadPathProgram = (pathToLoad: string): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const fileIo = yield* WorkspaceFileIoService;
			const feedback = yield* WorkspaceFeedbackService;

			yield* Effect.sync(() => {
				readLoading = true;
			});

			const normalizedPath = pathToLoad.trim();
			try {
				const payload = yield* fileIo.read(normalizedPath);
				yield* Effect.sync(() => {
					filePayload = payload;
					browsePath = normalizedPath;
					editorContent = payload.kind === "file" ? payload.content ?? "" : "";
				});
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					readLoading = false;
				});
			}
		}).pipe(Effect.provide(workspaceLayer));

	const saveFileProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const fileIo = yield* WorkspaceFileIoService;
			const feedback = yield* WorkspaceFeedbackService;

			if (filePayload?.kind !== "file") {
				return;
			}

			yield* Effect.sync(() => {
				saveLoading = true;
			});

			try {
				yield* fileIo.save(browsePath, editorContent);
				yield* feedback.ok("File saved.");
				yield* Effect.sync(() => {
					if (filePayload?.kind === "file") {
						filePayload = { ...filePayload, content: editorContent };
					}
				});
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					saveLoading = false;
				});
			}
		}).pipe(Effect.provide(workspaceLayer));

	const uploadFileProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const fileIo = yield* WorkspaceFileIoService;
			const feedback = yield* WorkspaceFeedbackService;

			if (uploadFile === null) {
				return;
			}

			yield* Effect.sync(() => {
				uploadLoading = true;
			});

			try {
				yield* fileIo.upload(uploadPath.trim(), uploadFile);
				yield* feedback.ok("File uploaded.");
				yield* loadPathProgram(filePayload?.kind === "directory" ? browsePath : parentPath(uploadPath));
			} catch (error) {
				yield* feedback.error(error);
			} finally {
				yield* Effect.sync(() => {
					uploadLoading = false;
				});
			}
		}).pipe(Effect.provide(workspaceLayer));

	const startLogsProgram = (): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const logs = yield* WorkspaceLogStreamService;
			const feedback = yield* WorkspaceFeedbackService;

			yield* Effect.scoped(
				logs.stream({
					follow: logFollow,
					tail: logTail,
					onEntry: (entry) => {
						const html = ansiToHtml(entry.line);
						logEntries = [...logEntries.slice(-399), { stream: entry.stream, line: entry.line, html }];
					}
				})
			).pipe(
				Effect.catchAll((error) => {
					if (error instanceof DOMException && error.name === "AbortError") {
						return Effect.void;
					}
					return feedback.error(error);
				})
			);
		}).pipe(Effect.provide(workspaceLayer));

	// Load the workspace dir on mount
	onMount(() => {
		const initialWorkspaceDir = workspaceDirValue;
		browsePath = initialWorkspaceDir;
		uploadPath = defaultUploadPath(initialWorkspaceDir);
		void runWorkspaceProgram(loadPathProgram(initialWorkspaceDir));
	});

	onDestroy(() => {
		stopLogs();
		clearScheduledTimeout(deleteConfirmTimer);
		deleteConfirmTimer = null;
	});

	async function loadPath(pathToLoad: string): Promise<void> {
		await runWorkspaceProgram(loadPathProgram(pathToLoad));
	}

	async function submitSaveFile(): Promise<void> {
		await runWorkspaceProgram(saveFileProgram());
	}

	async function submitUploadFile(): Promise<void> {
		await runWorkspaceProgram(uploadFileProgram());
	}

	function stopLogs(): void {
		if (logsFiber) {
			Effect.runFork(Fiber.interruptFork(logsFiber));
			logsFiber = null;
		}
		streaming = false;
	}

	async function startLogs(): Promise<void> {
		stopLogs();
		logEntries = [];
		streaming = true;

		let launchedFiber: Fiber.RuntimeFiber<void, unknown> | null = null;
		const program = startLogsProgram().pipe(
			Effect.ensuring(
				Effect.sync(() => {
					if (logsFiber === launchedFiber) {
						logsFiber = null;
					}
					streaming = false;
				})
			)
		);

		launchedFiber = Effect.runFork(program);
		logsFiber = launchedFiber;
	}

	async function handleAction(action: "restart" | "reset" | "stop" | "delete"): Promise<void> {
		actionLoading = action;
		try {
			if (action === "restart") {
				if (workloadKind === "sandbox") {
					await runApiEffect(restartSandbox(config, targetId));
				} else {
					await runApiEffect(restartContainer(config, targetId));
				}
				Effect.runSync(invalidateWorkloadCaches(config));
				toast.ok("Restarted.");
				await Promise.resolve(onRefresh());
			} else if (action === "reset") {
				if (workloadKind === "sandbox") {
					await runApiEffect(resetSandbox(config, targetId));
					Effect.runSync(invalidateWorkloadCaches(config));
					toast.ok("Reset to clean workspace.");
					await Promise.resolve(onRefresh());
					await loadPath(workspaceDirValue);
				} else {
					if (!canReset) {
						throw new Error("Reset is only available for managed direct containers and compose workloads.");
					}
					const result = await runApiEffect(resetContainer(config, targetId));
					Effect.runSync(invalidateWorkloadCaches(config));
					onContainerReplaced(result.id);
					toast.ok("Container reset.");
					await Promise.resolve(onRefresh());
					await tick();
					await loadPath(browsePath);
				}
			} else if (action === "stop") {
				if (workloadKind === "sandbox") {
					await runApiEffect(stopSandbox(config, targetId));
				} else {
					await runApiEffect(stopContainer(config, targetId));
				}
				Effect.runSync(invalidateWorkloadCaches(config));
				toast.ok("Stopped.");
				await Promise.resolve(onRefresh());
			} else if (action === "delete") {
				if (workloadKind === "sandbox") {
					await runApiEffect(deleteSandbox(config, targetId));
				} else {
					await runApiEffect(removeContainer(config, targetId));
				}
				Effect.runSync(invalidateWorkloadCaches(config));
				onDeleted();
			}
		} catch (error) {
			toast.error(formatApiFailure(error));
		} finally {
			actionLoading = null;
		}
	}

	const tabs: Array<{ id: WorkspaceTab; label: string }> = [
		{ id: "overview", label: "Overview" },
		{ id: "terminal", label: "Terminal" },
		{ id: "files",    label: "Files"    },
		{ id: "logs",     label: "Logs"     }
	];
</script>

<div class="workspace anim-fade-up">
	<!-- ── Workspace header ───────────────────────────────────────────────────── -->
	<header class="ws-header">
		<div class="ws-header-left">
			<button class="back-btn" type="button" onclick={onBack} aria-label="Back to sandbox list">
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
					<polyline points="15 18 9 12 15 6"/>
				</svg>
			</button>
			<div class="ws-identity">
				<div class="ws-status-dot ws-status-dot--{st.cls}"></div>
				<h1 class="ws-title">{workloadName}</h1>
				<span class="ws-status-badge ws-status-badge--{st.cls}">{st.label}</span>
			</div>
		</div>

		<div class="ws-header-right">
			{#if ports.length > 0}
				<div class="port-links">
					{#each ports as port}
						<a class="port-link" href={port} target="_blank" rel="noreferrer">
							<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
								<path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
								<polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
							</svg>
							:{port.split(":").pop()}
						</a>
					{/each}
				</div>
			{/if}
			<div class="ws-actions">
				<button class="action-btn" type="button" onclick={() => void handleAction("restart")} disabled={actionLoading !== null}>
					{actionLoading === "restart" ? "..." : "Restart"}
				</button>
				<button class="action-btn" type="button" onclick={() => void handleAction("stop")} disabled={actionLoading !== null}>
					{actionLoading === "stop" ? "..." : "Stop"}
				</button>
				{#if canReset}
					<button class="action-btn" type="button" onclick={() => void handleAction("reset")} disabled={actionLoading !== null}>
						{actionLoading === "reset" ? "..." : "Reset"}
					</button>
				{/if}
				<button
					class="action-btn action-btn--danger"
					class:action-btn--confirming={deleteConfirm}
					type="button"
					onclick={requestDelete}
					disabled={actionLoading !== null}
				>
					{actionLoading === "delete" ? "..." : deleteConfirm ? "Confirm?" : workloadKind === "sandbox" ? "Delete" : "Remove"}
				</button>
			</div>
		</div>
	</header>

	<!-- ── Tab bar ────────────────────────────────────────────────────────────── -->
	<div class="tab-bar">
		{#each tabs as tab}
			<button
				type="button"
				class="tab-btn {activeTab === tab.id ? 'tab-btn--active' : ''}"
				onclick={() => activeTab = tab.id}
			>
				{tab.label}
				{#if tab.id === "logs" && streaming}
					<span class="tab-live-dot"></span>
				{/if}
			</button>
		{/each}
	</div>

	<!-- ── Tab content ────────────────────────────────────────────────────────── -->
	<div class="tab-content">

		<!-- Overview -->
		{#if activeTab === "overview"}
			<div class="overview anim-fade-up">
				<!-- Meta grid -->
				<div class="meta-grid">
					<div class="meta-card">
						<span class="meta-label">Image</span>
						<span class="meta-value">{workloadImage}</span>
					</div>
					<div class="meta-card">
						<span class="meta-label">Container ID</span>
						<span class="meta-value mono">{backingContainerId.slice(0, 16)}</span>
					</div>
					<div class="meta-card">
						<span class="meta-label">{workloadKind === "sandbox" ? "Workspace" : "Default path"}</span>
						<span class="meta-value mono">{workspaceMetaValue}</span>
					</div>
					{#if workloadCreatedAt > 0}
						<div class="meta-card">
							<span class="meta-label">Created</span>
							<span class="meta-value">{formatDate(workloadCreatedAt)}</span>
						</div>
					{/if}
					{#if ports.length > 0}
						<div class="meta-card meta-card--wide">
							<span class="meta-label">Exposed ports</span>
							<div class="port-chips">
								{#each ports as port}
									<a class="port-chip" href={port} target="_blank" rel="noreferrer">{port}</a>
								{/each}
							</div>
						</div>
					{/if}
				</div>

				<!-- Quick actions -->
				<div class="quick-actions">
					<button class="quick-btn" type="button" onclick={() => activeTab = "terminal"}>
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
							<polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>
						</svg>
						<span class="quick-btn-label">Open Terminal</span>
						<span class="quick-btn-sub">Run commands in this {workloadLabel}</span>
					</button>
					<button class="quick-btn" type="button" onclick={() => activeTab = "files"} disabled={!canBrowseFiles}>
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
							<path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/>
						</svg>
						<span class="quick-btn-label">Browse Files</span>
						<span class="quick-btn-sub">Read, edit and upload files</span>
					</button>
					<button class="quick-btn" type="button" onclick={() => { activeTab = "logs"; void startLogs(); }}>
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
							<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
						</svg>
						<span class="quick-btn-label">Stream Logs</span>
						<span class="quick-btn-sub">Live container output</span>
					</button>
				</div>
			</div>
		{/if}

		<!-- Terminal -->
		{#if activeTab === "terminal"}
			<SandboxTerminal targetId={targetId} targetType={workloadKind} workspaceDir={terminalWorkspaceDir} {config} />
		{/if}

		<!-- Files -->
		{#if activeTab === "files"}
			<div class="files-view anim-fade-up">
				<div class="files-layout">
					<!-- Left: browser -->
					<div class="files-browser panel">
						<div class="panel-header">
							<span class="panel-title">
								{filePayload?.kind === "file" ? browsePath.split("/").pop() : browsePath}
							</span>
							<div class="browser-nav">
								<button class="btn-ghost btn-xs" type="button"
									onclick={() => void loadPath(parentPath(browsePath))}
									disabled={browsePath === "/"}>
									<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>
									Up
								</button>
							</div>
						</div>
						<!-- Path input -->
						<form class="path-form" onsubmit={(e) => { e.preventDefault(); void loadPath(browsePath); }}>
							<input class="field path-field" bind:value={browsePath} required />
							<button class="btn-ghost btn-xs" type="submit" disabled={readLoading}>
								{readLoading ? "..." : "Go"}
							</button>
						</form>

						{#if filePayload?.kind === "directory"}
							<div class="entry-list">
								{#if directoryEntries.length === 0}
									<p class="entry-empty">Empty directory</p>
								{:else}
									{#each directoryEntries as entry (entry.path)}
										<button class="entry-row" type="button" onclick={() => void loadPath(entry.path)}>
											<span class="entry-icon">
												{#if entry.kind === "directory"}
													<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
												{:else}
													<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
												{/if}
											</span>
											<span class="entry-name">{entry.name}</span>
											{#if entry.kind === "file" && entry.size}
												<span class="entry-size">{entry.size}B</span>
											{/if}
										</button>
									{/each}
								{/if}
							</div>
						{:else if filePayload?.kind === "file"}
							<div class="file-actions">
								<button class="btn-primary btn-xs" type="button" onclick={() => void submitSaveFile()} disabled={saveLoading}>
									{saveLoading ? "Saving..." : "Save file"}
								</button>
								<button class="btn-ghost btn-xs" type="button" onclick={() => void loadPath(parentPath(browsePath))}>
									Back to dir
								</button>
							</div>
							<textarea class="editor-textarea" bind:value={editorContent} spellcheck={false}></textarea>
						{:else}
							<p class="entry-empty">Loading...</p>
						{/if}
					</div>

					<!-- Right: upload panel -->
					<div class="files-upload panel">
						<div class="panel-header">
							<span class="panel-title">Upload</span>
						</div>
						<div class="panel-body upload-body">
							<label class="field-col">
								<span class="section-label">Destination</span>
								<input class="field" bind:value={uploadPath} />
							</label>
							<label class="file-pick-label">
								<input type="file" class="file-pick-hidden" onchange={(e) => {
									const el = e.currentTarget as HTMLInputElement;
									uploadFile = el.files?.[0] ?? null;
									if (uploadFile !== null) {
										uploadPath = joinPath(uploadBasePath(), uploadFile.name);
									}
								}} />
								<span class="file-pick-display">
									<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
									{uploadFile ? uploadFile.name : "Choose file"}
								</span>
							</label>
							<button class="btn-primary btn-sm" type="button"
								onclick={() => void submitUploadFile()}
								disabled={uploadLoading || uploadFile === null}>
								{uploadLoading ? "Uploading..." : "Upload"}
							</button>
						</div>
					</div>
				</div>
			</div>
		{/if}

		<!-- Logs -->
		{#if activeTab === "logs"}
			<div class="logs-view anim-fade-up">
				<div class="logs-toolbar">
					<label class="field-col field-col-sm">
						<span class="section-label">Tail</span>
						<input class="field" bind:value={logTail} />
					</label>
					<label class="checkbox-label">
						<input type="checkbox" bind:checked={logFollow} />
						Follow
					</label>
					<div class="logs-btns">
						<button class="btn-primary btn-sm" type="button" onclick={() => void startLogs()} disabled={streaming}>
							{streaming ? "Streaming..." : "Start"}
						</button>
						<button class="btn-ghost btn-sm" type="button" onclick={stopLogs} disabled={!streaming}>Stop</button>
						{#if logEntries.length > 0}
							<button class="btn-ghost btn-sm" type="button" onclick={() => logEntries = []}>Clear</button>
						{/if}
					</div>
					{#if streaming}
						<div class="live-pill">
							<span class="live-dot"></span>
							Live
						</div>
					{/if}
				</div>

				<div class="panel logs-panel">
					<div class="panel-header">
						<span class="panel-title">Output</span>
						{#if logEntries.length > 0}
							<span class="log-count">{logEntries.length} lines</span>
						{/if}
					</div>
					<div bind:this={logsViewport} class="log-viewport">
						{#if logEntries.length === 0}
							<span class="log-empty">No output yet. Press Start above.</span>
						{:else}
							{#each logEntries as entry, i (`${entry.stream}-${i}`)}
								<div class="log-line log-line--{entry.stream}">
									<span class="log-stream">[{entry.stream}]</span>
									<span class="log-text">{@html entry.html}</span>
								</div>
							{/each}
						{/if}
					</div>
				</div>
			</div>
		{/if}

	</div>
</div>

<style>
	.workspace {
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 100vh;
	}

	/* ── Header ──────────────────────────────────────────────────────────────── */
	.ws-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.875rem 1.5rem;
		border-bottom: 1px solid var(--border-dim);
		background: var(--bg-surface);
		flex-shrink: 0;
		flex-wrap: wrap;
	}
	.ws-header-left {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		min-width: 0;
	}
	.back-btn {
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		background: transparent;
		border: 1px solid var(--border-mid);
		border-radius: 4px;
		color: var(--text-muted);
		cursor: pointer;
		flex-shrink: 0;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
	}
	.back-btn:hover {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}
	.ws-identity {
		display: flex;
		align-items: center;
		gap: 0.55rem;
		min-width: 0;
	}
	.ws-status-dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.ws-status-dot--ok    { background: var(--status-ok); box-shadow: 0 0 7px rgba(74,222,128,0.5); }
	.ws-status-dot--error { background: var(--status-error); }
	.ws-status-dot--idle  { background: var(--status-idle); }

	.ws-title {
		font-family: var(--font-display);
		font-size: 1.25rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		margin: 0;
		letter-spacing: -0.01em;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ws-status-badge {
		font-family: var(--font-mono);
		font-size: 0.58rem;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		padding: 0.12rem 0.45rem;
		border-radius: 3px;
		border: 1px solid transparent;
		flex-shrink: 0;
	}
	.ws-status-badge--ok    { color: var(--status-ok); border-color: var(--status-ok-border); background: var(--status-ok-bg); }
	.ws-status-badge--error { color: var(--status-error); border-color: var(--status-error-border); background: var(--status-error-bg); }
	.ws-status-badge--idle  { color: var(--text-muted); border-color: var(--border-dim); }

	.ws-header-right {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		flex-shrink: 0;
	}
	.port-links {
		display: flex;
		align-items: center;
		gap: 0.35rem;
	}
	.port-link {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-secondary);
		background: var(--bg-raised);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		padding: 0.18rem 0.5rem;
		text-decoration: none;
		transition: color 0.12s, border-color 0.12s;
	}
	.port-link:hover { color: var(--text-primary); border-color: var(--border-hi); }

	.ws-actions {
		display: flex;
		align-items: center;
		gap: 0.3rem;
	}
	.action-btn {
		background: transparent;
		border: 1px solid var(--border-dim);
		border-radius: 3px;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.62rem;
		padding: 0.25rem 0.55rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s, background 0.12s;
		white-space: nowrap;
	}
	.action-btn:hover:not(:disabled) {
		color: var(--text-primary);
		border-color: var(--border-hi);
		background: var(--accent-dim);
	}
	.action-btn:disabled { opacity: 0.35; cursor: not-allowed; }
	.action-btn--danger:hover:not(:disabled) {
		color: var(--status-error);
		border-color: var(--status-error-border);
		background: var(--status-error-bg);
	}
	.action-btn--confirming {
		color: var(--status-error) !important;
		border-color: var(--status-error-border) !important;
		background: var(--status-error-bg) !important;
		animation: pulse-confirm 0.25s var(--ease-spring);
	}
	@keyframes pulse-confirm {
		0%   { transform: scale(1); }
		50%  { transform: scale(1.06); }
		100% { transform: scale(1); }
	}

	/* ── Tab bar ─────────────────────────────────────────────────────────────── */
	.tab-bar {
		display: flex;
		align-items: center;
		gap: 0;
		padding: 0 1.5rem;
		border-bottom: 1px solid var(--border-dim);
		background: var(--bg-surface);
		flex-shrink: 0;
	}
	.tab-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.7rem 1rem;
		background: transparent;
		border: none;
		border-bottom: 2px solid transparent;
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.7rem;
		cursor: pointer;
		transition: color 0.12s, border-color 0.12s;
		white-space: nowrap;
		margin-bottom: -1px;
	}
	.tab-btn:hover:not(.tab-btn--active) { color: var(--text-secondary); }
	.tab-btn--active {
		color: var(--text-primary);
		border-bottom-color: var(--text-primary);
	}
	.tab-live-dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		background: var(--status-error);
		animation: blink 0.9s step-end infinite;
	}
	@keyframes blink { 0%, 100% { opacity: 1; } 50% { opacity: 0; } }

	/* ── Alerts ──────────────────────────────────────────────────────────────── */
	.alert-wrap {
		padding: 0.75rem 1.5rem 0;
		flex-shrink: 0;
	}

	/* ── Tab content ─────────────────────────────────────────────────────────── */
	.tab-content {
		flex: 1;
		overflow-y: auto;
		padding: 1.5rem;
	}

	/* ── Overview ────────────────────────────────────────────────────────────── */
	.overview {
		display: flex;
		flex-direction: column;
		gap: 1.5rem;
	}
	.meta-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: 0.75rem;
	}
	.meta-card {
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
		padding: 0.875rem 1rem;
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}
	.meta-card--wide { grid-column: span 2; }
	.meta-label {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--text-muted);
	}
	.meta-value {
		font-family: var(--font-mono);
		font-size: 0.72rem;
		color: var(--text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.meta-value.mono { font-family: var(--font-mono); }

	.port-chips { display: flex; flex-wrap: wrap; gap: 0.4rem; }
	.port-chip {
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-secondary);
		background: var(--bg-raised);
		border: 1px solid var(--border-mid);
		border-radius: 3px;
		padding: 0.15rem 0.5rem;
		text-decoration: none;
		transition: color 0.12s;
	}
	.port-chip:hover { color: var(--text-primary); }

	.quick-actions {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 0.75rem;
	}
	.quick-btn {
		background: var(--bg-surface);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-lg);
		padding: 1.25rem;
		cursor: pointer;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 0.5rem;
		text-align: left;
		color: var(--text-muted);
		transition: border-color 0.15s, background 0.15s, transform 0.15s var(--ease-snappy);
	}
	.quick-btn:hover {
		border-color: var(--border-hi);
		background: var(--bg-raised);
		transform: translateY(-1px);
		color: var(--text-secondary);
	}
	.quick-btn-label {
		font-family: var(--font-mono);
		font-size: 0.78rem;
		color: var(--text-primary);
		font-weight: 500;
	}
	.quick-btn-sub {
		font-family: var(--font-mono);
		font-size: 0.65rem;
		color: var(--text-muted);
	}

	/* ── Files ───────────────────────────────────────────────────────────────── */
	.files-layout {
		display: grid;
		grid-template-columns: 1fr 260px;
		gap: 1rem;
		align-items: start;
	}
	.browser-nav { display: flex; gap: 0.35rem; }
	.path-form {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.5rem 0.875rem;
		border-bottom: 1px solid var(--border-dim);
		background: var(--bg-overlay);
	}
	.path-field { flex: 1; font-size: 0.65rem; }
	.entry-list { overflow: hidden; }
	.entry-row {
		width: 100%;
		display: flex;
		align-items: center;
		gap: 0.55rem;
		padding: 0.48rem 0.875rem;
		background: transparent;
		border: 0;
		border-bottom: 1px solid var(--border-dim);
		color: var(--text-primary);
		text-align: left;
		cursor: pointer;
		transition: background 0.1s;
		font-family: var(--font-mono);
		font-size: 0.7rem;
	}
	.entry-row:last-child { border-bottom: 0; }
	.entry-row:hover { background: var(--bg-raised); }
	.entry-icon { color: var(--text-muted); display: flex; flex-shrink: 0; }
	.entry-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.entry-size { font-size: 0.58rem; color: var(--text-muted); flex-shrink: 0; }
	.entry-empty {
		padding: 1.5rem 0.875rem;
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.68rem;
		color: var(--text-muted);
	}
	.file-actions {
		display: flex;
		gap: 0.4rem;
		padding: 0.5rem 0.875rem;
		border-bottom: 1px solid var(--border-dim);
		background: var(--bg-overlay);
	}
	.editor-textarea {
		display: block;
		width: 100%;
		min-height: 28rem;
		font-family: var(--font-mono);
		font-size: 0.68rem;
		line-height: 1.65;
		background: #040404;
		border: none;
		border-top: 1px solid var(--border-dim);
		color: var(--text-primary);
		padding: 0.875rem;
		resize: vertical;
		outline: none;
		caret-color: var(--text-primary);
	}
	.upload-body {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}
	.field-col { display: flex; flex-direction: column; gap: 0.3rem; }
	.field-col-sm { max-width: 120px; }
	.file-pick-label { display: block; cursor: pointer; }
	.file-pick-hidden { display: none; }
	.file-pick-display {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-sm);
		color: var(--text-secondary);
		font-family: var(--font-mono);
		font-size: 0.68rem;
		padding: 0.4rem 0.6rem;
		transition: border-color 0.12s, color 0.12s;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.file-pick-label:hover .file-pick-display {
		border-color: var(--border-hi);
		color: var(--text-primary);
	}

	/* ── Logs ────────────────────────────────────────────────────────────────── */
	.logs-view {
		display: flex;
		flex-direction: column;
		gap: 1rem;
	}
	.logs-toolbar {
		display: flex;
		align-items: flex-end;
		gap: 0.875rem;
		flex-wrap: wrap;
		padding: 0.875rem 1rem;
		background: var(--bg-raised);
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
	}
	.logs-btns { display: flex; gap: 0.4rem; align-items: center; }
	.live-pill {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--status-error);
		background: var(--status-error-bg);
		border: 1px solid var(--status-error-border);
		border-radius: 3px;
		padding: 0.18rem 0.55rem;
	}
	.live-dot {
		width: 5px;
		height: 5px;
		border-radius: 50%;
		background: var(--status-error);
		animation: blink 0.9s step-end infinite;
	}
	.log-count {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		color: var(--text-muted);
	}
	.log-viewport {
		background: #040404;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		height: 36rem;
		overflow-y: auto;
		padding: 0.875rem 1rem;
		font-family: var(--font-mono);
		font-size: 0.68rem;
		line-height: 1.7;
	}
	.log-empty {
		color: var(--text-muted);
		font-family: var(--font-mono);
		font-size: 0.68rem;
	}
	.log-line {
		display: flex;
		gap: 0.6rem;
		padding: 0.05rem 0;
	}
	.log-line:hover {
		background: rgba(255,255,255,0.02);
	}
	.log-stream {
		flex-shrink: 0;
		color: var(--text-muted);
		min-width: 4rem;
		font-size: 0.6rem;
		opacity: 0.6;
		user-select: none;
	}
	.log-text {
		word-break: break-all;
		white-space: pre-wrap;
		flex: 1;
	}
	/* ANSI color classes from ansi-to-html */
	:global(.log-text .ansi-bold) { font-weight: bold; }
	:global(.log-text .ansi-italic) { font-style: italic; }
	:global(.log-text .ansi-underline) { text-decoration: underline; }
	.log-line--stdout .log-text { color: var(--code-stdout); }
	.log-line--stderr .log-text { color: var(--code-stderr); }
	.log-line--done   .log-text { color: var(--text-secondary); }
	.log-line--info   .log-text { color: var(--text-muted); }

	/* ── Responsive ──────────────────────────────────────────────────────────── */
	@media (max-width: 960px) {
		.quick-actions { grid-template-columns: 1fr 1fr; }
		.files-layout { grid-template-columns: 1fr; }
		.meta-card--wide { grid-column: span 1; }
	}
	@media (max-width: 640px) {
		.ws-header { padding: 0.75rem 1rem; }
		.tab-content { padding: 1rem; }
		.tab-bar { padding: 0 1rem; }
		.quick-actions { grid-template-columns: 1fr; }
		.ws-header-right { flex-wrap: wrap; }
	}
</style>
