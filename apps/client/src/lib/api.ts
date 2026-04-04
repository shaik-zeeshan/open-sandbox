import { browser } from "$app/environment";
import { FetchHttpClient, HttpClient, HttpClientRequest } from "@effect/platform";
import { Data, Effect, ManagedRuntime, pipe } from "effect";

export interface ApiConfig {
	baseUrl: string;
	token?: string;
}

export interface LoginResponse {
	token: string;
	token_type: string;
	user_id: string;
	username: string;
	role: string;
	expires_at: number;
}

export interface SessionResponse {
	authenticated: boolean;
	user_id: string;
	username: string;
	role: string;
	expires_at: number;
}

export interface SetupStatusResponse {
	bootstrap_required: boolean;
}

export interface UserSummary {
	id: string;
	username: string;
	role: string;
	created_at: number;
	updated_at: number;
}

export interface BuildImageRequest {
	context_path?: string;
	dockerfile?: string;
	dockerfile_content?: string;
	context_files?: Record<string, string>;
	tag: string;
	build_args?: Record<string, string>;
}

export interface PullImageRequest {
	image: string;
	tag?: string;
}

export interface ImageSummary {
	id: string;
	repo_tags: string[];
	created: number;
	size: number;
}

export interface ImageSearchResult {
	name: string;
	description: string;
	stars: number;
	official: boolean;
	automated: boolean;
}

export interface RemoveImageResponse {
	deleted: Array<{
		Deleted?: string;
		Untagged?: string;
	}>;
}

export interface ComposeRequest {
	content: string;
	project_name?: string;
	services?: string[];
	volumes?: boolean;
	remove_orphans?: boolean;
}

export interface ComposeResponse {
	stdout: string;
	stderr: string;
}

export interface ComposeStatusResponse {
	services: unknown;
	raw: string;
}

export interface GitCloneRequest {
	container_id: string;
	repo_url: string;
	target_path: string;
	branch?: string;
}

export interface CreateContainerRequest {
	image: string;
	name?: string;
	cmd?: string[];
	env?: string[];
	workdir?: string;
	tty?: boolean;
	user?: string;
	binds?: string[];
	ports?: string[];
	auto_remove?: boolean;
	start?: boolean;
}

export interface CreateContainerResponse {
	container_id: string;
	warnings: string[];
	started: boolean;
}

export interface ExecRequest {
	cmd: string[];
	workdir?: string;
	env?: string[];
	detach?: boolean;
	tty?: boolean;
	user?: string;
}

export interface ExecResponse {
	exec_id: string;
	exit_code?: number;
	stdout?: string;
	stderr?: string;
	detached: boolean;
}

export interface ContainerSummary {
	id: string;
	names: string[];
	image: string;
	state: string;
	status: string;
	created: number;
	labels: Record<string, string>;
	ports?: PortSummary[];
}

export interface PortSummary {
	private: number;
	public?: number;
	type: string;
	ip?: string;
}

export interface CreateSandboxRequest {
	name: string;
	image: string;
	repo_url?: string;
	branch?: string;
	repo_target_path?: string;
	use_image_default_cmd?: boolean;
	env?: string[];
	cmd?: string[];
	workdir?: string;
	tty?: boolean;
	user?: string;
	ports?: string[];
}

export interface Sandbox {
	id: string;
	name: string;
	image: string;
	container_id: string;
	workspace_dir: string;
	repo_url?: string;
	status: string;
	owner_username?: string;
	ports?: PortSummary[];
	created_at: number;
	updated_at: number;
}

export interface FileEntry {
	name: string;
	path: string;
	kind: "file" | "directory";
	size?: number;
}

export interface FileReadResponse {
	path: string;
	name: string;
	kind: "file" | "directory";
	content?: string;
	entries?: FileEntry[];
}

const DEFAULT_BASE_URL = "http://localhost:8080";

export class ApiError extends Data.TaggedError("ApiError")<{
	readonly status: number;
	readonly message: string;
	readonly body: unknown;
}> {}

