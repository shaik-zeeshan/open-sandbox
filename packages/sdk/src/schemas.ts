import { Schema } from "effect";

export interface UserSummary {
	id: string;
	username: string;
	role: string;
	created_at: number;
	updated_at: number;
}

export interface CreateUserRequest {
	username: string;
	password: string;
	role?: string;
}

export interface UpdateUserPasswordResponse {
	id: string;
	updated: boolean;
}

export interface ItemDeletedResponse {
	id: string;
	deleted: boolean;
}

export interface APIKeyResponse {
	id: string;
	name?: string;
	preview?: string;
	created_at: number;
	revoked_at?: number;
}

export interface CreateAPIKeyRequest {
	name?: string;
}

export interface CreateAPIKeyResponse {
	api_key: APIKeyResponse;
	secret: string;
}

export interface RevokeAPIKeyResponse {
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

export interface OutputImageResponse {
	output: string;
	image: string;
}

export interface ImageSummary {
	id: string;
	repo_tags: readonly string[];
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
	deleted: ReadonlyArray<{
		readonly Deleted?: string;
		readonly Untagged?: string;
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

export interface ComposeStatusService {
	name: string;
	service: string;
	state: string;
}

export interface ComposeStatusResponse {
	services: readonly ComposeStatusService[];
	raw: string;
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
	ports: readonly ComposePublishedPortEntry[];
}

export interface ComposeProjectPreview {
	project_name: string;
	services: readonly ComposeServicePreview[];
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
	warnings: readonly string[];
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

export interface PreviewUrl {
	private_port: number;
	url: string;
}

export interface PortSummary {
	private: number;
	public?: number;
	type: string;
	ip?: string;
}

export interface ContainerSummary {
	id: string;
	container_id: string;
	worker_id?: string;
	names: readonly string[];
	image: string;
	state: string;
	status: string;
	created: number;
	labels: Record<string, string>;
	workload_kind?: string;
	project_name?: string;
	service_name?: string;
	resettable: boolean;
	port_specs?: readonly string[];
	ports?: readonly PortSummary[];
	preview_urls?: readonly PreviewUrl[];
}

export interface ContainerStoppedResponse {
	id: string;
	container_id: string;
	stopped: boolean;
}

export interface ContainerRestartedResponse {
	id: string;
	container_id: string;
	restarted: boolean;
}

export interface ContainerResetResponse {
	id: string;
	container_id: string;
	reset: boolean;
}

export interface ContainerRemovedResponse {
	id: string;
	container_id: string;
	removed: boolean;
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
	entries?: readonly FileEntry[];
}

export interface FileSavedResponse {
	id: string;
	path: string;
	saved: boolean;
}

export interface ContainerFileSavedResponse {
	id: string;
	container_id: string;
	path: string;
	saved: boolean;
}

export interface FileUploadedResponse {
	id: string;
	path: string;
	uploaded: boolean;
}

export interface ContainerFileUploadedResponse {
	id: string;
	container_id: string;
	path: string;
	uploaded: boolean;
}

export interface SandboxPortCORSConfig {
	allow_origins?: readonly string[];
	allow_methods?: readonly string[];
	allow_headers?: readonly string[];
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
	env?: readonly string[];
	secret_env?: readonly string[];
	cmd?: readonly string[];
	workdir?: string;
	tty?: boolean;
	user?: string;
	ports?: readonly string[];
	proxy_config?: Record<string, SandboxPortProxyConfig>;
}

export interface UpdateSandboxEnvRequest {
	env: readonly string[];
	secret_env?: readonly string[];
	remove_secret_env_keys?: readonly string[];
}

export interface Sandbox {
	id: string;
	name: string;
	image: string;
	container_id: string;
	worker_id?: string;
	workspace_dir: string;
	repo_url?: string;
	env?: readonly string[];
	secret_env_keys?: readonly string[];
	status: string;
	owner_username?: string;
	proxy_config?: Record<string, SandboxPortProxyConfig>;
	port_specs?: readonly string[];
	ports?: readonly PortSummary[];
	preview_urls?: readonly PreviewUrl[];
	created_at: number;
	updated_at: number;
}

export interface SandboxRestartedResponse {
	id: string;
	restarted: boolean;
}

export interface SandboxResetResponse {
	id: string;
	reset: boolean;
}

export interface WorkerResponse {
	id: string;
	name: string;
	advertise_address?: string;
	execution_mode: string;
	status: string;
	version?: string;
	labels?: Record<string, string>;
	registered_at: number;
	last_heartbeat_at: number;
	heartbeat_ttl_seconds: number;
	updated_at: number;
	control_plane_owned: boolean;
	execution_reachable: boolean;
}

export interface TraefikRoutePortDescription {
	private: number;
	public: number;
}

export interface TraefikRouteWorkloadResponse {
	id: string;
	file: string;
	ports: readonly TraefikRoutePortDescription[];
}

export interface TraefikRouteComposeServiceResult {
	name: string;
	ports: readonly TraefikRoutePortDescription[];
}

export interface TraefikRouteComposeProjectInfo {
	project: string;
	file: string;
	services: readonly TraefikRouteComposeServiceResult[];
}

export interface TraefikRouteStateResponse {
	enabled: boolean;
	dynamic_config_dir?: string;
	sandboxes: readonly TraefikRouteWorkloadResponse[];
	containers: readonly TraefikRouteWorkloadResponse[];
	compose_projects: readonly TraefikRouteComposeProjectInfo[];
}

export interface MaintenanceCleanupRequest {
	dry_run?: boolean;
	max_artifact_age?: string;
	max_missing_sandbox_age?: string;
}

export interface MaintenanceCleanupResponse {
	dry_run: boolean;
	artifact_max_age: string;
	missing_sandbox_age: string;
	removed: Record<string, number>;
	errors?: readonly string[];
	checked_at: number;
}

export interface MaintenanceReconcileRequest {
	dry_run?: boolean;
}

export interface MaintenanceReconcileResponse {
	dry_run: boolean;
	removed: Record<string, number>;
	errors?: readonly string[];
	checked_at: number;
}

const StringRecordSchema: Schema.Schema<Record<string, string>> = Schema.Record({
	key: Schema.String,
	value: Schema.String
}) as Schema.Schema<Record<string, string>>;

const NumberRecordSchema: Schema.Schema<Record<string, number>> = Schema.Record({
	key: Schema.String,
	value: Schema.Number
}) as Schema.Schema<Record<string, number>>;

export const UserSummarySchema: Schema.Schema<UserSummary> = Schema.Struct({
	id: Schema.String,
	username: Schema.String,
	role: Schema.String,
	created_at: Schema.Number,
	updated_at: Schema.Number
}) as Schema.Schema<UserSummary>;

export const UpdateUserPasswordResponseSchema: Schema.Schema<UpdateUserPasswordResponse> = Schema.Struct({
	id: Schema.String,
	updated: Schema.Boolean
}) as Schema.Schema<UpdateUserPasswordResponse>;

export const ItemDeletedResponseSchema: Schema.Schema<ItemDeletedResponse> = Schema.Struct({
	id: Schema.String,
	deleted: Schema.Boolean
}) as Schema.Schema<ItemDeletedResponse>;

export const APIKeyResponseSchema: Schema.Schema<APIKeyResponse> = Schema.Struct({
	id: Schema.String,
	name: Schema.optional(Schema.String),
	preview: Schema.optional(Schema.String),
	created_at: Schema.Number,
	revoked_at: Schema.optional(Schema.Number)
}) as Schema.Schema<APIKeyResponse>;

export const CreateAPIKeyResponseSchema: Schema.Schema<CreateAPIKeyResponse> = Schema.Struct({
	api_key: APIKeyResponseSchema,
	secret: Schema.String
}) as Schema.Schema<CreateAPIKeyResponse>;

export const RevokeAPIKeyResponseSchema: Schema.Schema<RevokeAPIKeyResponse> = Schema.Struct({
	id: Schema.String,
	revoked: Schema.Boolean
}) as Schema.Schema<RevokeAPIKeyResponse>;

export const OutputImageResponseSchema: Schema.Schema<OutputImageResponse> = Schema.Struct({
	output: Schema.String,
	image: Schema.String
}) as Schema.Schema<OutputImageResponse>;

export const ImageSummarySchema: Schema.Schema<ImageSummary> = Schema.Struct({
	id: Schema.String,
	repo_tags: Schema.Array(Schema.String),
	created: Schema.Number,
	size: Schema.Number
}) as Schema.Schema<ImageSummary>;

export const ImageSearchResultSchema: Schema.Schema<ImageSearchResult> = Schema.Struct({
	name: Schema.String,
	description: Schema.String,
	stars: Schema.Number,
	official: Schema.Boolean,
	automated: Schema.Boolean
}) as Schema.Schema<ImageSearchResult>;

export const RemoveImageResponseSchema: Schema.Schema<RemoveImageResponse> = Schema.Struct({
	deleted: Schema.Array(
		Schema.Struct({
			Deleted: Schema.optional(Schema.String),
			Untagged: Schema.optional(Schema.String)
		})
	)
}) as Schema.Schema<RemoveImageResponse>;

export const ComposeResponseSchema: Schema.Schema<ComposeResponse> = Schema.Struct({
	stdout: Schema.String,
	stderr: Schema.String
}) as Schema.Schema<ComposeResponse>;

export const ComposeStatusServiceSchema: Schema.Schema<ComposeStatusService> = Schema.Struct({
	name: Schema.String,
	service: Schema.String,
	state: Schema.String
}) as Schema.Schema<ComposeStatusService>;

export const ComposeStatusResponseSchema: Schema.Schema<ComposeStatusResponse> = Schema.Struct({
	services: Schema.Array(ComposeStatusServiceSchema),
	raw: Schema.String
}) as Schema.Schema<ComposeStatusResponse>;

export const ComposePublishedPortEntrySchema: Schema.Schema<ComposePublishedPortEntry> = Schema.Struct({
	private_port: Schema.Number,
	public_port: Schema.Number,
	type: Schema.String,
	ip: Schema.optional(Schema.String),
	preview_url: Schema.String
}) as Schema.Schema<ComposePublishedPortEntry>;

export const ComposeServicePreviewSchema: Schema.Schema<ComposeServicePreview> = Schema.Struct({
	service_name: Schema.String,
	ports: Schema.Array(ComposePublishedPortEntrySchema)
}) as Schema.Schema<ComposeServicePreview>;

export const ComposeProjectPreviewSchema: Schema.Schema<ComposeProjectPreview> = Schema.Struct({
	project_name: Schema.String,
	services: Schema.Array(ComposeServicePreviewSchema)
}) as Schema.Schema<ComposeProjectPreview>;

export const ExecResponseSchema: Schema.Schema<ExecResponse> = Schema.Struct({
	exec_id: Schema.String,
	exit_code: Schema.optional(Schema.Number),
	stdout: Schema.optional(Schema.String),
	stderr: Schema.optional(Schema.String),
	detached: Schema.Boolean
}) as Schema.Schema<ExecResponse>;

export const CreateContainerResponseSchema: Schema.Schema<CreateContainerResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	warnings: Schema.Array(Schema.String),
	started: Schema.Boolean
}) as Schema.Schema<CreateContainerResponse>;

export const PreviewUrlSchema: Schema.Schema<PreviewUrl> = Schema.Struct({
	private_port: Schema.Number,
	url: Schema.String
}) as Schema.Schema<PreviewUrl>;

export const PortSummarySchema: Schema.Schema<PortSummary> = Schema.Struct({
	private: Schema.Number,
	public: Schema.optional(Schema.Number),
	type: Schema.String,
	ip: Schema.optional(Schema.String)
}) as Schema.Schema<PortSummary>;

export const ContainerSummarySchema: Schema.Schema<ContainerSummary> = Schema.Struct({
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
}) as Schema.Schema<ContainerSummary>;

export const ContainerStoppedResponseSchema: Schema.Schema<ContainerStoppedResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	stopped: Schema.Boolean
}) as Schema.Schema<ContainerStoppedResponse>;

