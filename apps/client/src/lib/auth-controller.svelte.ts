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

const checkHealthProgram = (): Effect.Effect<void, never, HttpClient.HttpClient> =>
	Effect.gen(function* () {
		yield* Effect.sync(() => {
			authController.health = "checking";
			authController.healthMessage = "Checking...";
		});

		const result = yield* Effect.match(healthCheck(clientState.config), {
			onFailure: (error) => ({
				health: "error" as const,
				message: formatApiFailure(error)
			}),
			onSuccess: (status) => ({
				health: status.status === "ok" ? ("ok" as const) : ("error" as const),
				message: status.status === "ok" ? "Reachable" : `Status: ${status.status}`
			})
		});

		yield* Effect.sync(() => {
			authController.health = result.health;
			authController.healthMessage = result.message;
		});
	});

const refreshAuthSessionProgram = (): Effect.Effect<boolean, never, HttpClient.HttpClient> =>
	Effect.match(refreshSession({ baseUrl: clientState.baseUrl }), {
		onFailure: () => false,
		onSuccess: (refreshed) => {
			applyAuthSession(refreshed);
			return true;
		}
	});

const restoreSessionProgram = (): Effect.Effect<void, never, HttpClient.HttpClient> =>
	Effect.gen(function* () {
		yield* Effect.sync(beginAuthCheck);

		const sessionResult = yield* Effect.either(getSession({ baseUrl: clientState.baseUrl }));
		if (sessionResult._tag === "Right") {
			applyAuthSession(sessionResult.right);
			return;
		}

		const refreshed = yield* refreshAuthSessionProgram();
		if (refreshed) {
			return;
		}

		const message = formatApiFailure(sessionResult.left);

		yield* Effect.sync(() => {
			clearAuth();
			if (!message.startsWith("Unauthorized:")) {
				toast.error(message);
			}
		});
	});

const signOutProgram = (revoke: boolean): Effect.Effect<void, never, HttpClient.HttpClient> =>
	Effect.gen(function* () {
		if (revoke) {
			yield* Effect.catchAll(logout({ baseUrl: clientState.baseUrl }), () =>
				Effect.sync(() => {
					// Always clear client auth state even if server-side revoke fails.
					toast.warn("Sign out completed, but session cleanup failed.");
				})
			);
		}

		yield* Effect.sync(() => {
			clearAuth();
			clearAuthNotice();
		});
	});

export const checkHealth = (): Promise<void> => runClientEffect(checkHealthProgram());

export const refreshAuthSession = (): Promise<boolean> => runClientEffect(refreshAuthSessionProgram());

export const restoreSession = (): Promise<void> => runClientEffect(restoreSessionProgram());

export const signOut = (revoke = true): Promise<void> => runClientEffect(signOutProgram(revoke));

export const handleAuthError = (message = "Session expired. Sign in again."): void => {
	clearAuth();
	authController.authNotice = message;
};
