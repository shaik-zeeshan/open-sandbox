import { browser } from "$app/environment";
import { FetchHttpClient, HttpClient, HttpClientRequest } from "@effect/platform";
import type { HttpClientResponse } from "@effect/platform";
import {
	Api as SdkApi,
	ApiError,
	AuthError,
	bearerAuth,
	DEFAULT_BASE_URL as SDK_DEFAULT_BASE_URL,
	extractAuthReason,
	extractMessage,
	formatSdkError,
	mapAuthReasonToMessage,
	NetworkError,
	normalizeBaseUrl as sdkNormalizeBaseUrl,
	resolveApiUrl as sdkResolveApiUrl,
	resolveWebSocketUrl as sdkResolveWebSocketUrl,
	SdkAuthLayer,
	SdkConfigLayer,
	type SdkTransportEnv
} from "@open-sandbox/sdk";
import { dispatchAuthErrorEvent } from "$lib/client/browser";
import { clientRuntime } from "$lib/client/runtime";
import { Effect, Layer, pipe, Schema } from "effect";

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

export interface ApiKeySummary {
	id: string;
	name?: string;
	preview?: string;
	created_at: number;
	revoked_at?: number;
}

export interface CreateApiKeyRequest {
	name: string;
}

export interface CreateApiKeyResponse {
	api_key: ApiKeySummary;
	secret: string;
}

export interface RevokeApiKeyResponse {
	id: string;
	revoked: boolean;
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
	services: ComposeStatusService[];
	raw: string;
}

export interface ComposeStatusService {
	name: string;
	service: string;
	state: string;
}

export interface PreviewUrl {
	private_port: number;
	url: string;
}

export interface ComposePublishedPortEntry {
	private_port: number;
	public_port: number;
	type: string;
	ip?: string;
	preview_url: string;
}

export interface ComposeServicePreview {
	service_name: string;
	ports: ComposePublishedPortEntry[];
}

export interface ComposeProjectPreview {
	project_name: string;
	services: ComposeServicePreview[];
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
	id: string;
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
	container_id: string;
	names: string[];
	image: string;
	state: string;
	status: string;
	created: number;
	labels: Record<string, string>;
	workload_kind?: string;
	project_name?: string;
	service_name?: string;
	resettable: boolean;
	port_specs?: string[];
	ports?: PortSummary[];
	preview_urls?: PreviewUrl[];
}

export interface PortSummary {
	private: number;
	public?: number;
	type: string;
	ip?: string;
}

export interface SandboxPortCORSConfig {
	allow_origins?: string[];
	allow_methods?: string[];
	allow_headers?: string[];
	allow_credentials?: boolean;
	max_age?: number;
}

