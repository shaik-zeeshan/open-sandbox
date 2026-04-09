import { Schema } from "effect";

import { deleteRequest, getRequest, postJsonRequest } from "../request";
import {
	type CreateUserRequest,
	ItemDeletedResponseSchema,
	UpdateUserPasswordResponseSchema,
	UserSummarySchema
} from "../schemas";
import { execute } from "../transport";

export const listUsers = () => execute(getRequest("/api/users"), Schema.Array(UserSummarySchema));

export const createUser = (request: CreateUserRequest) => execute(postJsonRequest("/api/users", request), UserSummarySchema);

export const updateUserPassword = (userId: string, password: string) =>
	execute(postJsonRequest(`/api/users/${encodeURIComponent(userId)}/password`, { password }), UpdateUserPasswordResponseSchema);

export const deleteUser = (userId: string) =>
	execute(deleteRequest(`/api/users/${encodeURIComponent(userId)}`), ItemDeletedResponseSchema);
