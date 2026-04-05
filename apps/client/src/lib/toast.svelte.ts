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

	function add(kind: ToastKind, message: string, duration = kind === "error" ? 6000 : 4000): string {
		const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
		toasts = [...toasts, { id, kind, message, duration }];

		if (duration > 0) {
			setTimeout(() => remove(id), duration);
		}

		return id;
	}

	function remove(id: string): void {
		toasts = toasts.filter((t) => t.id !== id);
	}

	function clear(): void {
		toasts = [];
	}

	return {
		get list() { return toasts; },
		error: (msg: string, duration?: number) => add("error", msg, duration),
		ok:    (msg: string, duration?: number) => add("ok",    msg, duration),
		warn:  (msg: string, duration?: number) => add("warn",  msg, duration),
		remove,
		clear
	};
}

export const toast = createToastStore();