export interface SandboxPortProxyConfig {
	request_headers?: Record<string, string>;
	response_headers?: Record<string, string>;
	cors?: SandboxPortCORSConfig;
	path_prefix_strip?: string;
	skip_auth?: boolean;
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
	proxy_config?: Record<string, SandboxPortProxyConfig>;
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
	proxy_config?: Record<string, SandboxPortProxyConfig>;
	port_specs?: string[];
	ports?: PortSummary[];
	preview_urls?: PreviewUrl[];
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

const LoginResponseSchema = Schema.Struct({
	token: Schema.String,
	token_type: Schema.String,
	user_id: Schema.String,
	username: Schema.String,
	role: Schema.String,
	expires_at: Schema.Number
});

const SessionResponseSchema = Schema.Struct({
	authenticated: Schema.Boolean,
	user_id: Schema.String,
	username: Schema.String,
	role: Schema.String,
	expires_at: Schema.Number
});

const SetupStatusResponseSchema = Schema.Struct({
	bootstrap_required: Schema.Boolean
});

const UserSummarySchema: Schema.Schema<UserSummary> = Schema.Struct({
	id: Schema.String,
	username: Schema.String,
	role: Schema.String,
	created_at: Schema.Number,
	updated_at: Schema.Number
}) as Schema.Schema<UserSummary>;

const ApiKeySummarySchema: Schema.Schema<ApiKeySummary> = Schema.Struct({
	id: Schema.String,
	name: Schema.optional(Schema.String),
	preview: Schema.optional(Schema.String),
	created_at: Schema.Number,
	revoked_at: Schema.optional(Schema.Number)
}) as Schema.Schema<ApiKeySummary>;

const CreateApiKeyResponseSchema: Schema.Schema<CreateApiKeyResponse> = Schema.Struct({
	api_key: ApiKeySummarySchema,
	secret: Schema.String
}) as Schema.Schema<CreateApiKeyResponse>;

const RevokeApiKeyResponseSchema: Schema.Schema<RevokeApiKeyResponse> = Schema.Struct({
	id: Schema.String,
	revoked: Schema.Boolean
}) as Schema.Schema<RevokeApiKeyResponse>;

const HealthStatusResponseSchema = Schema.Struct({ status: Schema.String });

const ImageSummarySchema: Schema.Schema<ImageSummary> = Schema.Struct({
	id: Schema.String,
	repo_tags: Schema.Array(Schema.String),
	created: Schema.Number,
	size: Schema.Number
}) as unknown as Schema.Schema<ImageSummary>;

const ImageSearchResultSchema: Schema.Schema<ImageSearchResult> = Schema.Struct({
	name: Schema.String,
	description: Schema.String,
	stars: Schema.Number,
	official: Schema.Boolean,
	automated: Schema.Boolean
}) as Schema.Schema<ImageSearchResult>;

const RemoveImageResponseSchema: Schema.Schema<RemoveImageResponse> = Schema.Struct({
	deleted: Schema.Array(
		Schema.Struct({
			Deleted: Schema.optional(Schema.String),
			Untagged: Schema.optional(Schema.String)
		})
	)
}) as unknown as Schema.Schema<RemoveImageResponse>;

const ComposeResponseSchema = Schema.Struct({
	stdout: Schema.String,
	stderr: Schema.String
});

const ComposeStatusServiceSchema: Schema.Schema<ComposeStatusService> = Schema.Struct({
	name: Schema.String,
	service: Schema.String,
	state: Schema.String
}) as Schema.Schema<ComposeStatusService>;

const ComposeStatusResponseSchema: Schema.Schema<ComposeStatusResponse> = Schema.Struct({
	services: Schema.Array(ComposeStatusServiceSchema),
	raw: Schema.String
}) as unknown as Schema.Schema<ComposeStatusResponse>;

const PreviewUrlSchema: Schema.Schema<PreviewUrl> = Schema.Struct({
	private_port: Schema.Number,
	url: Schema.String
}) as Schema.Schema<PreviewUrl>;

const ComposePublishedPortEntrySchema: Schema.Schema<ComposePublishedPortEntry> = Schema.Struct({
	private_port: Schema.Number,
	public_port: Schema.Number,
	type: Schema.String,
	ip: Schema.optional(Schema.String),
	preview_url: Schema.String
}) as Schema.Schema<ComposePublishedPortEntry>;

const ComposeServicePreviewSchema: Schema.Schema<ComposeServicePreview> = Schema.Struct({
	service_name: Schema.String,
	ports: Schema.Array(ComposePublishedPortEntrySchema)
}) as unknown as Schema.Schema<ComposeServicePreview>;

const ComposeProjectPreviewSchema: Schema.Schema<ComposeProjectPreview> = Schema.Struct({
	project_name: Schema.String,
	services: Schema.Array(ComposeServicePreviewSchema)
}) as unknown as Schema.Schema<ComposeProjectPreview>;

const CreateContainerResponseSchema: Schema.Schema<CreateContainerResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	warnings: Schema.Array(Schema.String),
	started: Schema.Boolean
}) as unknown as Schema.Schema<CreateContainerResponse>;

const PortSummarySchema: Schema.Schema<PortSummary> = Schema.Struct({
	private: Schema.Number,
	public: Schema.optional(Schema.Number),
	type: Schema.String,
	ip: Schema.optional(Schema.String)
}) as Schema.Schema<PortSummary>;

const StringRecordSchema: Schema.Schema<Record<string, string>> = Schema.Record({
	key: Schema.String,
	value: Schema.String
}) as unknown as Schema.Schema<Record<string, string>>;

const ContainerSummarySchema: Schema.Schema<ContainerSummary> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	worker_id: Schema.optional(Schema.String),
	names: Schema.Array(Schema.String),
	image: Schema.String,
	state: Schema.String,
	status: Schema.String,
	created: Schema.Number,
	labels: StringRecordSchema,
	workload_kind: Schema.optional(Schema.String),
	project_name: Schema.optional(Schema.String),
	service_name: Schema.optional(Schema.String),
	resettable: Schema.Boolean,
	port_specs: Schema.optional(Schema.Array(Schema.String)),
	ports: Schema.optional(Schema.Array(PortSummarySchema)),
	preview_urls: Schema.optional(Schema.Array(PreviewUrlSchema))
}) as unknown as Schema.Schema<ContainerSummary>;

