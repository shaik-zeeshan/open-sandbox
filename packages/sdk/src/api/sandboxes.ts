import { Effect, Schema } from "effect";

import { deleteRequest, getRequest, patchJsonRequest, postJsonRequest, putJsonRequest } from "../request";
import {
	type CreateSandboxRequest,
	type ExecRequest,
	type SandboxOperationProgress,
	type SandboxPortProxyConfig,
	type SandboxStreamDone,
	type UpdateSandboxEnvRequest,
	ExecResponseSchema,
	FileReadResponseSchema,
	FileSavedResponseSchema,
	FileUploadedResponseSchema,
	ItemDeletedResponseSchema,
	SandboxOperationProgressSchema,
	SandboxResetResponseSchema,
	SandboxRestartedResponseSchema,
	SandboxSchema,
	SandboxStreamDoneSchema
} from "../schemas";
import { type JsonResultStreamEvent, collectJsonResultSse } from "../stream";
import { execute, executeFetch, openStream } from "../transport";

export interface SandboxLogsStreamQuery {
	follow?: boolean;
	tail?: string | number;
}

export interface SandboxTerminalWebSocketQuery {
	cols?: number;
	rows?: number;
}

export type SandboxCreateStreamEvent = JsonResultStreamEvent<SandboxOperationProgress, CreateSandboxResponse, SandboxStreamDone>;
export type SandboxResetStreamEvent = JsonResultStreamEvent<SandboxOperationProgress, SandboxResetResponse, SandboxStreamDone>;

type CreateSandboxResponse = Schema.Schema.Type<typeof SandboxSchema>;
type SandboxResetResponse = Schema.Schema.Type<typeof SandboxResetResponseSchema>;

export const createSandbox = (request: CreateSandboxRequest) =>
	execute(postJsonRequest("/api/sandboxes", request), SandboxSchema);

export const createSandboxStream = (
	request: CreateSandboxRequest,
	onEvent?: (event: SandboxCreateStreamEvent) => void
) =>
	Effect.flatMap(
		openStream("/api/sandboxes/stream", {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Accept: "text/event-stream",
				"Accept-Encoding": "identity"
			},
			body: JSON.stringify(request)
		}),
		(response) =>
			collectJsonResultSse(response, {
				progressSchema: SandboxOperationProgressSchema,
				resultSchema: SandboxSchema,
				doneSchema: SandboxStreamDoneSchema,
				onEvent
			})
	);

export const listSandboxes = () => execute(getRequest("/api/sandboxes"), Schema.Array(SandboxSchema));

export const getSandbox = (id: string) =>
	execute(getRequest(`/api/sandboxes/${encodeURIComponent(id)}`), SandboxSchema);

export const restartSandbox = (id: string) =>
	execute(postJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/restart`, {}), SandboxRestartedResponseSchema);

export const resetSandbox = (id: string) =>
	execute(postJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/reset`, {}), SandboxResetResponseSchema);

export const resetSandboxStream = (id: string, onEvent?: (event: SandboxResetStreamEvent) => void) =>
	Effect.flatMap(
		openStream(`/api/sandboxes/${encodeURIComponent(id)}/reset/stream`, {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Accept: "text/event-stream",
				"Accept-Encoding": "identity"
			},
			body: JSON.stringify({})
		}),
		(response) =>
			collectJsonResultSse(response, {
				progressSchema: SandboxOperationProgressSchema,
				resultSchema: SandboxResetResponseSchema,
				doneSchema: SandboxStreamDoneSchema,
				onEvent
			})
	);

export const stopSandbox = (id: string) =>
	execute(postJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/stop`, {}), SandboxSchema);

export const deleteSandbox = (id: string) =>
	execute(deleteRequest(`/api/sandboxes/${encodeURIComponent(id)}`), ItemDeletedResponseSchema);

export const execInSandbox = (id: string, request: ExecRequest) =>
	execute(postJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/exec`, request), ExecResponseSchema);

export const updateSandboxProxyConfig = (id: string, proxyConfig: Record<string, SandboxPortProxyConfig>) =>
	execute(
		patchJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/proxy-config`, { proxy_config: proxyConfig }),
		SandboxSchema
	);

export const updateSandboxEnv = (id: string, request: UpdateSandboxEnvRequest) =>
	execute(patchJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/env`, request), SandboxSchema);

export const readSandboxFile = (id: string, filePath: string) =>
	execute(
		getRequest(`/api/sandboxes/${encodeURIComponent(id)}/files`, { path: filePath }),
		FileReadResponseSchema
	);

export const saveSandboxFile = (id: string, targetPath: string, content: string) =>
	execute(
		putJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/files`, { target_path: targetPath, content }),
		FileSavedResponseSchema
	);

export const uploadSandboxFile = (id: string, targetPath: string, file: Blob, filename = "upload.bin") => {
	const formData = new FormData();
	formData.set("target_path", targetPath);
	formData.set("file", file, filename);

	return executeFetch(`/api/sandboxes/${encodeURIComponent(id)}/files`, FileUploadedResponseSchema, {
		method: "PUT",
		body: formData
	});
};

export const resolveSandboxLogsSseUrl = (id: string, query?: SandboxLogsStreamQuery) => ({
	path: `/api/sandboxes/${encodeURIComponent(id)}/logs`,
	query: {
		follow: query?.follow,
		tail: query?.tail
	}
});

export const resolveSandboxTerminalWebSocketPath = (id: string, query?: SandboxTerminalWebSocketQuery) => ({
	path: `/api/sandboxes/${encodeURIComponent(id)}/terminal/ws`,
	query: {
		cols: query?.cols,
		rows: query?.rows
	}
});
