import { Effect } from "effect";

import { ApiError, NetworkError, type SdkError } from "./errors";

export interface StreamEvent {
	event: string;
	data: string;
}

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
