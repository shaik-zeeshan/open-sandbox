import { Context, Layer } from "effect";

export interface SdkConfig {
	readonly baseUrl: string;
}

export const DEFAULT_BASE_URL = "http://localhost:8080";

export class SdkConfigService extends Context.Tag("open-sandbox/sdk/SdkConfig")<
	SdkConfigService,
	SdkConfig
>() {}

export const normalizeBaseUrl = (baseUrl: string): string => {
	const trimmed = baseUrl.trim();
	if (trimmed.length === 0) {
		return DEFAULT_BASE_URL;
	}

	const withScheme = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
	return withScheme.replace(/\/+$/, "");
};

export const normalizePath = (path: string): string => (path.startsWith("/") ? path : `/${path}`);

export const resolveApiUrl = (
	config: SdkConfig,
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): string => {
	const base = normalizeBaseUrl(config.baseUrl);
	const url = new URL(normalizePath(path), `${base}/`);

	if (query) {
		for (const [key, value] of Object.entries(query)) {
			if (value === undefined || value === "") {
				continue;
			}
			url.searchParams.set(key, String(value));
		}
	}

	return url.toString();
};

export const resolveWebSocketUrl = (
	config: SdkConfig,
	path: string,
	query?: Record<string, string | boolean | number | undefined>
): string => {
	const apiUrl = new URL(resolveApiUrl(config, path, query));
	apiUrl.protocol = apiUrl.protocol === "https:" ? "wss:" : "ws:";
	return apiUrl.toString();
};

export const makeSdkConfig = (config: SdkConfig): SdkConfig => ({
	baseUrl: normalizeBaseUrl(config.baseUrl)
});

export const SdkConfigLayer = (config: SdkConfig): Layer.Layer<SdkConfigService> =>
	Layer.succeed(SdkConfigService, makeSdkConfig(config));