export const ContainerRestartedResponseSchema: Schema.Schema<ContainerRestartedResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	restarted: Schema.Boolean
}) as Schema.Schema<ContainerRestartedResponse>;

export const ContainerResetResponseSchema: Schema.Schema<ContainerResetResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	reset: Schema.Boolean
}) as Schema.Schema<ContainerResetResponse>;

export const ContainerRemovedResponseSchema: Schema.Schema<ContainerRemovedResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	removed: Schema.Boolean
}) as Schema.Schema<ContainerRemovedResponse>;

export const FileEntrySchema: Schema.Schema<FileEntry> = Schema.Struct({
	name: Schema.String,
	path: Schema.String,
	kind: Schema.Literal("file", "directory"),
	size: Schema.optional(Schema.Number)
}) as Schema.Schema<FileEntry>;

export const FileReadResponseSchema: Schema.Schema<FileReadResponse> = Schema.Struct({
	path: Schema.String,
	name: Schema.String,
	kind: Schema.Literal("file", "directory"),
	content: Schema.optional(Schema.String),
	entries: Schema.optional(Schema.Array(FileEntrySchema))
}) as Schema.Schema<FileReadResponse>;

export const FileSavedResponseSchema: Schema.Schema<FileSavedResponse> = Schema.Struct({
	id: Schema.String,
	path: Schema.String,
	saved: Schema.Boolean
}) as Schema.Schema<FileSavedResponse>;

