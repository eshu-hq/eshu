# AGENTS.md - storage/postgres

`storage/postgres` is Eshu's durable coordination layer. Treat every change as
runtime, concurrency, and recovery work, even when it looks like a small SQL
helper.

Accuracy, performance, and concurrency are mandatory.

## Read First

1. `README.md` - package boundary, store groups, telemetry, and verification.
2. `change-guide.md` - store routing, queue rules, drift rules, AWS rules, and
   failure modes.
3. `doc.go` or `go doc ./internal/storage/postgres`.
4. `db.go` - `ExecQueryer`, `Queryer`, `Transaction`, `Beginner`, `SQLDB`,
   and `SQLTx`.
5. The owning store files for the path you touch: queues, facts/generations,
   schema, status, shared projection, workflow/webhooks, AWS, drift, or content.

## Mandatory Invariants

- Writes MUST be idempotent. Use `ON CONFLICT` for data and `IF NOT EXISTS` for
  schema DDL.
- Fact inserts MUST call `deduplicateEnvelopes` before batching.
- Fact JSONB MUST pass through `sanitizeJSONB`.
- `CommitScopeGeneration` MUST compare incoming freshness against newest
  same-scope pending or active generation. Failed generations remain retryable.
- `ProjectorQueue.Ack` MUST keep supersede, activate, scope pointer update, and
  work success in one transaction.
- Lease/fence rejection is terminal for that worker. Stop on
  `ErrProjectorClaimRejected`, `ErrReducerClaimRejected`, or
  `ErrWorkflowClaimRejected`.
- Projector claims MUST preserve oldest-ready selection, `FOR UPDATE SKIP
  LOCKED`, expired-lease priority, duplicate reclaim, supersession, and
  heartbeat behavior.
- Reducer NornicDB gates are narrow and intentional. Do not bypass the semantic
  materialization gate or graph-drain gate.
- `/admin/status` MUST include shared projection backlog so healthy does not
  mean graph edges are still pending.
- `BootstrapDefinitions` MUST remain ordered by foreign-key dependency.
- AWS checkpoint, scan status, workflow, projector, and reducer writes MUST stay
  fenced.
- Terraform config/state drift strings MUST stay byte-identical between parser
  and state flattening.
- Module-prefix logic MUST use forward-slash `path`, not `path/filepath`.
- Content writer concurrency MUST stay within the Postgres pool budget.

## Change Routing

- New store: use `ExecQueryer`, add `New*Store`, add idempotent schema, register
  bootstrap definition, and wrap with `InstrumentedDB` in command wiring.
- Schema change: update DDL, bootstrap order, mirror tests, migration/default
  behavior, status/recovery readers when affected, and docs.
- Fact kind or column change: update insert columns, scanners, DDL, tests, and
  migration/default handling.
- Queue change: map claim order, transaction scope, retry scope, lease owner,
  stale-lease handling, supersession, duplicate delivery, and dead-letter
  behavior before editing.
- Reducer enqueue-only call site: use the existing `ReducerQueue`; do not add a
  parallel enqueuer.
- Graph projection phase: add reducer phase constant, persistence, readiness
  lookup, and tests for any gated reducer domain.
- Terraform drift or AWS drift: keep keys scoped, fenced, bounded, and free of
  high-cardinality metric labels.

## Anti-Patterns

- Do not hold Postgres transactions across graph writes, AWS calls, HTTP calls,
  filesystem work, or other network work.
- Do not build SQL from caller-controlled values.
- Do not spread backend-specific branches through this package.
- Do not bypass dedupe, JSONB sanitization, fences, or stale-owner checks.
- Do not raise content writer concurrency caps without pool-budget math and
  performance evidence.
- Do not reintroduce per-call env reads in `ContentWriter`.
- Do not assert positional execution order in concurrent batch tests.

## Do Not Change Without A Current Design Record

- `fact_work_items` schema, states, conflict keys, claim ordering, or recovery.
- `graph_projection_phase_state` schema or phase semantics.
- NornicDB reducer queue gate activation.
- `BootstrapDefinitions` foreign-key ordering.
- Shared projection intent identity fields.
- Terraform drift dot-path encoding or prior-config window semantics.
- Content writer concurrency caps.

## Required Proof

- Run focused tests for the touched store or queue path first.
- Run `go test ./internal/storage/postgres -count=1`.
- Run `go run ./cmd/eshu docs verify ../go/internal/storage/postgres --limit 1000 --fail-on contradicted,missing_evidence`
  for docs changes.
- Queue, lease, worker, batching, or runtime-config changes also require
  performance-evidence gates and tracked benchmark/observability evidence.