const SandboxPortCORSConfigSchema: Schema.Schema<SandboxPortCORSConfig> = Schema.Struct({
	allow_origins: Schema.optional(Schema.Array(Schema.String)),
	allow_methods: Schema.optional(Schema.Array(Schema.String)),
	allow_headers: Schema.optional(Schema.Array(Schema.String)),
	allow_credentials: Schema.optional(Schema.Boolean),
	max_age: Schema.optional(Schema.Number)
}) as Schema.Schema<SandboxPortCORSConfig>;

const SandboxPortProxyConfigSchema: Schema.Schema<SandboxPortProxyConfig> = Schema.Struct({
	request_headers: Schema.optional(StringRecordSchema),
	response_headers: Schema.optional(StringRecordSchema),
	cors: Schema.optional(SandboxPortCORSConfigSchema),
	path_prefix_strip: Schema.optional(Schema.String),
	skip_auth: Schema.optional(Schema.Boolean)
}) as Schema.Schema<SandboxPortProxyConfig>;

const SandboxProxyConfigSchema: Schema.Schema<Record<string, SandboxPortProxyConfig>> = Schema.Record({
	key: Schema.String,
	value: SandboxPortProxyConfigSchema
}) as unknown as Schema.Schema<Record<string, SandboxPortProxyConfig>>;

const ExecResponseSchema = Schema.Struct({
	exec_id: Schema.String,
	exit_code: Schema.optional(Schema.Number),
	stdout: Schema.optional(Schema.String),
	stderr: Schema.optional(Schema.String),
	detached: Schema.Boolean
});

const SandboxSchema: Schema.Schema<Sandbox> = Schema.Struct({
	id: Schema.String,
	name: Schema.String,
	image: Schema.String,
	container_id: Schema.String,
	workspace_dir: Schema.String,
	repo_url: Schema.optional(Schema.String),
	status: Schema.String,
	owner_username: Schema.optional(Schema.String),
	proxy_config: Schema.optional(SandboxProxyConfigSchema),
	port_specs: Schema.optional(Schema.Array(Schema.String)),
	ports: Schema.optional(Schema.Array(PortSummarySchema)),
	preview_urls: Schema.optional(Schema.Array(PreviewUrlSchema)),
	created_at: Schema.Number,
	updated_at: Schema.Number
}) as Schema.Schema<Sandbox>;

const FileEntrySchema: Schema.Schema<FileEntry> = Schema.Struct({
	name: Schema.String,
	path: Schema.String,
	kind: Schema.Literal("file", "directory"),
	size: Schema.optional(Schema.Number)
}) as Schema.Schema<FileEntry>;

const FileReadResponseSchema: Schema.Schema<FileReadResponse> = Schema.Struct({
	path: Schema.String,
	name: Schema.String,
	kind: Schema.Literal("file", "directory"),
	content: Schema.optional(Schema.String),
	entries: Schema.optional(Schema.Array(FileEntrySchema))
}) as Schema.Schema<FileReadResponse>;

const SignedOutResponseSchema = Schema.Struct({ signed_out: Schema.Boolean });
const ItemDeletedResponseSchema = Schema.Struct({ id: Schema.String, deleted: Schema.Boolean });
const ItemRestartedResponseSchema = Schema.Struct({ id: Schema.String, restarted: Schema.Boolean });
const ItemResetResponseSchema = Schema.Struct({ id: Schema.String, reset: Schema.Boolean });
const ItemSavedResponseSchema = Schema.Struct({ id: Schema.String, path: Schema.String, saved: Schema.Boolean });
const ItemUploadedResponseSchema = Schema.Struct({ id: Schema.String, path: Schema.String, uploaded: Schema.Boolean });
const ContainerUploadedResponseSchema = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	path: Schema.String,
	uploaded: Schema.Boolean
});
const ContainerStoppedResponseSchema = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	stopped: Schema.Boolean
});
const ContainerRestartedResponseSchema = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	restarted: Schema.Boolean
});
const ContainerResetResponseSchema = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	reset: Schema.Boolean
});
const ContainerRemovedResponseSchema = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	removed: Schema.Boolean
});
const UserPasswordUpdateResponseSchema = Schema.Struct({ id: Schema.String, updated: Schema.Boolean });
const OutputImageResponseSchema = Schema.Struct({ output: Schema.String, image: Schema.String });

