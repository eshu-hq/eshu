# internal/storage/postgres Agent Instructions

These rules are mandatory for this package. Treat every change here as runtime,
concurrency, recovery, and operator-observability work.

## Read First

1. `README.md`, `change-guide.md`, and `doc.go`.
2. `db.go` for database and transaction interfaces.
3. The owning store files for the path you touch: facts, queues, schema,
   status, recovery, drift, AWS, content, shared projection, workflow, or
   webhooks.
4. `docs/public/reference/local-testing.md` and
   `docs/public/reference/telemetry/index.md` before runtime-affecting changes.

## Local Rules

- Writes must be idempotent. Use `ON CONFLICT` for data and `IF NOT EXISTS` for
  schema DDL.
- Fact inserts must deduplicate envelopes before batching and sanitize JSONB
  payloads before insert.
- `CommitScopeGeneration` must compare freshness against newest same-scope
  pending or active generation. Failed generations stay retryable.
- Queue claim, heartbeat, ack, fail, replay, supersession, stale reclaim, and
  dead-letter behavior must remain retry-safe and lease/fence-aware.
- `ProjectorQueue.Ack` must keep supersede, activate, scope-pointer update, and
  work success in one transaction.
- Stop processing on `ErrProjectorClaimRejected`, `ErrReducerClaimRejected`, or
  `ErrWorkflowClaimRejected`; the worker no longer owns the claim.
- Projector claims must preserve same-scope ordering, oldest-ready selection,
  expired-lease priority, `FOR UPDATE SKIP LOCKED`, stale duplicate reclaim,
  supersession, and heartbeat semantics.
- Reducer NornicDB gates are narrow scheduling controls. Do not bypass semantic
  materialization or graph-drain gates.
- `/admin/status` must include fact, shared-projection, active-lease, retry, and
  dead-letter signals so healthy never hides pending graph truth.
- `BootstrapDefinitions` must stay ordered by foreign-key dependency.
- Terraform config/state drift strings must stay byte-identical between parser
  config rows and state flattening. Module-prefix logic uses forward-slash
  `path`, not `path/filepath`.
- Content writer concurrency must stay inside the Postgres pool budget.
- Runtime paths must use existing telemetry wrappers and must not put paths,
  ARNs, fact IDs, resource names, or payload bodies in metric labels.

## Change Gates

- Schema changes require DDL, bootstrap order, mirror tests when applicable,
  migration/default behavior, and status/recovery/query docs when affected.
- Queue changes require a written claim-order, transaction-scope, retry,
  idempotency, stale-lease, supersession, and dead-letter argument before code.
- New stores use `ExecQueryer`, expose `New*Store`, add idempotent schema when
  they own tables, register bootstrap definitions, and wire `InstrumentedDB`.
- Fact kind or column changes require insert/scanner/schema/test updates and
  storage compatibility review.
- Drift or AWS changes must stay bounded, fenced, and scoped by existing
  identity keys.

## Do Not Change Without Architecture-Owner Approval

- `fact_work_items` schema, lifecycle states, conflict keys, claim ordering, or
  recovery behavior.
- `graph_projection_phase_state` schema or phase semantics.
- ReducerQueue NornicDB gate activation.
- `BootstrapDefinitions` dependency order.
- Shared projection intent identity fields.
- Terraform drift dot-path encoding, locator-hash joins, or prior-config depth.
- Content writer concurrency caps.
