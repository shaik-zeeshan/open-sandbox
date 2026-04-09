import { browser } from "$app/environment";
import { invalidateAllApiCachesSync } from "$lib/api-cache";
import type { ApiConfig, SandboxPortProxyConfig } from "$lib/api";
import { readStorageItem, writeStorageItem } from "$lib/client/browser";

const BASE_URL_KEY = "open-sandbox.base-url";

const resolveDefaultBaseUrl = (): string => {
	const configured = (import.meta.env.VITE_SANDBOX_BASE_URL as string | undefined)?.trim() || "http://localhost:8080";
	if (!browser) {
		return configured;
	}

	if (configured === "/") {
		return window.location.origin;
	}

	if (configured.startsWith("/")) {
		return new URL(configured, `${window.location.origin}/`).toString().replace(/\/+$/, "");
	}

	return configured;
};

const DEFAULT_BASE_URL = resolveDefaultBaseUrl();

const readStorage = (key: string, fallback: string): string => {
	if (!browser) {
		return fallback;
	}

	const value = readStorageItem(key);
	if (value === null) {
		return fallback;
	}

	return value;
};

const writeStorage = (key: string, value: string): void => {
	if (!browser) {
		return;
	}

	writeStorageItem(key, value);
};

export interface PendingDuplicateCreateDraft {
	name: string;
	image: string;
	repoUrl: string;
	branch: string;
	workdir: string;
	env: string[];
	secretEnvKeys: string[];
	ports: string;
	proxyConfig: Record<string, SandboxPortProxyConfig>;
}

class ClientState {
	baseUrl = $state(readStorage(BASE_URL_KEY, DEFAULT_BASE_URL));
	token = $state("");
	userId = $state("");
	username = $state("");
	role = $state("");
	tokenExpiresAt = $state<number | null>(null);
	authResolved = $state(false);
	authenticated = $state(false);
	pendingDuplicateCreateDraft = $state<PendingDuplicateCreateDraft | null>(null);

	get config(): ApiConfig {
		return {
			baseUrl: this.baseUrl,
			token: this.token
		};
	}

	get isAuthenticated(): boolean {
		return this.authenticated;
	}

}

export const clientState = new ClientState();

export const setPendingDuplicateCreateDraft = (draft: PendingDuplicateCreateDraft): void => {
	clientState.pendingDuplicateCreateDraft = draft;
};

export const clearPendingDuplicateCreateDraft = (): void => {
	clientState.pendingDuplicateCreateDraft = null;
};

export const getPendingDuplicateCreateDraft = (): PendingDuplicateCreateDraft | null => {
	return clientState.pendingDuplicateCreateDraft;
};

export const consumePendingDuplicateCreateDraft = (): PendingDuplicateCreateDraft | null => {
	const draft = clientState.pendingDuplicateCreateDraft;
	clientState.pendingDuplicateCreateDraft = null;
	return draft;
};

export const setBaseUrl = (value: string): void => {
	invalidateAllApiCachesSync();
	clientState.baseUrl = value;
	writeStorage(BASE_URL_KEY, value);
};

export const beginAuthCheck = (): void => {
	clientState.authResolved = false;
};

export const setAuthSession = (session: { userId: string; username: string; role: string; expiresAt: number | null }): void => {
	invalidateAllApiCachesSync();
	clientState.token = "";
	clientState.userId = session.userId;
	clientState.username = session.username;
	clientState.role = session.role;
	clientState.tokenExpiresAt = session.expiresAt;
	clientState.authenticated = true;
	clientState.authResolved = true;
};

export const clearAuth = (): void => {
	invalidateAllApiCachesSync();
	clientState.token = "";
	clientState.userId = "";
	clientState.username = "";
	clientState.role = "";
	clientState.tokenExpiresAt = null;
	clientState.authenticated = false;
	clientState.authResolved = true;
};
