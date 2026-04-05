# Phase 5 Small-Cluster Protocol

## Goal

Phase 5 introduces a control-plane to worker boundary without changing the default deployment shape.

- Default mode remains one server process on one host.
- Execution remains Docker-based.
- Kubernetes is out of scope.
- The new boundary is optimized for a future small set of trusted worker hosts, not fleet-scale orchestration.

## Current Architecture

- The Go server is the control plane.
- Runtime actions are dispatched through a worker-aware runtime interface.
- The only built-in execution backend today is the `local` worker, which executes against the local Docker daemon.
- Future remote workers can register with the control plane and implement the same execution contract.

## Ownership Model

Each managed workload has an owning worker.

- Sandboxes persist `worker_id` in the `sandboxes` table.
- Managed direct containers carry `open-sandbox.worker_id` as runtime metadata.
- If no worker is specified, ownership defaults to `local`.
- The control plane must route lifecycle, exec, file, log, and terminal operations to the owning worker.
- Ownership is sticky for the life of the runtime instance. Re-scheduling is a future control-plane operation, not an implicit side effect.

This model keeps single-server behavior unchanged while making cross-host delegation explicit.

## Data Model

### `sandboxes`

Added field:

- `worker_id TEXT NOT NULL DEFAULT 'local'`

Meaning:

- Identifies which worker currently owns the sandbox runtime instance.
- Does not change the public sandbox ID.

### `runtime_workers`

New table used by the control plane to track available workers.

- `id`: stable worker identifier
- `name`: display/debug name
- `advertise_address`: worker-reported control address for future remote execution
- `execution_mode`: current execution adapter, currently `docker`
- `status`: worker-reported health state
- `version`: worker software/build identifier
- `labels_json`: small placement/capability metadata map
- `registered_at`: first registration timestamp
- `last_heartbeat_at`: last heartbeat observed by control plane
- `heartbeat_ttl_seconds`: expected heartbeat lease window
- `updated_at`: last metadata update

## Registration And Heartbeat Protocol

These routes are control-plane routes, not end-user routes.

Authentication:

- Header: `X-Open-Sandbox-Worker-Token`
- Secret source: `SANDBOX_WORKER_SHARED_SECRET`

If the shared secret is unset, worker registration is disabled and single-server mode still works.

### Register Worker

`POST /control/workers/register`

Request body:

```json
{
  "worker_id": "worker-2",
  "name": "worker-2",
  "advertise_address": "http://10.0.0.2:8080",
  "execution_mode": "docker",
  "version": "v1",
  "heartbeat_ttl_seconds": 20,
  "labels": {
    "zone": "lab"
  }
}
```

Behavior:

- Creates or updates the worker record.
- Marks the worker active.
- Stores the latest metadata and initial heartbeat timestamp.

### Heartbeat

`POST /control/workers/:id/heartbeat`

Request body:

```json
{
  "status": "active",
  "advertise_address": "http://10.0.0.2:9090",
  "version": "v2",
  "labels": {
    "zone": "lab"
  }
}
```

Behavior:

- Updates `last_heartbeat_at`.
- Allows the worker to refresh status and metadata.
- Returns the stored worker record as acknowledged by the control plane.

## Runtime Delegation Contract

The runtime layer now accepts a `worker_id` for execution-oriented actions:

- inspect workload
- create/start/stop/remove workload
- create volume
- exec / terminal
- logs
- file copy in/out
- reset

Today the control plane resolves `worker_id` and dispatches to the local Docker worker backend.

That means:

- single-server mode still runs exactly on the local Docker host
- the execution hop is explicit in code now
- a future remote worker backend can be added without changing sandbox lifecycle handlers again

## Admin Visibility

Admin users can inspect known workers through:

- `GET /api/admin/workers`

Responses include whether the worker is control-plane owned and whether an execution backend is currently reachable from this server process.

## Non-Goals In This Step

- distributed scheduling
- worker failover or fencing
- workload migration between workers
- remote terminal/log transport
- large-cluster orchestration features

Those stay out of scope until the small-cluster control-plane contract needs them.
