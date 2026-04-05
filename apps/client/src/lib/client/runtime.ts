import { FetchHttpClient } from "@effect/platform";
import { Layer, ManagedRuntime } from "effect";

import { BrowserServicesLayer } from "$lib/client/browser";

const ClientRuntimeLayer = Layer.merge(FetchHttpClient.layer, BrowserServicesLayer);

export const clientRuntime = ManagedRuntime.make(ClientRuntimeLayer);
