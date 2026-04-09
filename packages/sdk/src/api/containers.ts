import { Schema } from "effect";

import { deleteRequest, getRequest, postJsonRequest, putJsonRequest } from "../request";
import {
	type CreateContainerRequest,
	type ExecRequest,
	ContainerFileSavedResponseSchema,
	ContainerFileUploadedResponseSchema,
	ContainerRemovedResponseSchema,
	ContainerResetResponseSchema,
	ContainerRestartedResponseSchema,
	ContainerStoppedResponseSchema,
	ContainerSummarySchema,
	CreateContainerResponseSchema,
	ExecResponseSchema,
	FileReadResponseSchema
} from "../schemas";
import { execute, executeFetch } from "../transport";

export interface ContainerLogsStreamQuery {
	follow?: boolean;
	tail?: string | number;
}

export interface ContainerTerminalWebSocketQuery {
	cols?: number;
	rows?: number;
	workdir?: string;
}

export const listContainers = () =>
	execute(getRequest("/api/containers"), Schema.Array(ContainerSummarySchema));

export const createContainer = (request: CreateContainerRequest) =>
	execute(postJsonRequest("/api/containers/create", request), CreateContainerResponseSchema);

export const execInContainer = (id: string, request: ExecRequest) =>
	execute(postJsonRequest(`/api/containers/${encodeURIComponent(id)}/exec`, request), ExecResponseSchema);

export const stopContainer = (id: string) =>
	execute(postJsonRequest(`/api/containers/${encodeURIComponent(id)}/stop`, {}), ContainerStoppedResponseSchema);

export const restartContainer = (id: string) =>
	execute(
		postJsonRequest(`/api/containers/${encodeURIComponent(id)}/restart`, {}),
		ContainerRestartedResponseSchema
	);

export const resetContainer = (id: string) =>
	execute(postJsonRequest(`/api/containers/${encodeURIComponent(id)}/reset`, {}), ContainerResetResponseSchema);

export const removeContainer = (id: string, force = true) =>
	execute(
		deleteRequest(`/api/containers/${encodeURIComponent(id)}`, force ? { force: true } : undefined),
		ContainerRemovedResponseSchema
	);

export const readContainerFile = (id: string, filePath: string) =>
	execute(
		getRequest(`/api/containers/${encodeURIComponent(id)}/files`, { path: filePath }),
		FileReadResponseSchema
	);

export const saveContainerFile = (id: string, targetPath: string, content: string) =>
	execute(
		putJsonRequest(`/api/containers/${encodeURIComponent(id)}/files`, { target_path: targetPath, content }),
		ContainerFileSavedResponseSchema
	);

export const uploadContainerFile = (id: string, targetPath: string, file: Blob, filename = "upload.bin") => {
	const formData = new FormData();
	formData.set("target_path", targetPath);
	formData.set("file", file, filename);

	return executeFetch(
		`/api/containers/${encodeURIComponent(id)}/files`,
		ContainerFileUploadedResponseSchema,
		{
			method: "PUT",
			body: formData
		}
	);
};

export const resolveContainerLogsSseUrl = (id: string, query?: ContainerLogsStreamQuery) => ({
	path: `/api/containers/${encodeURIComponent(id)}/logs`,
	query: {
		follow: query?.follow,
		tail: query?.tail
	}
});

export const resolveContainerTerminalWebSocketPath = (id: string, query?: ContainerTerminalWebSocketQuery) => ({
	path: `/api/containers/${encodeURIComponent(id)}/terminal/ws`,
	query: {
		cols: query?.cols,
		rows: query?.rows,
		workdir: query?.workdir
	}
});
