import { browser } from "$app/environment";
import { Context, Effect, Fiber, Layer } from "effect";

export const AUTH_ERROR_EVENT = "open-sandbox:auth-error";
export const TOAST_EVENT = "open-sandbox:toast";

export type TimeoutHandle = Fiber.RuntimeFiber<void, never>;
export type IntervalHandle = Fiber.RuntimeFiber<void, never>;
export type Cleanup = () => void;

export type ToastDispatch = {
	id: string;
	kind: "error" | "ok" | "warn" | "loading";
	message: string;
	duration: number;
};

export interface StorageService {
	getItem: (key: string) => string | null;
	setItem: (key: string, value: string) => void;
	removeItem: (key: string) => void;
}

export interface WindowEventsService {
	dispatch: (name: string, detail?: unknown) => void;
	listen: (name: string, listener: EventListenerOrEventListenerObject) => Cleanup;
}

export interface ToastDispatchService {
	dispatch: (payload: ToastDispatch) => void;
	listen: (listener: (payload: ToastDispatch) => void) => Cleanup;
}

export const StorageService = Context.GenericTag<StorageService>("open-sandbox/client/StorageService");
export const WindowEventsService = Context.GenericTag<WindowEventsService>("open-sandbox/client/WindowEventsService");
export const ToastDispatchService = Context.GenericTag<ToastDispatchService>("open-sandbox/client/ToastDispatchService");

const canUseWindow = (): boolean => browser && typeof window !== "undefined";

const storageService: StorageService = {
	getItem: (key) => {
		if (!canUseWindow()) {
			return null;
		}
		try {
			return window.localStorage.getItem(key);
		} catch {
			// Ignore storage quota/privacy mode errors.
			return null;
		}
	},
	setItem: (key, value) => {
		if (!canUseWindow()) {
			return;
		}
		try {
			window.localStorage.setItem(key, value);
		} catch {
			// Ignore storage quota/privacy mode errors.
		}
	},
	removeItem: (key) => {
		if (!canUseWindow()) {
			return;
		}
		try {
			window.localStorage.removeItem(key);
		} catch {
			// Ignore storage quota/privacy mode errors.
		}
	}
};

const eventsService: WindowEventsService = {
	dispatch: (name, detail) => {
		if (!canUseWindow()) {
			return;
		}
		window.dispatchEvent(new CustomEvent(name, { detail }));
	},
	listen: (name, listener) => {
		if (!canUseWindow()) {
			return () => {};
		}
		window.addEventListener(name, listener);
		return () => window.removeEventListener(name, listener);
	}
};

const isToastDispatch = (detail: unknown): detail is ToastDispatch => {
	if (typeof detail !== "object" || detail === null) {
		return false;
	}
	const payload = detail as Partial<ToastDispatch>;
	return (
		typeof payload.id === "string" &&
		(payload.kind === "error" || payload.kind === "ok" || payload.kind === "warn" || payload.kind === "loading") &&
		typeof payload.message === "string" &&
		typeof payload.duration === "number"
	);
};

const toastDispatchService: ToastDispatchService = {
	dispatch: (payload) => {
		eventsService.dispatch(TOAST_EVENT, payload);
	},
	listen: (listener) =>
		eventsService.listen(TOAST_EVENT, (event) => {
			if (!(event instanceof CustomEvent)) {
				return;
			}
			if (isToastDispatch(event.detail)) {
				listener(event.detail);
			}
		})
};

export const BrowserServicesLayer = Layer.mergeAll(
	Layer.succeed(StorageService, storageService),
	Layer.succeed(WindowEventsService, eventsService),
	Layer.succeed(ToastDispatchService, toastDispatchService)
);

export const readStorageItem = (key: string): string | null => storageService.getItem(key);
export const writeStorageItem = (key: string, value: string): void => storageService.setItem(key, value);
export const removeStorageItem = (key: string): void => storageService.removeItem(key);

export const dispatchAuthErrorEvent = (): void => eventsService.dispatch(AUTH_ERROR_EVENT);
export const onAuthErrorEvent = (listener: () => void): Cleanup =>
	eventsService.listen(AUTH_ERROR_EVENT, () => listener());

const runTask = (fn: () => void): Effect.Effect<void> =>
	Effect.sync(() => {
		fn();
	});

export const scheduleTimeout = (fn: () => void, delayMs: number): TimeoutHandle | null => {
	if (!canUseWindow()) {
		return null;
	}
	const delay = Math.max(0, delayMs);
	return Effect.runFork(
		Effect.sleep(delay).pipe(
			Effect.andThen(runTask(fn))
		)
	);
};

export const clearScheduledTimeout = (handle: TimeoutHandle | null | undefined): void => {
	if (handle === null || handle === undefined) {
		return;
	}
	Effect.runFork(Fiber.interruptFork(handle));
};

export const scheduleInterval = (fn: () => void, delayMs: number): IntervalHandle | null => {
	if (!canUseWindow()) {
		return null;
	}
	const delay = Math.max(0, delayMs);
	return Effect.runFork(
		Effect.sleep(delay).pipe(
			Effect.andThen(runTask(fn)),
			Effect.forever,
			Effect.asVoid
		)
	);
};

export const clearScheduledInterval = (handle: IntervalHandle | null | undefined): void =>
	clearScheduledTimeout(handle);

export const createDebouncer = (delayMs: number): {
	trigger: (fn: () => void) => void;
	cancel: () => void;
} => {
	let handle: TimeoutHandle | null = null;

	return {
		trigger: (fn) => {
			clearScheduledTimeout(handle);
			handle = scheduleTimeout(() => {
				handle = null;
				fn();
			}, delayMs);
		},
		cancel: () => {
			clearScheduledTimeout(handle);
			handle = null;
		}
	};
};

export const dispatchToast = (payload: ToastDispatch): void => toastDispatchService.dispatch(payload);
export const onToastDispatch = (listener: (payload: ToastDispatch) => void): Cleanup =>
	toastDispatchService.listen(listener);
