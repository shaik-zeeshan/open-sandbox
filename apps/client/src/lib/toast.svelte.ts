import {
	clearScheduledTimeout,
	dispatchToast,
	onToastDispatch,
	scheduleTimeout,
	type TimeoutHandle
} from "$lib/client/browser";

export type ToastKind = "error" | "ok" | "warn" | "loading";

export interface Toast {
	id: string;
	kind: ToastKind;
	message: string;
	/** Auto-dismiss after ms. 0 = sticky (manual dismiss only). */
	duration: number;
}

export interface ToastStore {
	readonly list: Toast[];
	error: (msg: string, duration?: number) => string;
	ok: (msg: string, duration?: number) => string;
	warn: (msg: string, duration?: number) => string;
	loading: (msg: string, duration?: number) => string;
	update: (id: string, kind: ToastKind, message: string, duration?: number) => void;
	remove: (id: string) => void;
	clear: () => void;
}

function createToastStore(): ToastStore {
	let toasts = $state<Toast[]>([]);
	const timers = new Map<string, TimeoutHandle | null>();

	const add = (toast: Toast): void => {
		toasts = [...toasts, toast];

		if (toast.duration > 0) {
			const handle = scheduleTimeout(() => remove(toast.id), toast.duration);
			timers.set(toast.id, handle);
		}
	};

	const createToast = (kind: ToastKind, message: string, duration = kind === "error" ? 6000 : kind === "loading" ? 0 : 4000): Toast => {
		const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
		return { id, kind, message, duration };
	};

	onToastDispatch((payload) => {
		add(payload);
	});

	function push(kind: ToastKind, message: string, duration = kind === "error" ? 6000 : kind === "loading" ? 0 : 4000): string {
		const toast = createToast(kind, message, duration);
		dispatchToast(toast);
		return toast.id;
	}

	function update(id: string, kind: ToastKind, message: string, duration = kind === "error" ? 6000 : 4000): void {
		const existing = timers.get(id);
		if (existing !== undefined) {
			clearScheduledTimeout(existing);
			timers.delete(id);
		}
		const idx = toasts.findIndex((t) => t.id === id);
		if (idx === -1) {
			push(kind, message, duration);
			return;
		}
		const updated: Toast = { id, kind, message, duration };
		toasts = [...toasts.slice(0, idx), updated, ...toasts.slice(idx + 1)];
		if (duration > 0) {
			const handle = scheduleTimeout(() => remove(id), duration);
			timers.set(id, handle);
		}
	}

	function remove(id: string): void {
		const handle = timers.get(id);
		if (handle !== undefined) {
			clearScheduledTimeout(handle);
			timers.delete(id);
		}

		toasts = toasts.filter((t) => t.id !== id);
	}

	function clear(): void {
		for (const handle of timers.values()) {
			clearScheduledTimeout(handle);
		}
		timers.clear();
		toasts = [];
	}

	return {
		get list() { return toasts; },
		error:   (msg: string, duration?: number) => push("error", msg, duration),
		ok:      (msg: string, duration?: number) => push("ok", msg, duration),
		warn:    (msg: string, duration?: number) => push("warn", msg, duration),
		loading: (msg: string, duration = 0) => push("loading", msg, duration),
		update,
		remove,
		clear
	};
}

export const toast = createToastStore();
