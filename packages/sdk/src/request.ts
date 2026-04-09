import { HttpClientRequest } from "@effect/platform";
import { pipe } from "effect";

export const getRequest = (
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): HttpClientRequest.HttpClientRequest =>
	HttpClientRequest.get(path, {
		urlParams: query
			? Object.fromEntries(
					Object.entries(query)
						.filter(([, value]) => value !== undefined && value !== "")
						.map(([key, value]) => [key, String(value)])
				)
			: undefined
	});

export const deleteRequest = (
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): HttpClientRequest.HttpClientRequest =>
	HttpClientRequest.del(path, {
		urlParams: query
			? Object.fromEntries(
					Object.entries(query)
						.filter(([, value]) => value !== undefined && value !== "")
						.map(([key, value]) => [key, String(value)])
				)
			: undefined
	});

export const postJsonRequest = (path: string, body: unknown): HttpClientRequest.HttpClientRequest =>
	pipe(HttpClientRequest.post(path), HttpClientRequest.bodyUnsafeJson(body));

export const putJsonRequest = (path: string, body: unknown): HttpClientRequest.HttpClientRequest =>
	pipe(HttpClientRequest.put(path), HttpClientRequest.bodyUnsafeJson(body));

export const patchJsonRequest = (path: string, body: unknown): HttpClientRequest.HttpClientRequest =>
	pipe(HttpClientRequest.patch(path), HttpClientRequest.bodyUnsafeJson(body));