export const ContainerFileSavedResponseSchema: Schema.Schema<ContainerFileSavedResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	path: Schema.String,
	saved: Schema.Boolean
}) as Schema.Schema<ContainerFileSavedResponse>;

export const FileUploadedResponseSchema: Schema.Schema<FileUploadedResponse> = Schema.Struct({
	id: Schema.String,
	path: Schema.String,
	uploaded: Schema.Boolean
}) as Schema.Schema<FileUploadedResponse>;

export const ContainerFileUploadedResponseSchema: Schema.Schema<ContainerFileUploadedResponse> = Schema.Struct({
	id: Schema.String,
	container_id: Schema.String,
	path: Schema.String,
	uploaded: Schema.Boolean
}) as Schema.Schema<ContainerFileUploadedResponse>;

export const SandboxPortCORSConfigSchema: Schema.Schema<SandboxPortCORSConfig> = Schema.Struct({
	allow_origins: Schema.optional(Schema.Array(Schema.String)),
	allow_methods: Schema.optional(Schema.Array(Schema.String)),
	allow_headers: Schema.optional(Schema.Array(Schema.String)),
	allow_credentials: Schema.optional(Schema.Boolean),
	max_age: Schema.optional(Schema.Number)
}) as Schema.Schema<SandboxPortCORSConfig>;

