<script lang="ts">
	import { invalidateWorkloadCaches } from "$lib/api-cache";
	import { onDestroy, onMount, tick } from "svelte";
	import {
		clearScheduledTimeout,
		dispatchAuthErrorEvent,
		scheduleTimeout,
		type TimeoutHandle
	} from "$lib/client/browser";
	import { toast, type ToastKind } from "$lib/toast.svelte";
	import SandboxTerminal from "$lib/components/SandboxTerminal.svelte";
	import EnvEditor from "$lib/components/EnvEditor.svelte";
	import ProxyConfigEditor from "$lib/components/ProxyConfigEditor.svelte";
	import Checkbox from "$lib/components/Checkbox.svelte";
	import AnsiToHtml from "ansi-to-html";
	import {
		deleteSandbox,
		formatApiFailure,
		readContainerFile,
		readSandboxFile,
		removeContainer,
		resolveContainerLogsSseUrl,
		restartContainer,
		resetContainer,
		resetSandboxStream,
		resolveApiUrl,
		resolveSandboxLogsSseUrl,
		restartSandbox,
		runApiEffect,
		saveContainerFile,
		saveSandboxFile,
		type SandboxPortProxyConfig,
		stopContainer,
		stopSandbox,
		updateSandboxEnv,
		updateSandboxProxyConfig,
		uploadContainerFile,
		uploadSandboxFile,
		type ApiConfig,
		type ContainerSummary,
		type FileReadResponse,
		type PreviewUrl,
		type Sandbox
	} from "$lib/api";
	import {
		formatSandboxProgress,
		formatSandboxProgressToast,
		type SandboxProgressDisplay
	} from "$lib/sandbox-progress";
	import {
		cloneSandboxEnv,
		listSandboxEnvKeys,
		normalizeSandboxEnv,
		normalizeSandboxSecretEnv,
		parseSandboxEnvEntry,
		serializeSandboxEnvEntry
	} from "$lib/sandbox-env";
	import { Context, Effect, Fiber, Layer, type Scope } from "effect";

	type WorkspaceTab = "overview" | "terminal" | "files" | "logs" | "env" | "proxy";

	let {
		sandbox = null,
		container,
		runtimeContainer = null,
		showTerminal = true,
		config,
		onBack,
		onDuplicate,
		onRefresh,
		onContainerReplaced,
		onDeleted
	} = $props<{
		sandbox?: Sandbox | null;
		container: ContainerSummary | null;
		runtimeContainer?: ContainerSummary | null;
		showTerminal?: boolean;
		config: ApiConfig;
		onBack: () => void;
		onDuplicate: () => void;
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
	let uploadPathError = $state("");

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
	let lastBackingContainerId = $state("");

	// Actions
	let actionLoading = $state<string | null>(null);
	let deleteConfirm = $state(false);
	let deleteConfirmTimer: TimeoutHandle | null = null;
	let editableProxyConfig = $state<Record<string, SandboxPortProxyConfig>>({});
	let savingProxyConfig = $state(false);
	let editableEnv = $state<string[]>([]);
	let editableSecretEnv = $state<string[]>([]);
	let removedSecretEnvKeys = $state<string[]>([]);
	let savingEnv = $state(false);
	let resetProgress = $state<SandboxProgressDisplay | null>(null);

	function cloneProxyConfig(
		value: Record<string, SandboxPortProxyConfig> | undefined
	): Record<string, SandboxPortProxyConfig> {
		if (!value) {
			return {};
		}
		return JSON.parse(JSON.stringify(value)) as Record<string, SandboxPortProxyConfig>;
	}

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
		loadingToast: (message: string) => Effect.Effect<string>;
		updateToast: (id: string, kind: ToastKind, message: string) => Effect.Effect<void>;
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
				const logsRequest =
					workloadKind === "sandbox"
						? resolveSandboxLogsSseUrl(targetId, { follow, tail: tail.trim() || "100" })
						: resolveContainerLogsSseUrl(targetId, { follow, tail: tail.trim() || "100" });

				const controller = yield* Effect.acquireRelease(
					Effect.sync(() => new AbortController()),
					(value) => Effect.sync(() => value.abort())
				);

				const headers = new Headers();
				headers.set("Accept", "text/event-stream");
				headers.set("Accept-Encoding", "identity");
				const token = config.token?.trim() ?? "";
				if (token.length > 0) {
					headers.set("Authorization", `Bearer ${token}`);
				}

				const response = yield* Effect.tryPromise({
					try: () =>
						fetch(
							resolveApiUrl(config, logsRequest.path, logsRequest.query),
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
			}),
		loadingToast: (message) =>
			Effect.sync(() => toast.loading(message)),
		updateToast: (id, kind, message) =>
			Effect.sync(() => {
				toast.update(id, kind, message);
			})
	};

	const workspaceLayer = Layer.mergeAll(
		Layer.succeed(WorkspaceFileIoService, workspaceFileIoService),
		Layer.succeed(WorkspaceLogStreamService, workspaceLogStreamService),
		Layer.succeed(WorkspaceFeedbackService, workspaceFeedbackService)
	);

	const runWorkspaceProgram = <A>(program: Effect.Effect<A, unknown>): Promise<A> => Effect.runPromise(program);

	const directoryEntries = $derived(filePayload?.kind === "directory" ? filePayload.entries ?? [] : []);

	type PreviewLink = {
		url: string;
		privatePort: number;
	};

	type EditablePreviewProxy = PreviewLink & {
		config: SandboxPortProxyConfig | null;
	};

	const previewLinks = (entries?: PreviewUrl[]): PreviewLink[] =>
		Array.from(
			new Map(
				(entries ?? [])
					.filter((entry) => entry.private_port > 0 && entry.url.trim().length > 0)
					.sort((a, b) => a.private_port - b.private_port)
					.map((entry) => {
						const resolvedURL = resolveApiUrl(config, entry.url);
						return [resolvedURL, { url: resolvedURL, privatePort: entry.private_port }] as const;
					})
			).values()
		);

	const mergedPreviewUrls = (primary?: PreviewUrl[], secondary?: PreviewUrl[]): PreviewUrl[] => {
		const seen = new Set<string>();
		const merged: PreviewUrl[] = [];
		for (const entry of [...(primary ?? []), ...(secondary ?? [])]) {
			const key = `${entry.private_port}:${entry.url}`;
			if (seen.has(key)) {
				continue;
			}
			seen.add(key);
			merged.push(entry);
		}
		return merged;
	};

	const statusInfo = (status: string): { label: string; cls: "ok" | "error" | "idle" } => {
		const n = status.toLowerCase();
		if (n.includes("up") || n.includes("running")) return { label: "running", cls: "ok" };
		if (n.includes("exit") || n.includes("dead") || n.includes("error")) return { label: "stopped", cls: "error" };
		return { label: "idle", cls: "idle" };
	};

	const workloadKind = $derived(runtimeContainer ? "container" : "sandbox");
	const runtimeComposeProject = $derived((runtimeContainer?.project_name ?? runtimeContainer?.labels?.["com.docker.compose.project"] ?? "").trim());
	const runtimeWorkloadKind = $derived((runtimeContainer?.workload_kind ?? "").trim().toLowerCase());
	const workloadLabel = $derived.by(() => {
		if (workloadKind === "sandbox") {
			return "sandbox";
		}
		if (runtimeComposeProject.length > 0 || runtimeWorkloadKind === "compose") {
			return "compose service";
		}
		return "container";
	});
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
	const previews = $derived(previewLinks(mergedPreviewUrls(activeContainer?.preview_urls, sandbox?.preview_urls)));
	const canReset = $derived.by(() => {
		if (workloadKind === "sandbox") {
			return true;
		}
		return activeContainer?.resettable ?? false;
	});
	const canBrowseFiles = $derived(backingContainerId.length > 0);
	const editablePreviews = $derived.by<EditablePreviewProxy[]>(() =>
		workloadKind !== "sandbox"
			? []
			: previews.map((preview) => ({
				...preview,
				config: editableProxyConfig[String(preview.privatePort)] ?? null
			}))
	);
	const proxyConfigDirty = $derived.by(
		() => JSON.stringify(editableProxyConfig) !== JSON.stringify(cloneProxyConfig(sandbox?.proxy_config))
	);
	const editableSecretEnvNormalized = $derived(normalizeSandboxSecretEnv(editableSecretEnv));
	const editableSecretDraftKeys = $derived(listSandboxEnvKeys(editableSecretEnv));
	const secretEnvKeys = $derived([...(sandbox?.secret_env_keys ?? [])]);
	const envDirty = $derived.by(
		() =>
			JSON.stringify(normalizeSandboxEnv(editableEnv)) !== JSON.stringify(cloneSandboxEnv(sandbox?.env)) ||
			editableSecretEnvNormalized.length > 0 ||
			removedSecretEnvKeys.length > 0
	);

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

	$effect(() => {
		sandbox?.id;
		sandbox?.updated_at;
		editableProxyConfig = cloneProxyConfig(sandbox?.proxy_config);
		editableEnv = cloneSandboxEnv(sandbox?.env);
		editableSecretEnv = [];
		removedSecretEnvKeys = [];
	});

	$effect(() => {
		const nextBackingContainerId = backingContainerId;
		const previousBackingContainerId = lastBackingContainerId;
		lastBackingContainerId = nextBackingContainerId;

		if (
			previousBackingContainerId.length === 0 ||
			nextBackingContainerId.length === 0 ||
			previousBackingContainerId === nextBackingContainerId
		) {
			return;
		}

		if (streaming) {
			void startLogs();
		}
	});

	const loadPathProgram = (pathToLoad: string): Effect.Effect<void, unknown> =>
		Effect.gen(function* () {
			const fileIo = yield* WorkspaceFileIoService;
			const feedback = yield* WorkspaceFeedbackService;

			const normalizedPath = pathToLoad.trim();
			if (normalizedPath.length === 0) {
				yield* feedback.error(new Error("Path cannot be empty."));
				return;
			}

			yield* Effect.sync(() => {
				readLoading = true;
			});

			try {
				const payload = yield* fileIo.read(normalizedPath);
				yield* Effect.sync(() => {
					filePayload = payload;
					browsePath = normalizedPath;
					editorContent = payload.kind === "file" ? payload.content ?? "" : "";
				});
			} catch (error) {
				yield* feedback.error(new Error(formatApiFailure(error)));
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

			const toastId = yield* feedback.loadingToast("Saving...");
			try {
				yield* fileIo.save(browsePath, editorContent);
				yield* feedback.updateToast(toastId, "ok", "File saved.");
				yield* Effect.sync(() => {
					if (filePayload?.kind === "file") {
						filePayload = { ...filePayload, content: editorContent };
					}
				});
			} catch (error) {
				yield* feedback.updateToast(toastId, "error", formatApiFailure(error));
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

			const destPath = uploadPath.trim();
			if (destPath.length === 0) {
				yield* Effect.sync(() => {
					uploadPathError = "Destination path cannot be empty.";
				});
				return;
			}

			yield* Effect.sync(() => {
				uploadLoading = true;
				uploadPathError = "";
			});

			const toastId = yield* feedback.loadingToast("Uploading...");
			try {
				yield* fileIo.upload(destPath, uploadFile);
				yield* feedback.updateToast(toastId, "ok", "File uploaded.");
				yield* loadPathProgram(filePayload?.kind === "directory" ? browsePath : parentPath(uploadPath));
			} catch (error) {
				yield* feedback.updateToast(toastId, "error", formatApiFailure(error));
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

	async function saveEnvSettings(): Promise<void> {
		if (workloadKind !== "sandbox" || !sandbox || savingEnv) {
			return;
		}

		savingEnv = true;
		const toastId = toast.loading("Applying environment changes...");
		try {
			const secretEnv = editableSecretEnvNormalized;
			const removedSecretKeys = [...new Set(removedSecretEnvKeys.map((key) => key.trim()).filter(Boolean))];
			const savedSandbox = await runApiEffect(
				updateSandboxEnv(config, sandbox.id, {
					env: normalizeSandboxEnv(editableEnv),
					secret_env: secretEnv.length > 0 ? secretEnv : undefined,
					remove_secret_env_keys: removedSecretKeys.length > 0 ? removedSecretKeys : undefined
				})
			);
			sandbox = savedSandbox;
			editableSecretEnv = [];
			removedSecretEnvKeys = [];
			Effect.runSync(invalidateWorkloadCaches(config));
			toast.update(toastId, "ok", "Environment variables saved.");
			await Promise.resolve(onRefresh());
		} catch (error) {
			toast.update(toastId, "error", formatApiFailure(error));
		} finally {
			savingEnv = false;
		}
	}

	function resetEnvSettings(): void {
		editableEnv = cloneSandboxEnv(sandbox?.env);
		editableSecretEnv = [];
		removedSecretEnvKeys = [];
	}

	function queueSecretReplacement(key: string): void {
		const normalizedKey = key.trim();
		if (normalizedKey.length === 0) {
			return;
		}

		removedSecretEnvKeys = removedSecretEnvKeys.filter((entry) => entry !== normalizedKey);
		if (editableSecretDraftKeys.includes(normalizedKey)) {
			return;
		}

		editableSecretEnv = [...editableSecretEnv, serializeSandboxEnvEntry(normalizedKey, "")];
	}

	function toggleSecretRemoval(key: string): void {
		const normalizedKey = key.trim();
		if (normalizedKey.length === 0) {
			return;
		}

		removedSecretEnvKeys = removedSecretEnvKeys.includes(normalizedKey)
			? removedSecretEnvKeys.filter((entry) => entry !== normalizedKey)
			: [...removedSecretEnvKeys, normalizedKey];
		editableSecretEnv = editableSecretEnv.filter((entry) => parseSandboxEnvEntry(entry).key !== normalizedKey);
	}

	async function saveProxySettings(): Promise<void> {
		if (workloadKind !== "sandbox" || !sandbox || savingProxyConfig) {
			return;
		}

		savingProxyConfig = true;
		const toastId = toast.loading("Saving proxy settings...");
		try {
			const savedSandbox = await runApiEffect(
				updateSandboxProxyConfig(config, sandbox.id, editableProxyConfig)
			);
			sandbox = savedSandbox;
			toast.update(toastId, "ok", "Proxy settings saved.");
			await Promise.resolve(onRefresh());
		} catch (error) {
			toast.update(toastId, "error", formatApiFailure(error));
		} finally {
			savingProxyConfig = false;
		}
	}

	function clearProxySettings(): void {
		editableProxyConfig = {};
	}

	async function handleAction(action: "restart" | "reset" | "stop" | "delete"): Promise<void> {
		actionLoading = action;
		let resetToastId: string | null = null;
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
					resetProgress = null;
					const toastId = toast.loading("Resetting sandbox...");
					resetToastId = toastId;
					await runApiEffect(
						resetSandboxStream(config, targetId, (event) => {
							if (event.event === "progress") {
								resetProgress = formatSandboxProgress(event.data);
								toast.update(toastId, "loading", formatSandboxProgressToast("Resetting sandbox", event.data));
							}
						})
					);
					Effect.runSync(invalidateWorkloadCaches(config));
					toast.update(toastId, "ok", "Reset to clean workspace.");
					await Promise.resolve(onRefresh());
					await loadPath(workspaceDirValue);
				} else {
					if (!canReset) {
						throw new Error("Reset is only available for managed standalone containers and compose services.");
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
			resetProgress = null;
			if (resetToastId !== null) {
				toast.update(resetToastId, "error", formatApiFailure(error));
			} else {
				toast.error(formatApiFailure(error));
			}
		} finally {
			resetProgress = null;
			actionLoading = null;
		}
	}

	const tabs = $derived.by<Array<{ id: WorkspaceTab; label: string }>>(() => [
		{ id: "overview", label: "Overview" },
		...(showTerminal ? [{ id: "terminal" as const, label: "Terminal" }] : []),
		{ id: "files", label: "Files" },
		{ id: "logs", label: "Logs" },
		...(workloadKind === "sandbox" ? [{ id: "env" as const, label: "Environment" }] : []),
		...(workloadKind === "sandbox" && editablePreviews.length > 0 ? [{ id: "proxy" as const, label: "Proxy" }] : [])
	]);

	$effect(() => {
		if (!showTerminal && activeTab === "terminal") {
			activeTab = "overview";
		}
	});
</script>

<div class="workspace anim-fade-up">
	<!-- ── Workspace header ───────────────────────────────────────────────────── -->
	<header class="ws-header">
		<div class="ws-header-left">
			<button class="back-btn" type="button" onclick={onBack} aria-label={`Back to workloads list from this ${workloadLabel}`}>
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
			{#if previews.length > 0}
				<div class="port-links">
					{#each previews as preview}
						<a class="port-link" href={preview.url} target="_blank" rel="noreferrer" title={preview.url}>
							<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
								<path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
								<polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
							</svg>
							:{preview.privatePort}
						</a>
					{/each}
				</div>
			{/if}
			<div class="ws-actions">
				<button class="action-btn" type="button" onclick={onDuplicate} disabled={actionLoading !== null}>
					Duplicate
				</button>
				<button class="action-btn" type="button" onclick={() => void handleAction("restart")} disabled={actionLoading !== null}>
					{actionLoading === "restart" ? "..." : "Restart"}
				</button>
				<button class="action-btn" type="button" onclick={() => void handleAction("stop")} disabled={actionLoading !== null}>
					{actionLoading === "stop" ? "..." : "Stop"}
				</button>
				{#if canReset}
					<button class="action-btn" type="button" onclick={() => void handleAction("reset")} disabled={actionLoading !== null}>
						{actionLoading === "reset" ? (resetProgress ? `Reset · ${resetProgress.phaseLabel}` : "...") : "Reset"}
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

	{#if resetProgress}
		<div class="operation-progress operation-progress--{resetProgress.tone}" role="status" aria-live="polite">
			<div class="operation-progress-copy">
				<span class="operation-progress-label">Reset in progress</span>
				<strong>{resetProgress.phaseLabel}</strong>
				<p>{resetProgress.detail}</p>
			</div>
			<span class="operation-progress-status">{resetProgress.statusLabel}</span>
		</div>
	{/if}

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
						<span class="meta-label">
							{workloadLabel === "sandbox" ? "Workspace" : workloadLabel === "compose service" ? "Compose service path" : "Container path"}
						</span>
						<span class="meta-value mono">{workspaceMetaValue}</span>
					</div>
					{#if workloadCreatedAt > 0}
						<div class="meta-card">
							<span class="meta-label">Created</span>
							<span class="meta-value">{formatDate(workloadCreatedAt)}</span>
						</div>
					{/if}
					{#if previews.length > 0}
						<div class="meta-card meta-card--wide">
							<span class="meta-label">Exposed ports</span>
							<div class="port-chips">
								{#each previews as preview}
									<a class="port-chip" href={preview.url} target="_blank" rel="noreferrer" title={preview.url}>:{preview.privatePort}</a>
								{/each}
							</div>
						</div>
					{/if}
					</div>

				<!-- Quick actions -->
				<div class="quick-actions">
					{#if showTerminal}
						<button class="quick-btn" type="button" onclick={() => activeTab = "terminal"}>
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
								<polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>
							</svg>
							<span class="quick-btn-label">Open Terminal</span>
							<span class="quick-btn-sub">Run commands in this {workloadLabel}</span>
						</button>
					{/if}
					<button class="quick-btn" type="button" onclick={() => activeTab = "files"} disabled={!canBrowseFiles}>
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
							<path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/>
						</svg>
						<span class="quick-btn-label">Browse Files</span>
						<span class="quick-btn-sub">Read, edit and upload files for this {workloadLabel}</span>
					</button>
					<button class="quick-btn" type="button" onclick={() => { activeTab = "logs"; void startLogs(); }}>
						<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
							<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
						</svg>
						<span class="quick-btn-label">Stream Logs</span>
						<span class="quick-btn-sub">Live {workloadLabel} output</span>
					</button>
					{#if workloadKind === "sandbox" && editablePreviews.length > 0}
						<button class="quick-btn" type="button" onclick={() => activeTab = "proxy"}>
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/></svg>
							<span class="quick-btn-label">Proxy Settings</span>
							<span class="quick-btn-sub">Configure headers, auth, CORS for preview ports</span>
						</button>
					{/if}
					{#if workloadKind === "sandbox"}
						<button class="quick-btn" type="button" onclick={() => activeTab = "env"}>
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M4 12h16M4 17h10"/></svg>
							<span class="quick-btn-label">Environment</span>
							<span class="quick-btn-sub">Edit runtime variables for this sandbox</span>
						</button>
					{/if}
				</div>
			</div>
		{/if}

		{#if activeTab === "env" && workloadKind === "sandbox"}
			<div class="env-view anim-fade-up">
				<div class="env-view-header">
					<div>
						<h2 class="env-view-title">Sandbox environment</h2>
						<p class="env-view-sub">Update key/value pairs for this sandbox runtime. Saving reapplies the runtime config and may recreate the running sandbox container.</p>
					</div>
					<div class="env-view-actions">
						<button class="btn-ghost btn-xs" type="button" onclick={resetEnvSettings} disabled={savingEnv || !envDirty}>
							Reset
						</button>
						<button class="btn-primary btn-xs" type="button" onclick={() => void saveEnvSettings()} disabled={savingEnv || !envDirty}>
							{savingEnv ? "Saving..." : "Save environment"}
						</button>
					</div>
				</div>
				<div class="env-view-card panel">
					<div class="env-section">
						<h3 class="env-section-title">Visible environment variables</h3>
						<EnvEditor bind:value={editableEnv} addLabel="Add environment variable" emptyMessage="No environment variables configured for this sandbox." />
						<p class="env-view-note">Rows without a key are skipped when saving. Empty values are preserved.</p>
					</div>
					<div class="env-section env-section--secret">
						<div class="env-section-header">
							<div>
								<h3 class="env-section-title">Secret environment variables</h3>
								<p class="env-section-sub">Secret values are write-only. Existing keys are shown below, but values cannot be viewed after save or refresh.</p>
							</div>
						</div>
						<EnvEditor
							bind:value={editableSecretEnv}
							addLabel="Add secret"
							emptyMessage="No pending secret changes. Add a key/value pair to create or replace a secret."
							valuePlaceholder="Enter a secret value"
							valueInputType="password"
						/>
						<p class="env-view-note">Only rows with both a key and value are saved. Pending secret values are cleared after a successful save.</p>
						<div class="secret-keys-block">
							<div class="secret-keys-header">
								<span class="secret-keys-title">Current secret keys</span>
								{#if secretEnvKeys.length > 0}
									<span class="secret-keys-count">{secretEnvKeys.length}</span>
								{/if}
							</div>
							{#if secretEnvKeys.length === 0}
								<p class="env-view-note">No saved secret keys for this sandbox.</p>
							{:else}
								<div class="secret-key-list">
									{#each secretEnvKeys as key}
										<div class:secret-key-item--pending-remove={removedSecretEnvKeys.includes(key)} class="secret-key-item">
											<div class="secret-key-meta">
												<code class="inline-code">{key}</code>
												{#if editableSecretDraftKeys.includes(key)}
													<span class="secret-key-status">Pending replacement</span>
												{:else if removedSecretEnvKeys.includes(key)}
													<span class="secret-key-status">Pending removal</span>
												{/if}
											</div>
											<div class="secret-key-actions">
												<button class="btn-ghost btn-xs" type="button" onclick={() => queueSecretReplacement(key)} disabled={savingEnv || removedSecretEnvKeys.includes(key)}>
													Replace
												</button>
												<button class="btn-ghost btn-xs" type="button" onclick={() => toggleSecretRemoval(key)} disabled={savingEnv}>
													{removedSecretEnvKeys.includes(key) ? "Undo remove" : "Remove"}
												</button>
											</div>
										</div>
									{/each}
								</div>
							{/if}
						</div>
					</div>
				</div>
			</div>
		{/if}

		<!-- Terminal -->
		{#if activeTab === "terminal"}
			{#key `${targetId}:${backingContainerId}`}
				<SandboxTerminal targetId={targetId} targetType={workloadKind} workspaceDir={terminalWorkspaceDir} {config} />
			{/key}
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
							<div class="entry-list-wrap">
								{#if readLoading}
									<div class="entry-loading-overlay" aria-label="Loading directory">
										<svg class="entry-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="16" height="16"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
									</div>
								{/if}
								<div class="entry-list" class:entry-list--loading={readLoading}>
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
							<div class="entry-initial-load">
								<svg class="entry-spinner" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="16" height="16"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
							</div>
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
								<input
									class="field"
									class:field--error={uploadPathError}
									bind:value={uploadPath}
									oninput={() => uploadPathError = ""}
								/>
								{#if uploadPathError}
									<span class="field-inline-error">{uploadPathError}</span>
								{/if}
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
					<Checkbox bind:checked={logFollow} label="Follow" labelClass="logs-follow" />
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

		<!-- Proxy settings -->
		{#if activeTab === "proxy"}
			<div class="proxy-view anim-fade-up">
				<div class="proxy-view-header">
					<div>
						<h2 class="proxy-view-title">Per-port proxy settings</h2>
						<p class="proxy-view-sub">Customize headers, auth, path stripping, and CORS for preview ports.</p>
					</div>
					<div class="proxy-view-actions">
						<button class="btn-ghost btn-xs" type="button" onclick={clearProxySettings} disabled={savingProxyConfig || !proxyConfigDirty}>
							Clear all
						</button>
						<button class="btn-primary btn-xs" type="button" onclick={() => void saveProxySettings()} disabled={savingProxyConfig || !proxyConfigDirty}>
							{savingProxyConfig ? "Saving..." : "Save settings"}
						</button>
					</div>
				</div>
				<div class="proxy-port-list">
					{#each editablePreviews as preview (preview.privatePort)}
						<div class="proxy-port-block">
							<div class="proxy-port-row">
								<div class="proxy-port-label">
									<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/></svg>
									Port <code class="inline-code">{preview.privatePort}</code>
								</div>
								<a class="proxy-port-link" href={preview.url} target="_blank" rel="noreferrer">Open preview</a>
							</div>
							<ProxyConfigEditor
								value={preview.config}
								onchange={(v: SandboxPortProxyConfig | null) => {
									const key = String(preview.privatePort);
									if (v === null) {
										const next = { ...editableProxyConfig };
										delete next[key];
										editableProxyConfig = next;
									} else {
										editableProxyConfig = { ...editableProxyConfig, [key]: v };
									}
								}}
							/>
						</div>
					{/each}
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

	.operation-progress {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 0.9rem;
		margin: 0.9rem 1.5rem 0;
		padding: 0.9rem 1rem;
		border: 1px solid var(--border-mid);
		border-radius: var(--radius-md);
		background: color-mix(in srgb, var(--bg-raised) 88%, transparent);
	}

	.operation-progress--active {
		border-color: color-mix(in srgb, var(--accent) 38%, var(--border-mid));
	}

	.operation-progress--ok {
		border-color: var(--status-ok-border);
	}

	.operation-progress--error {
		border-color: var(--status-error-border);
	}

	.operation-progress-copy {
		display: flex;
		flex-direction: column;
		gap: 0.22rem;
		min-width: 0;
	}

	.operation-progress-copy strong {
		font-size: 0.84rem;
		font-weight: 500;
		color: var(--text-primary);
	}

	.operation-progress-copy p {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.64rem;
		line-height: 1.5;
		color: var(--text-muted);
	}

	.operation-progress-label,
	.operation-progress-status {
		font-family: var(--font-mono);
		font-size: 0.6rem;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--text-secondary);
	}

	.operation-progress-status {
		flex: none;
		padding-top: 0.05rem;
		text-align: right;
	}

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

	.proxy-config-card {
		gap: 0.9rem;
	}

	.proxy-config-head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 0.75rem;
		flex-wrap: wrap;
	}

	.proxy-config-subtitle {
		margin: 0.3rem 0 0;
		font-family: var(--font-mono);
		font-size: 0.66rem;
		color: var(--text-muted);
	}

	.proxy-config-actions {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		flex-wrap: wrap;
	}

	/* Proxy tab view */
	.env-view,
	.proxy-view {
		padding: 1.5rem;
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}

	.env-view-header,
	.proxy-view-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 0.75rem;
		flex-wrap: wrap;
	}

	.env-view-title,
	.proxy-view-title {
		margin: 0;
		font-family: var(--font-display);
		font-size: 1rem;
		font-weight: 400;
		font-style: italic;
		color: var(--text-primary);
		letter-spacing: -0.01em;
	}

	.env-view-sub,
	.proxy-view-sub {
		margin: 0.3rem 0 0;
		font-family: var(--font-mono);
		font-size: 0.66rem;
		color: var(--text-muted);
	}

	.env-view-actions,
	.proxy-view-actions {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		flex-wrap: wrap;
	}

	.env-view-card {
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.env-section {
		display: flex;
		flex-direction: column;
		gap: 0.65rem;
	}

	.env-section--secret {
		padding-top: 0.2rem;
		border-top: 1px solid var(--border-dim);
	}

	.env-section-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 0.65rem;
		flex-wrap: wrap;
	}

	.env-section-title {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.7rem;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-secondary);
	}

	.env-section-sub {
		margin: 0.25rem 0 0;
		font-family: var(--font-mono);
		font-size: 0.64rem;
		color: var(--text-muted);
	}

	.env-view-note {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.66rem;
		color: var(--text-muted);
	}

	.secret-keys-block {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.8rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-md);
		background: color-mix(in srgb, var(--bg-raised) 80%, transparent);
	}

	.secret-keys-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.secret-keys-title,
	.secret-keys-count,
	.secret-key-status {
		font-family: var(--font-mono);
		font-size: 0.62rem;
	}

	.secret-keys-title {
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--text-secondary);
	}

	.secret-keys-count,
	.secret-key-status {
		color: var(--text-muted);
	}

	.secret-key-list {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
	}

	.secret-key-item {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.75rem;
		flex-wrap: wrap;
		padding: 0.6rem 0.7rem;
		border: 1px solid var(--border-dim);
		border-radius: var(--radius-sm);
		background: color-mix(in srgb, var(--bg-elevated) 74%, transparent);
	}

	.secret-key-item--pending-remove {
		opacity: 0.72;
		border-color: var(--status-error-border);
		background: color-mix(in srgb, var(--status-error-bg) 55%, var(--bg-elevated));
	}

	.secret-key-meta,
	.secret-key-actions {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		flex-wrap: wrap;
	}

	.proxy-port-list {
		display: flex;
		flex-direction: column;
		gap: 0.9rem;
	}

	.proxy-port-block {
		display: flex;
		flex-direction: column;
		gap: 0.45rem;
	}

	.proxy-port-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.proxy-port-label {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-muted);
	}

	.proxy-port-link {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--text-secondary);
		text-decoration: none;
	}

	.proxy-port-link:hover {
		color: var(--text-primary);
	}

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
	.entry-list-wrap {
		position: relative;
	}
	.entry-list--loading {
		opacity: 0.35;
		pointer-events: none;
		user-select: none;
	}
	.entry-loading-overlay {
		position: absolute;
		inset: 0;
		z-index: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		background: rgba(0, 0, 0, 0.15);
	}
	.entry-spinner {
		color: var(--text-muted);
		animation: entry-spin 0.75s linear infinite;
	}
	.entry-initial-load {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 2rem;
	}
	@keyframes entry-spin {
		from { transform: rotate(0deg); }
		to   { transform: rotate(360deg); }
	}
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

	.field--error {
		border-color: var(--status-error-border) !important;
	}

	.field-inline-error {
		font-family: var(--font-mono);
		font-size: 0.62rem;
		color: var(--status-error);
		margin-top: 0.1rem;
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
	:global(.logs-follow) {
		--cb-font-size: 0.64rem;
		--cb-gap: 0.8rem;
		--cb-box-size: 1.35em;
		margin-bottom: 0.4rem;
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
		.operation-progress { margin: 0.9rem 1rem 0; flex-direction: column; }
		.operation-progress-status { text-align: left; }
		.quick-actions { grid-template-columns: 1fr; }
		.ws-header-right { flex-wrap: wrap; }
	}
</style>
