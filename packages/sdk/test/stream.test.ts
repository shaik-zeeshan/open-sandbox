import { describe, expect, test } from "bun:test";

import { Effect, Schema } from "effect";

import { ApiError } from "../src/errors";
import { collectJsonResultSse } from "../src/stream";

const httpStatusOk = 200;

const streamResponse = (status: number, body: string) =>
	new Response(new ReadableStream({
		start(controller) {
			controller.enqueue(new TextEncoder().encode(body));
			controller.close();
		}
	}), {
		status,
		headers: { "Content-Type": "text/event-stream" }
	});

describe("collectJsonResultSse", () => {
	test("preserves status from structured stream errors", async () => {
		const response = streamResponse(
			httpStatusOk,
			`event: error\ndata: {"error":"reset repository failed","reason":"git_cleanup_failed","stderr":"permission denied","status":500}\n\n`
		);

		const error = await Effect.runPromise(
			Effect.flip(
				collectJsonResultSse(response, {
					progressSchema: Schema.Struct({ phase: Schema.String, status: Schema.String, message: Schema.String }),
					resultSchema: Schema.Struct({ id: Schema.String, reset: Schema.Boolean }),
					doneSchema: Schema.Struct({ id: Schema.String, reset: Schema.optional(Schema.Boolean) })
				})
			)
		);

		expect(error).toBeInstanceOf(ApiError);
		expect((error as ApiError).status).toBe(500);
		expect((error as ApiError).message).toBe("reset repository failed");
	});
});
