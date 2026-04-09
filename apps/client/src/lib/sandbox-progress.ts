import type { SandboxOperationProgress } from "$lib/api";

export type SandboxProgressTone = "active" | "ok" | "error";

export type SandboxProgressDisplay = {
	phase: string;
	phaseLabel: string;
	status: string;
	statusLabel: string;
	message: string;
	detail: string;
	tone: SandboxProgressTone;
};

const titleCase = (value: string): string =>
	value
		.split(/[-_\s]+/)
		.filter(Boolean)
		.map((part) => part.charAt(0).toUpperCase() + part.slice(1))
		.join(" ");

export const formatSandboxProgress = (
	progress: SandboxOperationProgress
): SandboxProgressDisplay => {
	const phase = progress.phase.trim();
	const status = progress.status.trim();
	const message = progress.message.trim();
	const phaseLabel = phase.length > 0 ? titleCase(phase) : "Working";
	const statusLabel = status.length > 0 ? titleCase(status) : "Running";
	const normalizedStatus = status.toLowerCase();
	const tone: SandboxProgressTone = /(error|fail)/.test(normalizedStatus)
		? "error"
		: /(ok|done|complete|success)/.test(normalizedStatus)
			? "ok"
			: "active";

	return {
		phase,
		phaseLabel,
		status,
		statusLabel,
		message,
		detail: message.length > 0 ? message : `${phaseLabel} · ${statusLabel}`,
		tone
	};
};

export const formatSandboxProgressToast = (
	label: string,
	progress: SandboxOperationProgress
): string => {
	const display = formatSandboxProgress(progress);
	return `${label}: ${display.detail}`;
};
