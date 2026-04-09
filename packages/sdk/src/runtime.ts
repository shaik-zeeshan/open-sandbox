import { FetchHttpClient } from "@effect/platform";
import { Layer, ManagedRuntime } from "effect";

import { type AuthMode, SdkAuthLayer, sessionAuth } from "./auth";
import { type SdkConfig, SdkConfigLayer } from "./config";

export interface CreateSdkRuntimeOptions {
	readonly config: SdkConfig;
	readonly auth?: AuthMode;
}

export const createSdkRuntime = (options: CreateSdkRuntimeOptions) =>
	ManagedRuntime.make(
		Layer.mergeAll(
			FetchHttpClient.layer,
			SdkConfigLayer(options.config),
			SdkAuthLayer(options.auth ?? sessionAuth())
		)
	);

export const runSdkEffect = <A, E, R>(
	runtime: ReturnType<typeof createSdkRuntime>,
	effect: import("effect").Effect.Effect<A, E, import("./transport").SdkTransportEnv>
): Promise<A> => runtime.runPromise(effect);