const DEFAULT_BASE_URL = SDK_DEFAULT_BASE_URL;

export { ApiError, AuthError, NetworkError };

export type ApiFailure = ApiError | NetworkError | AuthError;

export const normalizeBaseUrl = (baseUrl: string): string => sdkNormalizeBaseUrl(baseUrl);

const normalizePath = (path: string): string => (path.startsWith("/") ? path : `/${path}`);

const sdkAuthForConfig = (config: ApiConfig) => {
	const token = config.token?.trim() ?? "";
	return token.length > 0 ? bearerAuth(token) : { _tag: "session" as const };
};

const toApiFailure = (error: unknown): ApiFailure => {
	if (
		typeof error === "object" &&
		error !== null &&
		("_tag" in error) &&
		(error._tag === "ApiError" || error._tag === "AuthError" || error._tag === "NetworkError")
	) {
		return error as ApiFailure;
	}

	return new ApiError({
		status: 200,
		message: "Response payload did not match expected shape.",
		body: error
	});
};

const sdkRequest = <A, B = A>(
	config: ApiConfig,
	effect: Effect.Effect<A, unknown, SdkTransportEnv>
): Effect.Effect<B, ApiFailure> =>
	Effect.provide(
		Effect.mapError(effect, toApiFailure),
		Layer.mergeAll(FetchHttpClient.layer, SdkConfigLayer({ baseUrl: config.baseUrl }), SdkAuthLayer(sdkAuthForConfig(config)))
	) as unknown as Effect.Effect<B, ApiFailure>;

function decodeResponsePayload(status: number, payload: unknown): Effect.Effect<unknown, never>;
function decodeResponsePayload<A, I = A>(
	status: number,
	payload: unknown,
	schema: Schema.Schema<A, I>
): Effect.Effect<A, ApiFailure>;
function decodeResponsePayload<A, I = A>(
	status: number,
	payload: unknown,
	schema?: Schema.Schema<A, I>
): Effect.Effect<unknown, ApiFailure> {
	if (schema === undefined) {
		return Effect.succeed(payload);
	}

	return pipe(
		Schema.decodeUnknown(schema)(payload),
		Effect.mapError(
			(error) =>
				new ApiError({
					status,
					message: "Response payload did not match expected shape.",
					body: error
				})
		)
	);
}

const readResponsePayload = (response: Response): Effect.Effect<unknown, never> =>
	pipe(
		Effect.tryPromise({
			try: () => response.text(),
			catch: () => undefined
		}),
		Effect.flatMap((text) => {
			if (text.trim().length === 0) {
				return Effect.succeed<unknown>("");
			}

			return pipe(
				Effect.try({
					try: () => JSON.parse(text) as unknown,
					catch: () => undefined
				}),
				Effect.orElseSucceed(() => text)
			);
		}),
		Effect.orElseSucceed(() => "")
	);

const readClientPayload = (response: HttpClientResponse.HttpClientResponse): Effect.Effect<unknown, never> =>
	pipe(response.json, Effect.orElse(() => response.text), Effect.orElseSucceed(() => ""));

const fetchJson = <A, I = A>(
	config: ApiConfig,
	path: string,
	schema: Schema.Schema<A, I>,
	init?: RequestInit
): Effect.Effect<A, ApiFailure> =>
	Effect.gen(function* () {
		const headers = new Headers(init?.headers);
		const token = config.token?.trim() ?? "";
		if (token.length > 0) {
			headers.set("Authorization", `Bearer ${token}`);
		}

		const response = yield* pipe(
			Effect.tryPromise({
				try: () =>
					fetch(resolveApiUrl(config, path), {
						...init,
						credentials: "include",
						headers
					}),
				catch: (error) =>
					new NetworkError({
						message: error instanceof Error ? error.message : "Network request failed.",
						cause: error
					})
			}),
			Effect.mapError((error) => error as ApiFailure)
		);

		const payload = yield* readResponsePayload(response);

		if (response.status === 401) {
			const reason = extractAuthReason(payload);
			return yield* Effect.fail(new AuthError({ message: mapAuthReasonToMessage(reason), reason }));
		}

		if (!response.ok) {
			return yield* Effect.fail(
				new ApiError({
					status: response.status,
					message: extractMessage(payload, `Request failed with status ${response.status}`),
					body: payload
				})
			);
		}

		return yield* decodeResponsePayload(response.status, payload, schema);
	});

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

