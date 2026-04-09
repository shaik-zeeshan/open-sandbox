import { Effect, Schema } from "effect";

import { ApiError, extractMessage, NetworkError, type SdkError } from "./errors";
import { ErrorResponseSchema } from "./schemas";

export interface StreamEvent {
	event: string;
	data: string;
}

export type JsonResultStreamEvent<TProgress, TResult, TDone> =
	| { event: "progress"; data: TProgress }
	| { event: "result"; data: TResult }
	| { event: "done"; data: TDone };

export const parseSseEvent = (block: string): StreamEvent | null => {
	const lines = block
		.split("\n")
		.map((line) => line.trimEnd())
		.filter((line) => line.length > 0);

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

export const collectSseOutput = (
	response: Response,
	onEvent?: (event: StreamEvent) => void
): Effect.Effect<{ stdout: string; stderr: string }, SdkError> =>
	pipeRead(response, onEvent);

export const collectJsonResultSse = <TProgress, TResult, TDone>(
	response: Response,
	options: {
		progressSchema: Schema.Schema<TProgress>;
		resultSchema: Schema.Schema<TResult>;
		doneSchema: Schema.Schema<TDone>;
		onEvent?: (event: JsonResultStreamEvent<TProgress, TResult, TDone>) => void;
	}
): Effect.Effect<TResult, SdkError> =>
	Effect.tryPromise({
		try: async () => {
			if (response.body === null) {
				throw new ApiError({
					status: response.status,
					message: "Response body is not readable.",
					body: null
				});
			}

			const decodeEventData = <A, I>(data: string, schema: Schema.Schema<A, I>): A => {
				let payload: unknown;
				try {
					payload = JSON.parse(data) as unknown;
				} catch (error) {
					throw new ApiError({
						status: response.status,
						message: "Stream event payload was not valid JSON.",
						body: error
					});
				}

				try {
					return Schema.decodeUnknownSync(schema)(payload);
				} catch (error) {
					throw new ApiError({
						status: response.status,
						message: "Stream event payload did not match expected shape.",
						body: error
					});
				}
			};

			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			let buffer = "";
			let result: TResult | undefined;

			const handleEvent = (event: StreamEvent): void => {
				if (event.event === "error") {
					const trimmed = event.data.trim();
					let payload: unknown = trimmed;
					let structuredPayload: Schema.Schema.Type<typeof ErrorResponseSchema> | undefined;
					if (trimmed.startsWith("{")) {
						try {
							payload = JSON.parse(trimmed) as unknown;
							structuredPayload = Schema.decodeUnknownSync(ErrorResponseSchema)(payload);
						} catch {
							payload = trimmed;
						}
					}
					const body = structuredPayload ?? payload;
					const status = structuredPayload?.status ?? response.status;
					throw new ApiError({
						status,
						message: extractMessage(body, "Stream request failed."),
						body
					});
				}

				if (event.event === "progress") {
					options.onEvent?.({
						event: "progress",
						data: decodeEventData(event.data, options.progressSchema)
					});
					return;
				}

				if (event.event === "result") {
					const decoded = decodeEventData(event.data, options.resultSchema);
					result = decoded;
					options.onEvent?.({ event: "result", data: decoded });
					return;
				}

				if (event.event === "done") {
					options.onEvent?.({
						event: "done",
						data: decodeEventData(event.data, options.doneSchema)
					});
				}
			};

			const consumeBuffer = (pending: string): string => {
				const normalized = pending.replace(/\r\n/g, "\n");
				const chunks = normalized.split("\n\n");
				const remainder = chunks.pop() ?? "";

				for (const chunk of chunks) {
					const parsed = parseSseEvent(chunk);
					if (parsed !== null) {
						handleEvent(parsed);
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
				buffer = consumeBuffer(buffer);
			}

			if (buffer.trim().length > 0) {
				const parsed = parseSseEvent(buffer);
				if (parsed !== null) {
					handleEvent(parsed);
				}
			}

			if (result === undefined) {
				throw new ApiError({
					status: response.status,
					message: "Stream did not include a result event.",
					body: null
				});
			}

			return result;
		},
		catch: (error) =>
			error instanceof ApiError
				? error
				: new NetworkError({
						message: error instanceof Error ? error.message : "Unable to read stream response.",
						cause: error
					})
	});

const pipeRead = (
	response: Response,
	onEvent?: (event: StreamEvent) => void
): Effect.Effect<{ stdout: string; stderr: string }, SdkError> =>
	Effect.tryPromise({
		try: async () => {
			if (response.body === null) {
				throw new ApiError({
					status: response.status,
					message: "Response body is not readable.",
					body: null
				});
			}

			const reader = response.body.getReader();
			const decoder = new TextDecoder();
			let buffer = "";
			const stdout: string[] = [];
			const stderr: string[] = [];

			const consumeBuffer = (pending: string): string => {
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
				buffer = consumeBuffer(buffer);
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
			error instanceof ApiError
				? error
				: new NetworkError({
						message: error instanceof Error ? error.message : "Unable to read stream response.",
						cause: error
					})
	});
