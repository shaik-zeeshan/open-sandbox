import { Schema } from "effect";

import { deleteRequest, getRequest, postJsonRequest } from "../request";
import {
	type CreateAPIKeyRequest,
	APIKeyResponseSchema,
	CreateAPIKeyResponseSchema,
	RevokeAPIKeyResponseSchema
} from "../schemas";
import { execute } from "../transport";

export const listApiKeys = () =>
	execute(getRequest("/api/api-keys"), Schema.Array(APIKeyResponseSchema));

export const createApiKey = (request?: CreateAPIKeyRequest) =>
	execute(postJsonRequest("/api/api-keys", request ?? {}), CreateAPIKeyResponseSchema);

export const revokeApiKey = (id: string) =>
	execute(deleteRequest(`/api/api-keys/${encodeURIComponent(id)}`), RevokeAPIKeyResponseSchema);
