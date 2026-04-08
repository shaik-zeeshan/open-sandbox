const parseUrl = (value: string): URL | null => {
	if (!URL.canParse(value)) {
		return null;
	}

	return new URL(value);
};

export const isValidClientUrl = (value: string): boolean => parseUrl(value) !== null;

export const normalizeClientEndpointUrl = (value: string): string | null => {
	if (parseUrl(value) === null) {
		return null;
	}

	return value.replace(/\/$/, "");
};
