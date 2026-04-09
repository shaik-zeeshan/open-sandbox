import { Effect, Schema } from "effect";

import { getRequest, postJsonRequest } from "../request";
import {
	type ComposeRequest,
	ComposeProjectPreviewSchema,
	ComposeResponseSchema,
	ComposeStatusResponseSchema
} from "../schemas";
import type { StreamEvent } from "../stream";
import { collectSseOutput } from "../stream";
import { execute, openStream } from "../transport";

export const composeDown = (request: ComposeRequest) =>
	execute(postJsonRequest("/api/compose/down", request), ComposeResponseSchema);

export const composeStatus = (request: ComposeRequest) =>
	execute(postJsonRequest("/api/compose/status", request), ComposeStatusResponseSchema);

export const listComposeProjects = () =>
	execute(
		getRequest("/api/compose/projects"),
		Schema.Array(ComposeProjectPreviewSchema)
	);

export const getComposeProject = (projectName: string) =>
	execute(
		getRequest(`/api/compose/projects/${encodeURIComponent(projectName)}`),
		ComposeProjectPreviewSchema
	);

export const composeUpStream = (request: ComposeRequest, onEvent?: (event: StreamEvent) => void) =>
	Effect.flatMap(
		openStream("/api/compose/up", {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Accept: "text/event-stream",
				"Accept-Encoding": "identity"
			},
			body: JSON.stringify(request)
		}),
		(response) =>
			Effect.flatMap(collectSseOutput(response, onEvent), (payload) =>
				Schema.decodeUnknown(ComposeResponseSchema)(payload)
			)
	);
