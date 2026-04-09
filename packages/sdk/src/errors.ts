import { Data } from "effect";

export class ApiError extends Data.TaggedError("ApiError")<{
	readonly status: number;
	readonly message: string;
	readonly body: unknown;
}> {}

export class NetworkError extends Data.TaggedError("NetworkError")<{
	readonly message: string;
	readonly cause: unknown;
}> {}

export class AuthError extends Data.TaggedError("AuthError")<{
	readonly message: string;
	readonly reason?: string;
}> {}

export type SdkError = ApiError | NetworkError | AuthError;

const isRecord = (value: unknown): value is Record<string, unknown> =>
	typeof value === "object" && value !== null;

export const extractMessage = (payload: unknown, fallback: string): string => {
	if (typeof payload === "string" && payload.trim().length > 0) {
		return payload;
	}

	if (isRecord(payload)) {
		const errorField = payload.error;
		if (typeof errorField === "string" && errorField.trim().length > 0) {
			return errorField;
		}

		const messageField = payload.message;
		if (typeof messageField === "string" && messageField.trim().length > 0) {
			return messageField;
		}
	}

	return fallback;
};

export const extractAuthReason = (payload: unknown): string | undefined => {
	if (!isRecord(payload)) {
		return undefined;
	}

	const reason = payload.reason;
	if (typeof reason === "string" && reason.trim().length > 0) {
		return reason.trim();
	}

	return undefined;
};

export const mapAuthReasonToMessage = (reason?: string): string => {
	switch (reason) {
		case "invalid_credentials":
			return "Invalid credentials.";
		case "refresh_token_missing":
		case "refresh_token_expired":
		case "refresh_token_invalid":
		case "token_expired":
			return "Unauthorized: your session expired. Please log in again.";
		case "token_missing":
			return "Unauthorized: missing token. Please log in.";
		case "token_invalid":
			return "Unauthorized: token is invalid. Please log in again.";
		default:
			return "Unauthorized: your session is missing or expired.";
	}
};

export const formatSdkError = (error: unknown): string => {
	if (isRecord(error) && typeof error._tag === "string") {
		switch (error._tag) {
			case "AuthError":
				return typeof error.message === "string" ? error.message : "Unauthorized.";
			case "ApiError": {
				const status = typeof error.status === "number" ? error.status : 0;
				const message = typeof error.message === "string" ? error.message.trim() : "";
				if (message.length > 0) {
					return status >= 400 && status < 600 && !message.includes(String(status))
						? `${status}: ${message}`
						: message;
				}
				return status > 0 ? `Server error ${status}.` : "Server returned an unexpected response.";
			}
			case "NetworkError": {
				const message = typeof error.message === "string" ? error.message.trim() : "";
				if (message.length === 0) {
					return "Network request failed.";
				}
				if (message.toLowerCase().includes("failed to fetch") || message.toLowerCase().includes("networkerror")) {
					return "Unable to reach the server. Check your connection or API endpoint.";
				}
				return message;
			}
		}
	}

	if (error instanceof Error) {
		return error.message;
	}

	return "An unexpected error occurred.";
};