const requestJson = <A, I = A>(
	config: ApiConfig,
	request: HttpClientRequest.HttpClientRequest,
	schema: Schema.Schema<A, I>
): Effect.Effect<A, ApiFailure, HttpClient.HttpClient> =>
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

		const payload = yield* readClientPayload(response);

		if (response.status === 401) {
			const reason = extractAuthReason(payload);
			return yield* Effect.fail(new AuthError({ message: mapAuthReasonToMessage(reason), reason }));
		}

		if (response.status < 200 || response.status >= 300) {
			return yield* Effect.fail(
				new ApiError({
					status: response.status,
					message: extractMessage(payload, `Request failed with status ${response.status}`),
					body: payload
				})
			);
		}

		return yield* decodeResponsePayload(response.status, payload, schema);
	});

const postJson = <TPayload, A, I = A>(
	config: ApiConfig,
	path: string,
	payload: TPayload,
	schema: Schema.Schema<A, I>
): Effect.Effect<A, ApiFailure, HttpClient.HttpClient> => {
	const request = pipe(HttpClientRequest.post(path), HttpClientRequest.bodyUnsafeJson(payload));
	return requestJson(config, request, schema);
};

export const runApiEffect = async <A>(
	effect: Effect.Effect<A, ApiFailure, HttpClient.HttpClient>,
	options?: { notifyAuthError?: boolean }
): Promise<A> => {
	try {
		return await clientRuntime.runPromise(effect);
	} catch (error) {
		const notifyAuthError = options?.notifyAuthError ?? true;
		const taggedError = typeof error === "object" && error !== null ? (error as { _tag?: string }) : null;
		if (
			notifyAuthError &&
			browser &&
			taggedError !== null &&
			taggedError._tag === "AuthError"
		) {
			dispatchAuthErrorEvent();
		}

		throw error;
	}
};

export const login = (
	config: ApiConfig,
	username: string,
	password: string
): Effect.Effect<LoginResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/login", { username, password }, LoginResponseSchema);

export const getSetupStatus = (
	config: ApiConfig
): Effect.Effect<SetupStatusResponse, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/auth/setup"), SetupStatusResponseSchema);

export const bootstrap = (
	config: ApiConfig,
	username: string,
	password: string
): Effect.Effect<LoginResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/bootstrap", { username, password }, LoginResponseSchema);

export const getSession = (
	config: ApiConfig
): Effect.Effect<SessionResponse, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/auth/session"), SessionResponseSchema);

export const logout = (
	config: ApiConfig
): Effect.Effect<{ signed_out: boolean }, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/logout", {}, SignedOutResponseSchema);

export const refreshSession = (
	config: ApiConfig
): Effect.Effect<LoginResponse, ApiFailure, HttpClient.HttpClient> =>
	postJson(config, "/auth/refresh", {}, LoginResponseSchema);

export const listUsers = (
	config: ApiConfig
): Effect.Effect<UserSummary[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.listUsers());

export const createUser = (
	config: ApiConfig,
	request: { username: string; password: string; role?: string }
): Effect.Effect<UserSummary, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.createUser(request));

export const updateUserPassword = (
	config: ApiConfig,
	userId: string,
	password: string
): Effect.Effect<{ id: string; updated: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.updateUserPassword(userId, password));

export const deleteUser = (
	config: ApiConfig,
	userId: string
): Effect.Effect<{ id: string; deleted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.deleteUser(userId));

export const listApiKeys = (
	config: ApiConfig
): Effect.Effect<ApiKeySummary[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.listApiKeys());

export const createApiKey = (
	config: ApiConfig,
	request: CreateApiKeyRequest
): Effect.Effect<CreateApiKeyResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.createApiKey(request));

export const revokeApiKey = (
	config: ApiConfig,
	apiKeyId: string
): Effect.Effect<RevokeApiKeyResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.revokeApiKey(apiKeyId));

export const resolveApiUrl = (
	config: ApiConfig,
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): string => sdkResolveApiUrl({ baseUrl: config.baseUrl }, path, query);

export const resolveSandboxLogsSseUrl = (
	sandboxId: string,
	query?: { follow?: boolean; tail?: string | number }
) => SdkApi.resolveSandboxLogsSseUrl(sandboxId, query);

