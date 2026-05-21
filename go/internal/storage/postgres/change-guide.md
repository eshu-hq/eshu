# storage/postgres Change Guide

Use this guide when changing Postgres stores, queues, schema definitions,
drift loaders, AWS status stores, or status/readiness queries.

## Read First

- `README.md` for the package boundary and store groups.
- `db.go` for `ExecQueryer`, `Queryer`, `Transaction`, `Beginner`, `SQLDB`, and
  `SQLTx`.
- `schema.go` and table-specific schema files before adding DDL.
- `facts.go` before changing fact insert, scan, filtering, or generation
  commit behavior.
- `projector_queue.go`, `projector_queue_sql.go`, `reducer_queue.go`, and
  `reducer_queue_sql.go` before changing queue semantics.
- `status_queries.go` and `status_registry.go` before changing admin status.
- AWS-specific stores before touching checkpoint, scan-status, freshness, or
  drift read-model behavior.

## Change Checklist

- Add a store by implementing against `ExecQueryer`, adding a `New*Store`
  constructor, adding idempotent `*SchemaSQL()` when the store owns a table,
  registering the table in `BootstrapDefinitions`, and wrapping with
  `InstrumentedDB` in command wiring.
- Add a table by placing its `Definition` after referenced tables, keeping DDL
  idempotent, adding SQL mirror tests when applicable, and adding bootstrap
  tests.
- Add a fact column or fact kind by updating the insert column list, scanner,
  schema DDL, and migration plan for non-nullable data.
- Add a queue domain by adding the reducer domain constant, extending claim
  filtering, and testing claim, ack, fail, retry, and stale-lease behavior.
- Add an enqueue-only reducer call site by using `ReducerQueue{db: s.db}` when
  only inserts are needed. Do not invent a parallel enqueuer port; the
  projector-facing narrow interface already exists.
- Add a graph projection phase by adding the reducer phase constant, storing it
  through `GraphProjectionPhaseStateStore`, and adding a readiness lookup when
  reducer domains gate on it.
- Add Postgres telemetry by wrapping the connection with `InstrumentedDB` and a
  bounded `StoreName`. Do not add query text, paths, ARNs, fact IDs, or
  resource names to metric labels.

## Queue Rules

- Keep projector claim ordering scoped by `scope_id`. A worker must not skip a
  locked older same-scope row and start a newer generation.
- Keep expired claimed/running rows ahead of ordinary pending rows so stale
  leases are reclaimed.
- Keep stale duplicate reclaim and same-scope supersession together. Removing
  either can make local polling look stalled or write obsolete graph state.
- Stop processing on `ErrProjectorClaimRejected`, `ErrReducerClaimRejected`, or
  `ErrWorkflowClaimRejected`. These errors mean the current worker no longer
  owns the claim.
- Do not hold transactions across graph writes, AWS calls, HTTP calls, or other
  network work.

## Drift Rules

- State-side flattening and parser-side HCL attribute encoding must stay
  byte-identical. Change `flattenStateAttributes` and
  `ctyValueToDriftString` together and add cross-package regression coverage.
- Use `path.Clean`, `path.Dir`, and `path.Join` for module-prefix logic.
  Postgres stores forward-slash parser paths, not OS filesystem paths.
- Keep `maxModulePrefixDepth = 10` bounded unless a performance-backed design
  changes the traversal contract.
- Prior-config walking must use the prior generation's module-prefix map.
  Current-generation prefixes cannot explain renamed module blocks in older
  config.
- Multi-element repeated nested blocks are intentionally first-wins on both
  parser and state sides and must stay observable through debug logs.

## AWS Store Rules

- AWS checkpoint primary keys remain scoped to collector instance, account,
  region, service, resource parent, and operation.
- Checkpoint and scan-status writes must keep fencing guards so stale AWS
  workers cannot overwrite newer claim state.
- AWS runtime drift reads stay scoped to one AWS generation plus the current ARN
  allowlist. Do not scan all active Terraform state to discover possible
  matches.
- AWS drift finding reads must remain active-generation, scope/account bounded,
  wildcard-safe, and capped before querying.

## Failure Modes

- High queue claim latency: check `fact_work_items` index coverage and
  `FOR UPDATE SKIP LOCKED` contention.
- Duplicate running projector rows for one scope: check oldest-ready selection,
  expired-lease priority, and stale duplicate reclaim.
- Dead-letter growth: inspect `failure_class`; replay only after root cause is
  fixed.
- Missing graph readiness rows: inspect `GraphProjectionPhaseRepairQueueStore`
  and projector `publish_phases` failures.
- `SQLSTATE 21000`: duplicate `fact_id` entered one batch; verify
  `deduplicateEnvelopes` still runs.
- `SQLSTATE 22P05` or `SQLSTATE 22P02`: payload sanitization was bypassed or a
  fact carries unsupported binary/control-byte content.

## Do Not Change Without A Current Design Record

- `fact_work_items` schema, conflict keys, lifecycle states, or claim ordering.
- `graph_projection_phase_state` schema or phase semantics.
- `ReducerQueue.Claim` NornicDB semantic gate and activation condition.
- `BootstrapDefinitions` ordering for foreign-key-dependent tables.
- Content writer batch concurrency caps without redoing Postgres pool-budget
  math.
- Shared projection intent identity fields.