export const SandboxPortProxyConfigSchema: Schema.Schema<SandboxPortProxyConfig> = Schema.Struct({
	request_headers: Schema.optional(StringRecordSchema),
	response_headers: Schema.optional(StringRecordSchema),
	cors: Schema.optional(SandboxPortCORSConfigSchema),
	path_prefix_strip: Schema.optional(Schema.String),
	skip_auth: Schema.optional(Schema.Boolean)
}) as Schema.Schema<SandboxPortProxyConfig>;

export const SandboxProxyConfigSchema: Schema.Schema<Record<string, SandboxPortProxyConfig>> = Schema.Record({
	key: Schema.String,
	value: SandboxPortProxyConfigSchema
}) as Schema.Schema<Record<string, SandboxPortProxyConfig>>;

export const SandboxSchema: Schema.Schema<Sandbox> = Schema.Struct({
	id: Schema.String,
	name: Schema.String,
	image: Schema.String,
	container_id: Schema.String,
	worker_id: Schema.optional(Schema.String),
	workspace_dir: Schema.String,
	repo_url: Schema.optional(Schema.String),
	env: Schema.optional(Schema.Array(Schema.String)),
	secret_env_keys: Schema.optional(Schema.Array(Schema.String)),
	status: Schema.String,
	owner_username: Schema.optional(Schema.String),
	proxy_config: Schema.optional(SandboxProxyConfigSchema),
	port_specs: Schema.optional(Schema.Array(Schema.String)),
	ports: Schema.optional(Schema.Array(PortSummarySchema)),
	preview_urls: Schema.optional(Schema.Array(PreviewUrlSchema)),
	created_at: Schema.Number,
	updated_at: Schema.Number
}) as Schema.Schema<Sandbox>;