export class NetworkError extends Data.TaggedError("NetworkError")<{
	readonly message: string;
	readonly cause: unknown;
}> {}

export class AuthError extends Data.TaggedError("AuthError")<{
	readonly message: string;
	readonly reason?: string;
}> {}

export type ApiFailure = ApiError | NetworkError | AuthError;

const runtime = ManagedRuntime.make(FetchHttpClient.layer);

const normalizeBaseUrl = (baseUrl: string): string => {
	const trimmed = baseUrl.trim();
	if (trimmed.length === 0) {
		return DEFAULT_BASE_URL;
	}
	const withScheme = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
	return withScheme.replace(/\/+$/, "");
};

const normalizePath = (path: string): string => (path.startsWith("/") ? path : `/${path}`);

const isRecord = (value: unknown): value is Record<string, unknown> =>
	typeof value === "object" && value !== null;

const mapAuthReasonToMessage = (reason?: string): string => {
	switch (reason) {
		case "invalid_credentials":
			return "Invalid credentials.";
		case "token_missing":
			return "Unauthorized: missing token. Please log in.";
		case "token_expired":
			return "Unauthorized: your session expired. Please log in again.";
		case "token_invalid":
			return "Unauthorized: token is invalid. Please log in again.";
		default:
			return "Unauthorized: your session is missing or expired.";
	}
};

const extractMessage = (payload: unknown, fallback: string): string => {
	if (typeof payload === "string" && payload.trim().length > 0) {
		return payload;
	}

	if (isRecord(payload)) {
		const errorField = payload.error;
		if (typeof errorField === "string" && errorField.trim().length > 0) {
			return errorField;
		}

		const messageField = payload.message;
		if (typeof messageField === "string" && messageField.trim().length > 0) {
			return messageField;
		}
	}

	return fallback;
};

const extractAuthReason = (payload: unknown): string | undefined => {
	if (!isRecord(payload)) {
		return undefined;
	}

	const reason = payload.reason;
	if (typeof reason === "string" && reason.trim().length > 0) {
		return reason.trim();
	}

	return undefined;
};

const readResponsePayload = async (response: Response): Promise<unknown> => {
	try {
		const text = await response.text();
		if (text.trim().length === 0) {
			return "";
		}
		try {
			return JSON.parse(text) as unknown;
		} catch {
			return text;
		}
	} catch {
		return "";
	}
};

const fetchJson = async <T>(
	config: ApiConfig,
	path: string,
	init?: RequestInit,
	options?: { notifyAuthError?: boolean }
): Promise<T> => {
	const headers = new Headers(init?.headers);
	const token = config.token?.trim() ?? "";
	if (token.length > 0) {
		headers.set("Authorization", `Bearer ${token}`);
	}

	let response: Response;
	try {
		response = await fetch(resolveApiUrl(config, path), {
			...init,
			credentials: "include",
			headers
		});
	} catch (error) {
		throw new NetworkError({
			message: error instanceof Error ? error.message : "Network request failed.",
			cause: error
		});
	}

	if (response.status === 401) {
		const payload = await readResponsePayload(response);
		const reason = extractAuthReason(payload);
		if ((options?.notifyAuthError ?? true) && browser) {
			window.dispatchEvent(new CustomEvent("open-sandbox:auth-error"));
		}
		throw new AuthError({ message: mapAuthReasonToMessage(reason), reason });
	}

	if (!response.ok) {
		const payload = await readResponsePayload(response);
		throw new ApiError({
			status: response.status,
			message: extractMessage(payload, `Request failed with status ${response.status}`),
			body: payload
		});
	}

	const payload = await readResponsePayload(response);
	return payload as T;
};

