import type { Effect as EffectType } from "effect";

import { type AuthMode } from "./auth";
import { type SdkConfig } from "./config";
import { createSdkRuntime, runSdkEffect } from "./runtime";
import type { SdkTransportEnv } from "./transport";
import * as Api from "./api";

export interface CreateClientOptions {
  readonly config: SdkConfig;
  readonly auth?: AuthMode;
}

export const createClient = (options: CreateClientOptions) => {
  const runtime = createSdkRuntime(options);


  return {
    runtime,
    api: Api,
    run: <A, E>(effect: EffectType.Effect<A, E, SdkTransportEnv>) => runSdkEffect(runtime, effect)
  };
};