export const UpdateSandboxEnvRequestSchema: Schema.Schema<UpdateSandboxEnvRequest> = Schema.Struct({
	env: Schema.Array(Schema.String),
	secret_env: Schema.optional(Schema.Array(Schema.String)),
	remove_secret_env_keys: Schema.optional(Schema.Array(Schema.String))
}) as Schema.Schema<UpdateSandboxEnvRequest>;

export const SandboxRestartedResponseSchema: Schema.Schema<SandboxRestartedResponse> = Schema.Struct({
	id: Schema.String,
	restarted: Schema.Boolean
}) as Schema.Schema<SandboxRestartedResponse>;

export const SandboxResetResponseSchema: Schema.Schema<SandboxResetResponse> = Schema.Struct({
	id: Schema.String,
	reset: Schema.Boolean
}) as Schema.Schema<SandboxResetResponse>;

export const WorkerResponseSchema: Schema.Schema<WorkerResponse> = Schema.Struct({
	id: Schema.String,
	name: Schema.String,
	advertise_address: Schema.optional(Schema.String),
	execution_mode: Schema.String,
	status: Schema.String,
	version: Schema.optional(Schema.String),
	labels: Schema.optional(StringRecordSchema),
	registered_at: Schema.Number,
	last_heartbeat_at: Schema.Number,
	heartbeat_ttl_seconds: Schema.Number,
	updated_at: Schema.Number,
	control_plane_owned: Schema.Boolean,
	execution_reachable: Schema.Boolean
}) as Schema.Schema<WorkerResponse>;

export const TraefikRoutePortDescriptionSchema: Schema.Schema<TraefikRoutePortDescription> = Schema.Struct({
	private: Schema.Number,
	public: Schema.Number
}) as Schema.Schema<TraefikRoutePortDescription>;

export const TraefikRouteWorkloadResponseSchema: Schema.Schema<TraefikRouteWorkloadResponse> = Schema.Struct({
	id: Schema.String,
	file: Schema.String,
	ports: Schema.Array(TraefikRoutePortDescriptionSchema)
}) as Schema.Schema<TraefikRouteWorkloadResponse>;

export const TraefikRouteComposeServiceResultSchema: Schema.Schema<TraefikRouteComposeServiceResult> = Schema.Struct({
	name: Schema.String,
	ports: Schema.Array(TraefikRoutePortDescriptionSchema)
}) as Schema.Schema<TraefikRouteComposeServiceResult>;

export const TraefikRouteComposeProjectInfoSchema: Schema.Schema<TraefikRouteComposeProjectInfo> = Schema.Struct({
	project: Schema.String,
	file: Schema.String,
	services: Schema.Array(TraefikRouteComposeServiceResultSchema)
}) as Schema.Schema<TraefikRouteComposeProjectInfo>;

export const TraefikRouteStateResponseSchema: Schema.Schema<TraefikRouteStateResponse> = Schema.Struct({
	enabled: Schema.Boolean,
	dynamic_config_dir: Schema.optional(Schema.String),
	sandboxes: Schema.Array(TraefikRouteWorkloadResponseSchema),
	containers: Schema.Array(TraefikRouteWorkloadResponseSchema),
	compose_projects: Schema.Array(TraefikRouteComposeProjectInfoSchema)
}) as Schema.Schema<TraefikRouteStateResponse>;

export const MaintenanceCleanupResponseSchema: Schema.Schema<MaintenanceCleanupResponse> = Schema.Struct({
	dry_run: Schema.Boolean,
	artifact_max_age: Schema.String,
	missing_sandbox_age: Schema.String,
	removed: NumberRecordSchema,
	errors: Schema.optional(Schema.Array(Schema.String)),
	checked_at: Schema.Number
}) as Schema.Schema<MaintenanceCleanupResponse>;

export const MaintenanceReconcileResponseSchema: Schema.Schema<MaintenanceReconcileResponse> = Schema.Struct({
	dry_run: Schema.Boolean,
	removed: NumberRecordSchema,
	errors: Schema.optional(Schema.Array(Schema.String)),
	checked_at: Schema.Number
}) as Schema.Schema<MaintenanceReconcileResponse>;