const configureRequest = (
	config: ApiConfig,
	request: HttpClientRequest.HttpClientRequest
): HttpClientRequest.HttpClientRequest => {
	const baseUrl = normalizeBaseUrl(config.baseUrl);
	const token = config.token?.trim() ?? "";

	let configured = pipe(request, HttpClientRequest.prependUrl(baseUrl));
	if (token.length > 0) {
		configured = HttpClientRequest.setHeader(configured, "Authorization", `Bearer ${token}`);
	}

	return configured;
};

const requestJson = <T>(
	config: ApiConfig,
	request: HttpClientRequest.HttpClientRequest
): Effect.Effect<T, ApiFailure, HttpClient.HttpClient> =>
	Effect.gen(function* () {
		const client = yield* HttpClient.HttpClient;
		const response = yield* pipe(
			Effect.provideService(
				client.execute(configureRequest(config, request)),
				FetchHttpClient.RequestInit,
				{ credentials: "include" }
			),
			Effect.mapError((error) => new NetworkError({ message: error.message, cause: error }))
		);

		if (response.status === 401) {
			const payload = yield* pipe(
				response.json,
				Effect.orElse(() => response.text),
				Effect.orElseSucceed(() => "")
			);
			const reason = extractAuthReason(payload);
			return yield* Effect.fail(new AuthError({ message: mapAuthReasonToMessage(reason), reason }));
		}

		if (response.status < 200 || response.status >= 300) {
			const payload = yield* pipe(
				response.json,
				Effect.orElse(() => response.text),
				Effect.orElseSucceed(() => "")
			);

			return yield* Effect.fail(
				new ApiError({
					status: response.status,
					message: extractMessage(payload, `Request failed with status ${response.status}`),
					body: payload
				})
			);
		}

		const payload = yield* pipe(
			response.json,
			Effect.mapError(
				(error) =>
					new ApiError({
						status: response.status,
						message: "Unable to decode JSON response.",
						body: error
					})
			)
		);

		return payload as T;
	});

const postJson = <TPayload, TResponse>(
	config: ApiConfig,
	path: string,
	payload: TPayload
): Effect.Effect<TResponse, ApiFailure, HttpClient.HttpClient> => {
	const request = pipe(HttpClientRequest.post(path), HttpClientRequest.bodyUnsafeJson(payload));
	return requestJson<TResponse>(config, request);
};

export const runApiEffect = async <A>(
	effect: Effect.Effect<A, ApiFailure, HttpClient.HttpClient>,
	options?: { notifyAuthError?: boolean }
): Promise<A> => {
	try {
		return await runtime.runPromise(effect);
	} catch (error) {
		const notifyAuthError = options?.notifyAuthError ?? true;
		const taggedError = typeof error === "object" && error !== null ? (error as { _tag?: string }) : null;
		if (
			notifyAuthError &&
			browser &&
			taggedError !== null &&
			taggedError._tag === "AuthError"
		) {
			window.dispatchEvent(new CustomEvent("open-sandbox:auth-error"));
		}

		throw error;
	}
};

export const login = (
	config: ApiConfig,
	username: string,
	password: string
): Effect.Effect<LoginResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/login", { username, password });

export const getSetupStatus = (
	config: ApiConfig
): Effect.Effect<SetupStatusResponse, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/auth/setup"));

export const bootstrap = (
	config: ApiConfig,
	username: string,
	password: string
): Effect.Effect<LoginResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/bootstrap", { username, password });

export const getSession = (
	config: ApiConfig
): Effect.Effect<SessionResponse, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/auth/session"));