export const resolveContainerLogsSseUrl = (
	containerId: string,
	query?: { follow?: boolean; tail?: string | number }
) => SdkApi.resolveContainerLogsSseUrl(containerId, query);

export const resolveWebSocketUrl = (
	config: ApiConfig,
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): string => sdkResolveWebSocketUrl({ baseUrl: config.baseUrl }, path, query);

export const formatApiFailure = (error: unknown): string => formatSdkError(error);

export const healthCheck = (config: ApiConfig): Effect.Effect<{ status: string }, ApiFailure, HttpClient.HttpClient> =>
	requestJson(config, HttpClientRequest.get("/health"), HealthStatusResponseSchema);

export const listImages = (
	config: ApiConfig
): Effect.Effect<ImageSummary[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.listImages());

export const searchImages = (
	config: ApiConfig,
	query: string,
	limit = 25
): Effect.Effect<ImageSearchResult[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.searchImages(query, limit));

export const pullImage = (
	config: ApiConfig,
	request: PullImageRequest
): Effect.Effect<{ output: string; image: string }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.pullImage(request));

export const buildImage = (
	config: ApiConfig,
	request: BuildImageRequest
): Effect.Effect<{ output: string; image: string }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.buildImage(request));

export const removeImage = (
	config: ApiConfig,
	id: string,
	force = false
): Effect.Effect<RemoveImageResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.removeImage(id, force));

export const composeDown = (
	config: ApiConfig,
	request: ComposeRequest
): Effect.Effect<ComposeResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.composeDown(request));

export const composeStatus = (
	config: ApiConfig,
	request: ComposeRequest
): Effect.Effect<ComposeStatusResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.composeStatus(request));

export const listComposeProjects = (
	config: ApiConfig
): Effect.Effect<ComposeProjectPreview[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.listComposeProjects());

export const getComposeProject = (
	config: ApiConfig,
	projectName: string
): Effect.Effect<ComposeProjectPreview, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.getComposeProject(projectName));

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

const streamJsonPost = (
	config: ApiConfig,
	path: string,
	payload: unknown,
	onEvent?: (event: StreamEvent) => void
): Effect.Effect<ComposeResponse, ApiFailure> =>
	Effect.gen(function* () {
		const headers = new Headers({
			"Content-Type": "application/json",
			Accept: "text/event-stream",
			"Accept-Encoding": "identity"
		});
		const token = config.token?.trim() ?? "";
		if (token.length > 0) {
			headers.set("Authorization", `Bearer ${token}`);
		}

		const response = yield* pipe(
			Effect.tryPromise({
				try: () =>
					fetch(resolveApiUrl(config, path), {
						method: "POST",
						credentials: "include",
						headers,
						body: JSON.stringify(payload)
					}),
				catch: (error) =>
					new NetworkError({
						message: error instanceof Error ? error.message : "Network request failed.",
						cause: error
					})
			}),
			Effect.mapError((error) => error as ApiFailure)
		);

		if (response.status === 401) {
			const errorPayload = yield* readResponsePayload(response);
			const reason = extractAuthReason(errorPayload);
			return yield* Effect.fail(new AuthError({ message: mapAuthReasonToMessage(reason), reason }));
		}

		if (!response.ok || response.body === null) {
			const errorPayload = yield* readResponsePayload(response);
			return yield* Effect.fail(
				new ApiError({
					status: response.status,
					message: extractMessage(errorPayload, `Request failed with status ${response.status}`),
					body: errorPayload
				})
			);
		}

		const streamResult = yield* pipe(
			Effect.tryPromise({
				try: async () => {
					const reader = response.body?.getReader();
					if (!reader) {
						throw new Error("Response body is not readable.");
					}

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
				},
				catch: (error) =>
					new NetworkError({
						message: error instanceof Error ? error.message : "Unable to read stream response.",
						cause: error
					})
			}),
			Effect.mapError((error) => error as ApiFailure)
		);

		return yield* decodeResponsePayload(response.status, streamResult, ComposeResponseSchema);
	});

export const composeUpStream = (
	config: ApiConfig,
	request: ComposeRequest,
	onEvent?: (event: StreamEvent) => void
): Effect.Effect<ComposeResponse, ApiFailure> => sdkRequest(config, SdkApi.composeUpStream(request, onEvent));

