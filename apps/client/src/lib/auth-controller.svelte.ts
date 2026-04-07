import { formatApiFailure, getSession, healthCheck, logout, refreshSession } from "$lib/api";
import { clientRuntime } from "$lib/client/runtime";
import { beginAuthCheck, clearAuth, clientState, setAuthSession } from "$lib/stores.svelte";
import { toast } from "$lib/toast.svelte";
import type * as HttpClient from "@effect/platform/HttpClient";
import { Effect } from "effect";

export type HealthState = "unknown" | "checking" | "ok" | "error";

class AuthControllerState {
	health = $state<HealthState>("unknown");
	healthMessage = $state("Waiting...");
	authNotice = $state("");
}

export const authController = new AuthControllerState();

const runClientEffect = <A>(effect: Effect.Effect<A, unknown, HttpClient.HttpClient>): Promise<A> =>
	clientRuntime.runPromise(effect);

const applyAuthSession = (session: { user_id: string; username: string; role: string; expires_at: number | null }): void => {
	setAuthSession({
		userId: session.user_id,
		username: session.username,
		role: session.role,
		expiresAt: session.expires_at
	});
	authController.authNotice = "";
};

export const clearAuthNotice = (): void => {
	authController.authNotice = "";
};

export const checkHealth = async (): Promise<void> => {
	authController.health = "checking";
	authController.healthMessage = "Checking...";
	try {
		const result = await runClientEffect(healthCheck(clientState.config));
		authController.health = result.status === "ok" ? "ok" : "error";
		authController.healthMessage = result.status === "ok" ? "Reachable" : `Status: ${result.status}`;
	} catch (error) {
		authController.health = "error";
		authController.healthMessage = formatApiFailure(error);
	}
};

export const refreshAuthSession = async (): Promise<boolean> => {
	try {
		const refreshed = await runClientEffect(refreshSession({ baseUrl: clientState.baseUrl }));
		applyAuthSession(refreshed);
		return true;
	} catch {
		return false;
	}
};

export const restoreSession = async (): Promise<void> => {
	beginAuthCheck();
	try {
		const session = await runClientEffect(getSession({ baseUrl: clientState.baseUrl }));
		applyAuthSession(session);
	} catch (error) {
		if (await refreshAuthSession()) {
			return;
		}

		const message = formatApiFailure(error);
		clearAuth();
		if (!message.startsWith("Unauthorized:")) {
			toast.error(message);
		}
	}
};

export const signOut = async (revoke = true): Promise<void> => {
	if (revoke) {
		try {
			await runClientEffect(logout({ baseUrl: clientState.baseUrl }));
		} catch {
			// Always clear client auth state even if server-side revoke fails.
			toast.warn("Sign out completed, but session cleanup failed.");
		}
	}

	clearAuth();
	clearAuthNotice();
};

export const handleAuthError = (message = "Session expired. Sign in again."): void => {
	clearAuth();
	authController.authNotice = message;
};