export const logout = (
	config: ApiConfig
): Effect.Effect<{ signed_out: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/logout", {});

export const listUsers = (
	config: ApiConfig
): Effect.Effect<UserSummary[], ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/api/users"));

export const createUser = (
	config: ApiConfig,
	request: { username: string; password: string; role?: string }
): Effect.Effect<UserSummary, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/api/users", request);

export const updateUserPassword = (
	config: ApiConfig,
	userId: string,
	password: string
): Effect.Effect<{ id: string; updated: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/users/${encodeURIComponent(userId)}/password`, { password });

export const deleteUser = (
	config: ApiConfig,
	userId: string
): Effect.Effect<{ id: string; deleted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.del(`/api/users/${encodeURIComponent(userId)}`));

export const resolveApiUrl = (
	config: ApiConfig,
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): string => {
	const base = normalizeBaseUrl(config.baseUrl);
	const baseWithSlash = `${base}/`;
	const url = new URL(normalizePath(path), baseWithSlash);

	if (query) {
		for (const [key, value] of Object.entries(query)) {
			if (value === undefined || value === "") {
				continue;
			}
			url.searchParams.set(key, String(value));
		}
	}

	return url.toString();
};

export const resolveWebSocketUrl = (
	config: ApiConfig,
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): string => {
	const apiUrl = new URL(resolveApiUrl(config, path, query));
	apiUrl.protocol = apiUrl.protocol === "https:" ? "wss:" : "ws:";
	return apiUrl.toString();
};

export const formatApiFailure = (error: unknown): string => {
	if (isRecord(error) && typeof error._tag === "string") {
		switch (error._tag) {
			case "AuthError":
				return typeof error.message === "string" ? error.message : "Unauthorized";
			case "ApiError":
				return typeof error.message === "string"
					? error.message
					: "Server returned an unexpected response.";
			case "NetworkError":
				return typeof error.message === "string" ? error.message : "Network request failed.";
		}
	}

	if (error instanceof Error) {
		return error.message;
	}

	return "Unexpected error";
};

export const healthCheck = (config: ApiConfig): Effect.Effect<{ status: string }, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/health"));

export const listImages = (
	config: ApiConfig
): Effect.Effect<ImageSummary[], ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/api/images"));

export const searchImages = (
	config: ApiConfig,
	query: string,
	limit = 25
): Effect.Effect<ImageSearchResult[], ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/api/images/search", {
		urlParams: {
			q: query,
			limit: String(limit)
		}
	}));

export const pullImage = (
	config: ApiConfig,
	request: PullImageRequest
): Effect.Effect<{ output: string; image: string }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/api/images/pull", request);

export const buildImage = (
	config: ApiConfig,
	request: BuildImageRequest
): Effect.Effect<{ output: string; image: string }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/api/images/build", request);

export const removeImage = (
	config: ApiConfig,
	id: string,
	force = false
): Effect.Effect<RemoveImageResponse, ApiFailure, HttpClient.HttpClient> =>
	requestJson(
		config,
		HttpClientRequest.del(`/api/images/${encodeURIComponent(id)}`, {
			urlParams: force ? { force: "true" } : undefined
		})
	);

export const composeDown = (
	config: ApiConfig,
	request: ComposeRequest
): Effect.Effect<ComposeResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/api/compose/down", request);

export const composeStatus = (
	config: ApiConfig,
	request: ComposeRequest
): Effect.Effect<ComposeStatusResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/api/compose/status", request);

export type StreamEvent = {
	event: string;
	data: string;
};

const parseSseEvent = (block: string): StreamEvent | null => {
	const lines = block.split("\n").map((line) => line.trimEnd()).filter((line) => line.length > 0);
	if (lines.length === 0) {
		return null;
	}

	let event = "message";
	const data: string[] = [];
	for (const line of lines) {
		if (line.startsWith("event:")) {
			event = line.slice(6).trim() || "message";
			continue;
		}
		if (line.startsWith("data:")) {
			data.push(line.slice(5).trimStart());
		}
	}

	return { event, data: data.join("\n") };
};

const streamJsonPost = async (
	config: ApiConfig,
	path: string,
	payload: unknown,
	onEvent?: (event: StreamEvent) => void
): Promise<ComposeResponse> => {
	const headers = new Headers({ "Content-Type": "application/json", Accept: "text/event-stream" });
	const token = config.token?.trim() ?? "";
	if (token.length > 0) {
		headers.set("Authorization", `Bearer ${token}`);
	}

	let response: Response;
	try {
		response = await fetch(resolveApiUrl(config, path), {
			method: "POST",
			credentials: "include",
			headers,
			body: JSON.stringify(payload)
		});
	} catch (error) {
		throw new NetworkError({ message: error instanceof Error ? error.message : "Network request failed.", cause: error });
	}

	if (response.status === 401) {
		if (browser) {
			window.dispatchEvent(new CustomEvent("open-sandbox:auth-error"));
		}
		const payload = await readResponsePayload(response);
		const reason = extractAuthReason(payload);
		throw new AuthError({ message: mapAuthReasonToMessage(reason), reason });
	}

	if (!response.ok || response.body === null) {
		const errorPayload = await readResponsePayload(response);
		throw new ApiError({
			status: response.status,
			message: extractMessage(errorPayload, `Request failed with status ${response.status}`),
			body: errorPayload
		});
	}

	const reader = response.body.getReader();
	const decoder = new TextDecoder();
	let buffer = "";
	const stdout: string[] = [];
	const stderr: string[] = [];

	const consumeSseBuffer = (pending: string): string => {
		const normalized = pending.replace(/\r\n/g, "\n");
		const chunks = normalized.split("\n\n");
		const remainder = chunks.pop() ?? "";
		for (const chunk of chunks) {
			const parsed = parseSseEvent(chunk);
			if (parsed === null) {
				continue;
			}
			onEvent?.(parsed);
			if (parsed.event === "stdout" && parsed.data.length > 0) {
				stdout.push(parsed.data);
			}
			if (parsed.event === "stderr" && parsed.data.length > 0) {
				stderr.push(parsed.data);
			}
		}
		return remainder;
	};

	while (true) {
		const { done, value } = await reader.read();
		if (done) {
			break;
		}
		buffer += decoder.decode(value, { stream: true });
		buffer = consumeSseBuffer(buffer);
	}

	if (buffer.trim().length > 0) {
		const parsed = parseSseEvent(buffer);
		if (parsed !== null) {
			onEvent?.(parsed);
			if (parsed.event === "stdout" && parsed.data.length > 0) {
				stdout.push(parsed.data);
			}
			if (parsed.event === "stderr" && parsed.data.length > 0) {
				stderr.push(parsed.data);
			}
		}
	}

	return { stdout: stdout.join("\n"), stderr: stderr.join("\n") };
};

export const composeUpStream = async (
	config: ApiConfig,
	request: ComposeRequest,
	onEvent?: (event: StreamEvent) => void
): Promise<ComposeResponse> => streamJsonPost(config, "/api/compose/up", request, onEvent);

export const buildImageStream = async (
	config: ApiConfig,
	request: BuildImageRequest,
	onEvent?: (event: StreamEvent) => void
): Promise<ComposeResponse> => streamJsonPost(config, "/api/images/build/stream", request, onEvent);

export const gitClone = (
	config: ApiConfig,
	request: GitCloneRequest
): Effect.Effect<ExecResponse, ApiFailure, HttpClient.HttpClient> => postJson(config, "/api/git/clone", request);

export const createContainer = (
	config: ApiConfig,
	request: CreateContainerRequest
): Effect.Effect<CreateContainerResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/api/containers/create", request);

export const execInContainer = (
	config: ApiConfig,
	containerId: string,
	request: ExecRequest
): Effect.Effect<ExecResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/containers/${encodeURIComponent(containerId)}/exec`, request);

export const listContainers = (
	config: ApiConfig
): Effect.Effect<ContainerSummary[], ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/api/containers"));

export const stopContainer = (
	config: ApiConfig,
	containerId: string
): Effect.Effect<{ container_id: string; stopped: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/containers/${encodeURIComponent(containerId)}/stop`, {});

export const restartContainer = (
	config: ApiConfig,
	containerId: string
): Effect.Effect<{ container_id: string; restarted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/containers/${encodeURIComponent(containerId)}/restart`, {});

export const removeContainer = (
	config: ApiConfig,
	containerId: string,
	force = true
): Effect.Effect<{ container_id: string; removed: boolean }, ApiFailure, HttpClient.HttpClient> =>
	requestJson(
		config,
		HttpClientRequest.del(`/api/containers/${encodeURIComponent(containerId)}`, {
			urlParams: force ? { force: "true" } : undefined
		})
	);

export const createSandbox = (
	config: ApiConfig,
	request: CreateSandboxRequest
): Effect.Effect<Sandbox, ApiFailure, HttpClient.HttpClient> => postJson(config, "/api/sandboxes", request);

export const listSandboxes = (
	config: ApiConfig
): Effect.Effect<Sandbox[], ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/api/sandboxes"));

export const getSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<Sandbox, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get(`/api/sandboxes/${encodeURIComponent(sandboxId)}`));

