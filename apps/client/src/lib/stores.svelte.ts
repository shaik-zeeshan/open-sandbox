import { browser } from "$app/environment";
import type { ApiConfig } from "$lib/api";

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

	const value = localStorage.getItem(key);
	if (value === null) {
		return fallback;
	}

	return value;
};

const writeStorage = (key: string, value: string): void => {
	if (!browser) {
		return;
	}

	localStorage.setItem(key, value);
};

class ClientState {
	baseUrl = $state(readStorage(BASE_URL_KEY, DEFAULT_BASE_URL));
	token = $state("");
	userId = $state("");
	username = $state("");
	role = $state("");
	tokenExpiresAt = $state<number | null>(null);
	authResolved = $state(false);
	authenticated = $state(false);

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

export const setBaseUrl = (value: string): void => {
	clientState.baseUrl = value;
	writeStorage(BASE_URL_KEY, value);
};

export const beginAuthCheck = (): void => {
	clientState.authResolved = false;
};

export const setAuthSession = (session: { userId: string; username: string; role: string; expiresAt: number | null }): void => {
	clientState.token = "";
	clientState.userId = session.userId;
	clientState.username = session.username;
	clientState.role = session.role;
	clientState.tokenExpiresAt = session.expiresAt;
	clientState.authenticated = true;
	clientState.authResolved = true;
};

export const clearAuth = (): void => {
	clientState.token = "";
	clientState.userId = "";
	clientState.username = "";
	clientState.role = "";
	clientState.tokenExpiresAt = null;
	clientState.authenticated = false;
	clientState.authResolved = true;
};
