import {
	clearScheduledTimeout,
	dispatchToast,
	onToastDispatch,
	scheduleTimeout,
	type TimeoutHandle
} from "$lib/client/browser";

export type ToastKind = "error" | "ok" | "warn";

export interface Toast {
	id: string;
	kind: ToastKind;
	message: string;
	/** Auto-dismiss after ms. 0 = sticky (manual dismiss only). */
	duration: number;
}

function createToastStore() {
	let toasts = $state<Toast[]>([]);
	const timers = new Map<string, TimeoutHandle | null>();

	const add = (toast: Toast): void => {
		toasts = [...toasts, toast];

		if (toast.duration > 0) {
			const handle = scheduleTimeout(() => remove(toast.id), toast.duration);
			timers.set(toast.id, handle);
		}
	};

	const createToast = (kind: ToastKind, message: string, duration = kind === "error" ? 6000 : 4000): Toast => {
		const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
		return { id, kind, message, duration };
	};

	onToastDispatch((payload) => {
		add(payload);
	});

	function push(kind: ToastKind, message: string, duration = kind === "error" ? 6000 : 4000): string {
		const toast = createToast(kind, message, duration);
		dispatchToast(toast);
		return toast.id;
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
		error: (msg: string, duration?: number) => push("error", msg, duration),
		ok:    (msg: string, duration?: number) => push("ok", msg, duration),
		warn:  (msg: string, duration?: number) => push("warn", msg, duration),
		remove,
		clear
	};
}

export const toast = createToastStore();
