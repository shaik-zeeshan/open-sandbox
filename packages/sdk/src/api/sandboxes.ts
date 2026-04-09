import { Schema } from "effect";

import { deleteRequest, getRequest, patchJsonRequest, postJsonRequest, putJsonRequest } from "../request";
import {
	type CreateSandboxRequest,
	type ExecRequest,
	type SandboxPortProxyConfig,
	ExecResponseSchema,
	FileReadResponseSchema,
	FileSavedResponseSchema,
	FileUploadedResponseSchema,
	ItemDeletedResponseSchema,
	SandboxResetResponseSchema,
	SandboxRestartedResponseSchema,
	SandboxSchema
} from "../schemas";
import { execute, executeFetch } from "../transport";

export interface SandboxLogsStreamQuery {
	follow?: boolean;
	tail?: string | number;
}

export interface SandboxTerminalWebSocketQuery {
	cols?: number;
	rows?: number;
}

export const createSandbox = (request: CreateSandboxRequest) =>
	execute(postJsonRequest("/api/sandboxes", request), SandboxSchema);

export const listSandboxes = () => execute(getRequest("/api/sandboxes"), Schema.Array(SandboxSchema));

export const getSandbox = (id: string) =>
	execute(getRequest(`/api/sandboxes/${encodeURIComponent(id)}`), SandboxSchema);

export const restartSandbox = (id: string) =>
	execute(postJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/restart`, {}), SandboxRestartedResponseSchema);

export const resetSandbox = (id: string) =>
	execute(postJsonRequest(`/api/sandboxes/${encodeURIComponent(id)}/reset`, {}), SandboxResetResponseSchema);

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
