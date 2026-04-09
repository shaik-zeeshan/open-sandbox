import { Effect, Schema } from "effect";

import { deleteRequest, getRequest, postJsonRequest } from "../request";
import {
	type BuildImageRequest,
	type PullImageRequest,
	ComposeResponseSchema,
	ImageSearchResultSchema,
	ImageSummarySchema,
	OutputImageResponseSchema,
	RemoveImageResponseSchema
} from "../schemas";
import type { StreamEvent } from "../stream";
import { collectSseOutput } from "../stream";
import { execute, openStream } from "../transport";

export const listImages = () =>
	execute(getRequest("/api/images"), Schema.Array(ImageSummarySchema));

export const searchImages = (query: string, limit = 25) =>
	execute(
		getRequest("/api/images/search", { q: query, limit }),
		Schema.Array(ImageSearchResultSchema)
	);

export const pullImage = (request: PullImageRequest) =>
	execute(postJsonRequest("/api/images/pull", request), OutputImageResponseSchema);

export const buildImage = (request: BuildImageRequest) =>
	execute(postJsonRequest("/api/images/build", request), OutputImageResponseSchema);

export const removeImage = (id: string, force = false) =>
	execute(deleteRequest(`/api/images/${encodeURIComponent(id)}`, force ? { force: true } : undefined), RemoveImageResponseSchema);

export const buildImageStream = (request: BuildImageRequest, onEvent?: (event: StreamEvent) => void) =>
	Effect.flatMap(
		openStream("/api/images/build/stream", {
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