export const restartSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<{ id: string; restarted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/sandboxes/${encodeURIComponent(sandboxId)}/restart`, {});

export const resetSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<{ id: string; reset: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/sandboxes/${encodeURIComponent(sandboxId)}/reset`, {});

export const stopSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<Sandbox, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/sandboxes/${encodeURIComponent(sandboxId)}/stop`, {});

export const deleteSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<{ id: string; deleted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.del(`/api/sandboxes/${encodeURIComponent(sandboxId)}`));

export const execInSandbox = (
	config: ApiConfig,
	sandboxId: string,
	request: ExecRequest
): Effect.Effect<ExecResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, `/api/sandboxes/${encodeURIComponent(sandboxId)}/exec`, request);

export const readSandboxFile = async (
	config: ApiConfig,
	sandboxId: string,
	filePath: string
): Promise<FileReadResponse> =>
	fetchJson<FileReadResponse>(config, `/api/sandboxes/${encodeURIComponent(sandboxId)}/files?path=${encodeURIComponent(filePath)}`);

export const uploadSandboxFile = async (
	config: ApiConfig,
	sandboxId: string,
	targetPath: string,
	file: File
): Promise<{ id: string; path: string; uploaded: boolean }> => {
	const formData = new FormData();
	formData.set("target_path", targetPath);
	formData.set("file", file, file.name);

	return fetchJson<{ id: string; path: string; uploaded: boolean }>(
		config,
		`/api/sandboxes/${encodeURIComponent(sandboxId)}/files`,
		{
			method: "PUT",
			body: formData
		}
	);
};

export const saveSandboxFile = async (
	config: ApiConfig,
	sandboxId: string,
	targetPath: string,
	content: string
): Promise<{ id: string; path: string; saved: boolean }> =>
	fetchJson<{ id: string; path: string; saved: boolean }>(
		config,
		`/api/sandboxes/${encodeURIComponent(sandboxId)}/files`,
		{
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ target_path: targetPath, content })
		}
	);

export const readContainerFile = async (
	config: ApiConfig,
	containerId: string,
	filePath: string
): Promise<FileReadResponse> =>
	fetchJson<FileReadResponse>(config, `/api/containers/${encodeURIComponent(containerId)}/files?path=${encodeURIComponent(filePath)}`);

export const uploadContainerFile = async (
	config: ApiConfig,
	containerId: string,
	targetPath: string,
	file: File
): Promise<{ container_id: string; path: string; uploaded: boolean }> => {
	const formData = new FormData();
	formData.set("target_path", targetPath);
	formData.set("file", file, file.name);

	return fetchJson<{ container_id: string; path: string; uploaded: boolean }>(
		config,
		`/api/containers/${encodeURIComponent(containerId)}/files`,
		{
			method: "PUT",
			body: formData
		}
	);
};

export const saveContainerFile = async (
	config: ApiConfig,
	containerId: string,
	targetPath: string,
	content: string
): Promise<{ container_id: string; path: string; saved: boolean }> =>
	fetchJson<{ container_id: string; path: string; saved: boolean }>(
		config,
		`/api/containers/${encodeURIComponent(containerId)}/files`,
		{
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ target_path: targetPath, content })
		}
	);