export const buildImageStream = (
	config: ApiConfig,
	request: BuildImageRequest,
	onEvent?: (event: StreamEvent) => void
): Effect.Effect<ComposeResponse, ApiFailure> => sdkRequest(config, SdkApi.buildImageStream(request, onEvent));

export const gitClone = (
	config: ApiConfig,
	request: GitCloneRequest
): Effect.Effect<ExecResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.gitClone(request));

export const createContainer = (
	config: ApiConfig,
	request: CreateContainerRequest
): Effect.Effect<CreateContainerResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.createContainer(request));

export const execInContainer = (
	config: ApiConfig,
	containerId: string,
	request: ExecRequest
): Effect.Effect<ExecResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.execInContainer(containerId, request));

export const listContainers = (
	config: ApiConfig
): Effect.Effect<ContainerSummary[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.listContainers());

export const stopContainer = (
	config: ApiConfig,
	workloadId: string
): Effect.Effect<{ id: string; container_id: string; stopped: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.stopContainer(workloadId));

export const restartContainer = (
	config: ApiConfig,
	workloadId: string
): Effect.Effect<{ id: string; container_id: string; restarted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.restartContainer(workloadId));

export const resetContainer = (
	config: ApiConfig,
	workloadId: string
): Effect.Effect<{ id: string; container_id: string; reset: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.resetContainer(workloadId));

export const removeContainer = (
	config: ApiConfig,
	workloadId: string,
	force = true
): Effect.Effect<{ id: string; container_id: string; removed: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.removeContainer(workloadId, force));

export const createSandbox = (
	config: ApiConfig,
	request: CreateSandboxRequest
): Effect.Effect<Sandbox, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.createSandbox(request));

export const listSandboxes = (
	config: ApiConfig
): Effect.Effect<Sandbox[], ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.listSandboxes());

export const getSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<Sandbox, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.getSandbox(sandboxId));

export const restartSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<{ id: string; restarted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.restartSandbox(sandboxId));

export const resetSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<{ id: string; reset: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.resetSandbox(sandboxId));

export const stopSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<Sandbox, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.stopSandbox(sandboxId));

export const updateSandboxProxyConfig = (
	config: ApiConfig,
	sandboxId: string,
	proxyConfig: Record<string, SandboxPortProxyConfig>
): Effect.Effect<Sandbox, ApiFailure> =>
	sdkRequest(config, SdkApi.updateSandboxProxyConfig(sandboxId, proxyConfig));

export const deleteSandbox = (
	config: ApiConfig,
	sandboxId: string
): Effect.Effect<{ id: string; deleted: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.deleteSandbox(sandboxId));

export const execInSandbox = (
	config: ApiConfig,
	sandboxId: string,
	request: ExecRequest
): Effect.Effect<ExecResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.execInSandbox(sandboxId, request));

export const readSandboxFile = (
	config: ApiConfig,
	sandboxId: string,
	filePath: string
): Effect.Effect<FileReadResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.readSandboxFile(sandboxId, filePath));

export const uploadSandboxFile = (
	config: ApiConfig,
	sandboxId: string,
	targetPath: string,
	file: File
): Effect.Effect<{ id: string; path: string; uploaded: boolean }, ApiFailure> =>
	sdkRequest(config, SdkApi.uploadSandboxFile(sandboxId, targetPath, file, file.name));

export const saveSandboxFile = (
	config: ApiConfig,
	sandboxId: string,
	targetPath: string,
	content: string
): Effect.Effect<{ id: string; path: string; saved: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.saveSandboxFile(sandboxId, targetPath, content));

export const readContainerFile = (
	config: ApiConfig,
	containerId: string,
	filePath: string
): Effect.Effect<FileReadResponse, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.readContainerFile(containerId, filePath));

export const uploadContainerFile = (
	config: ApiConfig,
	containerId: string,
	targetPath: string,
	file: File
): Effect.Effect<{ id: string; container_id: string; path: string; uploaded: boolean }, ApiFailure> =>
	sdkRequest(config, SdkApi.uploadContainerFile(containerId, targetPath, file, file.name));

export const saveContainerFile = (
	config: ApiConfig,
	containerId: string,
	targetPath: string,
	content: string
): Effect.Effect<{ id: string; container_id: string; path: string; saved: boolean }, ApiFailure, HttpClient.HttpClient> =>
	sdkRequest(config, SdkApi.saveContainerFile(containerId, targetPath, content));
