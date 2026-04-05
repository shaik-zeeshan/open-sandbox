import {
	composeDown,
	composeStatus,
	composeUpStream,
	formatApiFailure,
	runApiEffect,
	type ApiConfig,
	type ComposeRequest,
	type StreamEvent
} from "$lib/api";
import { invalidateWorkloadCaches } from "$lib/api-cache";
import { toast } from "$lib/toast.svelte";
import { Effect } from "effect";

export type ComposeAction = "up" | "status" | "down";

export interface ComposeRuntimeState {
	setLoading: (value: boolean) => void;
	setStep: (value: string) => void;
	setLogs: (value: string) => void;
	appendLog: (value: string) => void;
	setStatusServiceNames: (value: string[]) => void;
}

interface RunComposeActionOptions {
	action: ComposeAction;
	config: ApiConfig;
	request: ComposeRequest;
	removeVolumes: boolean;
	removeOrphans: boolean;
	runtime: ComposeRuntimeState;
}

const requireComposeRequest = (request: ComposeRequest): Effect.Effect<ComposeRequest, Error> =>
	Effect.gen(function* () {
		if (!request.content) {
			return yield* Effect.fail(new Error("docker-compose.yml content is required."));
		}
		if (!request.project_name) {
			return yield* Effect.fail(new Error("Compose project name is required."));
		}
		return request;
	});

const extractServiceNamesFromStatus = (value: unknown): string[] => {
	if (!Array.isArray(value)) {
		return [];
	}

	const names: string[] = [];
	for (const item of value) {
		if (typeof item !== "object" || item === null) {
			continue;
		}
		const record = item as Record<string, unknown>;
		const candidate = record["Service"] ?? record["service"] ?? record["Name"] ?? record["name"];
		if (typeof candidate === "string" && candidate.trim().length > 0) {
			names.push(candidate.trim());
		}
	}

	return Array.from(new Set(names));
};

const handleComposeStreamEvent = (
	event: StreamEvent,
	runtime: ComposeRuntimeState,
	setComposeError: (value: string) => void
): Effect.Effect<void> =>
	Effect.sync(() => {
		if ((event.event === "stdout" || event.event === "stderr") && event.data.length > 0) {
			runtime.appendLog(event.data);
		}
		if (event.event === "error") {
			setComposeError(event.data.trim());
		}
	});

const runComposeUpEffect = (
	config: ApiConfig,
	request: ComposeRequest,
	runtime: ComposeRuntimeState
): Effect.Effect<void, unknown> =>
	Effect.gen(function* () {
		yield* requireComposeRequest(request);

		yield* Effect.sync(() => {
			runtime.setStep("Running compose up");
			runtime.appendLog(`Starting docker compose up (project: ${request.project_name})...`);
		});

		let composeError = "";
		const result = yield* Effect.promise(() => runApiEffect(composeUpStream(config, request, (event) => {
			Effect.runSync(handleComposeStreamEvent(event, runtime, (value) => {
				composeError = value;
			}));
		})));

		yield* Effect.sync(() => {
			if (result.stdout.trim().length > 0) {
				runtime.appendLog(result.stdout);
			}
			if (result.stderr.trim().length > 0) {
				runtime.appendLog(result.stderr);
			}
		});

		if (composeError.length > 0) {
			return yield* Effect.fail(new Error(composeError));
		}

		yield* invalidateWorkloadCaches(config);
		yield* Effect.sync(() => {
			runtime.setStep("Done");
			toast.ok("Compose project started.");
			runtime.appendLog("Compose up complete.");
		});
	});

const runComposeStatusEffect = (
	config: ApiConfig,
	request: ComposeRequest,
	runtime: ComposeRuntimeState
): Effect.Effect<void, unknown> =>
	Effect.gen(function* () {
		yield* requireComposeRequest(request);

		const result = yield* Effect.promise(() => runApiEffect(composeStatus(config, request)));
		yield* Effect.sync(() => {
			runtime.setStatusServiceNames(extractServiceNamesFromStatus(result.services));
			runtime.appendLog(result.raw || JSON.stringify(result.services, null, 2));
			toast.ok("Compose status loaded.");
			runtime.setStep("Done");
		});
	});

const runComposeDownEffect = (
	config: ApiConfig,
	request: ComposeRequest,
	removeVolumes: boolean,
	removeOrphans: boolean,
	runtime: ComposeRuntimeState
): Effect.Effect<void, unknown> =>
	Effect.gen(function* () {
		const downRequest = {
			...(yield* requireComposeRequest(request)),
			volumes: removeVolumes,
			remove_orphans: removeOrphans
		};

		const result = yield* Effect.promise(() => runApiEffect(composeDown(config, downRequest)));
		yield* invalidateWorkloadCaches(config);
		yield* Effect.sync(() => {
			if (result.stdout.trim().length > 0) {
				runtime.appendLog(result.stdout);
			}
			if (result.stderr.trim().length > 0) {
				runtime.appendLog(result.stderr);
			}
			toast.ok("Compose project stopped.");
			runtime.setStep("Done");
		});
	});

const runComposeActionEffect = ({
	action,
	config,
	request,
	removeVolumes,
	removeOrphans,
	runtime
}: RunComposeActionOptions): Effect.Effect<void, unknown> =>
	Effect.gen(function* () {
		yield* Effect.sync(() => {
			runtime.setLoading(true);
			if (action === "up") {
				runtime.setLogs("");
				runtime.setStep("Preparing");
			} else if (action === "status") {
				runtime.setStep("Fetching status");
			} else {
				runtime.setStep("Running compose down");
			}
		});

		try {
			if (action === "up") {
				yield* runComposeUpEffect(config, request, runtime);
				return;
			}
			if (action === "status") {
				yield* runComposeStatusEffect(config, request, runtime);
				return;
			}
			yield* runComposeDownEffect(config, request, removeVolumes, removeOrphans, runtime);
		} catch (error) {
			yield* Effect.sync(() => {
				toast.error(formatApiFailure(error));
				runtime.setStep("Failed");
			});
		} finally {
			yield* Effect.sync(() => {
				runtime.setLoading(false);
			});
		}
	});

export const runComposeAction = (options: RunComposeActionOptions): Promise<void> =>
	Effect.runPromise(runComposeActionEffect(options));
