import { FetchHttpClient, HttpClient, HttpClientRequest } from "@effect/platform";
import type { HttpClientResponse } from "@effect/platform";
import { Effect, Schema, pipe } from "effect";

import { type SdkAuth, SdkAuthService } from "./auth";
import { resolveApiUrl, SdkConfigService } from "./config";
import {
	ApiError,
	AuthError,
	extractAuthReason,
	extractMessage,
	mapAuthReasonToMessage,
	NetworkError,
	type SdkError
} from "./errors";

export type SdkTransportEnv = HttpClient.HttpClient | SdkConfigService | SdkAuthService;

const configureRequest = (
	request: HttpClientRequest.HttpClientRequest,
	baseUrl: string,
	auth: SdkAuth
): HttpClientRequest.HttpClientRequest => {
	let configured = pipe(request, HttpClientRequest.prependUrl(baseUrl));
	if (auth.mode._tag === "bearer" && auth.mode.token.length > 0) {
		configured = HttpClientRequest.setHeader(configured, "Authorization", `Bearer ${auth.mode.token}`);
	}
	if (auth.mode._tag === "apiKey" && auth.mode.apiKey.length > 0) {
		configured = HttpClientRequest.setHeader(configured, "X-API-Key", auth.mode.apiKey);
	}
	return configured;
};

const readClientPayload = (response: HttpClientResponse.HttpClientResponse): Effect.Effect<unknown, never> =>
	pipe(response.json, Effect.orElse(() => response.text), Effect.orElseSucceed(() => ""));

const readFetchPayload = (response: Response): Effect.Effect<unknown, never> =>
	pipe(
		Effect.tryPromise({
			try: () => response.text(),
			catch: () => ""
		}),
		Effect.flatMap((text) => {
			if (text.trim().length === 0) {
				return Effect.succeed<unknown>("");
			}

			return pipe(
				Effect.try({
					try: () => JSON.parse(text) as unknown,
					catch: () => text
				}),
				Effect.orElseSucceed(() => text)
			);
		}),
		Effect.orElseSucceed(() => "")
	);

const applyFetchAuth = (headers: Headers, auth: SdkAuth): void => {
	if (auth.mode._tag === "bearer" && auth.mode.token.length > 0) {
		headers.set("Authorization", `Bearer ${auth.mode.token}`);
	}
	if (auth.mode._tag === "apiKey" && auth.mode.apiKey.length > 0) {
		headers.set("X-API-Key", auth.mode.apiKey);
	}
};

const fetchCredentialsForAuth = (auth: SdkAuth): RequestCredentials =>
	auth.mode._tag === "session" ? "include" : "omit";

export const execute = <A, I = A>(
	request: HttpClientRequest.HttpClientRequest,
	schema: Schema.Schema<A, I>
): Effect.Effect<A, SdkError, SdkTransportEnv> =>
	Effect.gen(function* () {
		const config = yield* SdkConfigService;
		const auth = yield* SdkAuthService;
		const client = yield* HttpClient.HttpClient;

		const response = yield* pipe(
			Effect.provideService(
				client.execute(configureRequest(request, config.baseUrl, auth)),
				FetchHttpClient.RequestInit,
				{ credentials: fetchCredentialsForAuth(auth) }
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

		return yield* pipe(
			Schema.decodeUnknown(schema)(payload),
			Effect.mapError(
				(error) =>
					new ApiError({
						status: response.status,
						message: "Response payload did not match expected shape.",
						body: error
					})
			)
		);
	});

export const executeFetch = <A, I = A>(
	path: string,
	schema: Schema.Schema<A, I>,
	init?: RequestInit
): Effect.Effect<A, SdkError, SdkConfigService | SdkAuthService> =>
	Effect.gen(function* () {
		const config = yield* SdkConfigService;
		const auth = yield* SdkAuthService;

		const headers = new Headers(init?.headers);
		applyFetchAuth(headers, auth);

		const response = yield* pipe(
			Effect.tryPromise({
				try: () =>
					fetch(resolveApiUrl(config, path), {
						...init,
						headers,
						credentials: fetchCredentialsForAuth(auth)
					}),
				catch: (error) =>
					new NetworkError({
						message: error instanceof Error ? error.message : "Network request failed.",
						cause: error
					})
			}),
			Effect.mapError((error) => error as SdkError)
		);

		const payload = yield* readFetchPayload(response);

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

		return yield* pipe(
			Schema.decodeUnknown(schema)(payload),
			Effect.mapError(
				(error) =>
					new ApiError({
						status: response.status,
						message: "Response payload did not match expected shape.",
						body: error
					})
			)
		);
	});

export const openStream = (
	path: string,
	init?: RequestInit
): Effect.Effect<Response, SdkError, SdkConfigService | SdkAuthService> =>
	Effect.gen(function* () {
		const config = yield* SdkConfigService;
		const auth = yield* SdkAuthService;

		const headers = new Headers(init?.headers);
		applyFetchAuth(headers, auth);

		const response = yield* pipe(
			Effect.tryPromise({
				try: () =>
					fetch(resolveApiUrl(config, path), {
						...init,
						headers,
						credentials: fetchCredentialsForAuth(auth)
					}),
				catch: (error) =>
					new NetworkError({
						message: error instanceof Error ? error.message : "Network request failed.",
						cause: error
					})
			}),
			Effect.mapError((error) => error as SdkError)
		);

		if (response.status === 401) {
			const payload = yield* readFetchPayload(response);
			const reason = extractAuthReason(payload);
			return yield* Effect.fail(new AuthError({ message: mapAuthReasonToMessage(reason), reason }));
		}

		if (!response.ok) {
			const payload = yield* readFetchPayload(response);
			return yield* Effect.fail(
				new ApiError({
					status: response.status,
					message: extractMessage(payload, `Request failed with status ${response.status}`),
					body: payload
				})
			);
		}

		return response;
	});
