import { Context, Layer } from "effect";

export type AuthMode =
	| { readonly _tag: "none" }
	| { readonly _tag: "session" }
	| { readonly _tag: "bearer"; readonly token: string }
	| { readonly _tag: "apiKey"; readonly apiKey: string };

export interface SdkAuth {
	readonly mode: AuthMode;
}

export class SdkAuthService extends Context.Tag("open-sandbox/sdk/SdkAuth")<SdkAuthService, SdkAuth>() {}

export const noAuth = (): AuthMode => ({ _tag: "none" });

export const sessionAuth = (): AuthMode => ({ _tag: "session" });

export const bearerAuth = (token: string): AuthMode => ({ _tag: "bearer", token: token.trim() });

export const apiKeyAuth = (apiKey: string): AuthMode => ({ _tag: "apiKey", apiKey: apiKey.trim() });

export const makeSdkAuth = (mode: AuthMode = sessionAuth()): SdkAuth => ({ mode });

export const SdkAuthLayer = (mode: AuthMode = sessionAuth()): Layer.Layer<SdkAuthService> =>
	Layer.succeed(SdkAuthService, makeSdkAuth(mode));
