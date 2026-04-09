import { Schema } from "effect";

import { getRequest, postJsonRequest } from "../request";
import {
	type MaintenanceCleanupRequest,
	type MaintenanceReconcileRequest,
	MaintenanceCleanupResponseSchema,
	MaintenanceReconcileResponseSchema,
	TraefikRouteStateResponseSchema,
	WorkerResponseSchema
} from "../schemas";
import { execute } from "../transport";

export const listWorkers = () =>
	execute(getRequest("/api/admin/workers"), Schema.Array(WorkerResponseSchema));

export const getTraefikRouteState = () =>
	execute(getRequest("/api/admin/traefik/routes"), TraefikRouteStateResponseSchema);

export const runMaintenanceCleanup = (request: MaintenanceCleanupRequest = {}) =>
	execute(
		postJsonRequest("/api/admin/maintenance/cleanup", request),
		MaintenanceCleanupResponseSchema
	);

export const runMaintenanceReconcile = (request: MaintenanceReconcileRequest = {}) =>
	execute(
		postJsonRequest("/api/admin/maintenance/reconcile", request),
		MaintenanceReconcileResponseSchema
	);
