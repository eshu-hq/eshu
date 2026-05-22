# AGENTS.md - storage/postgres guidance for LLM assistants

`storage/postgres` is Eshu's durable coordination layer. Treat changes here as
runtime and concurrency work, even when the diff looks like a small SQL helper.

Accuracy, performance, and concurrency are mandatory. A queue, lease, schema,
status, drift, or recovery change is not ready until its correctness path,
contention behavior, and operator signals are proven.

## Read first

Read these before editing this package:

1. `go/internal/storage/postgres/README.md` - package boundary, store groups,
   queue lifecycle, telemetry, and verification.
2. `go/internal/storage/postgres/change-guide.md` - change routing, queue
   rules, drift rules, AWS store rules, and failure modes.
3. `go/internal/storage/postgres/doc.go` or `go doc ./internal/storage/postgres`
   - godoc contract for exported stores and helper surfaces.
4. `go/internal/storage/postgres/db.go` - `ExecQueryer`, `Queryer`,
   `Transaction`, `Beginner`, `SQLDB`, and `SQLTx`.

Then read the owning files for the path you touch:

| Area | Read |
| --- | --- |
| Projector queue | `projector_queue.go`, `projector_queue_sql.go` |
| Reducer queue | `reducer_queue.go`, `reducer_queue_batch.go`, `reducer_queue_helpers.go` |
| Facts and generations | `facts.go`, `generation_freshness.go`, `ingestion.go` |
| Schema/bootstrap | `schema.go` and the table-specific `*_schema*.go` file |
| Status/readiness | `status.go`, `status_queries.go`, `status_registry.go` |
| Shared projection | `shared_intents*.go`, `shared_projection_acceptance.go` |
| Workflow/webhooks | `workflow_control*.go`, `webhook_trigger_store*.go` |
| AWS stores | `aws_*`, `status_aws_*`, and `aws_cloud_runtime_drift_*` files |
| Terraform drift | `tfstate_drift_evidence*.go`, `tfstate_backend*.go` |
| Content | `content_writer*.go`, `content_store*.go` |

## Invariants

- **Idempotency is mandatory.** Insert paths use `ON CONFLICT DO NOTHING` or
  `ON CONFLICT DO UPDATE`; schema DDL uses `IF NOT EXISTS`. Do not add
  non-idempotent writes.
- **Facts dedupe before batching.** `upsertFacts` calls
  `deduplicateEnvelopes` before batch insert so duplicate `fact_id` values do
  not trigger `SQLSTATE 21000`.
- **JSONB sanitization is mandatory.** Fact inserts pass through
  `sanitizeJSONB` so binary, control-byte, or non-UTF-8 payloads do not poison
  Postgres writes.
- **Generation freshness checks include pending and active work.**
  `CommitScopeGeneration` must compare incoming freshness against the newest
  same-scope pending or active generation. Failed generations remain retryable.
- **Projector ack is atomic.** `ProjectorQueue.Ack` supersedes stale active
  generation, activates the target generation, updates the scope pointer, and
  marks work succeeded in one transaction. The queue must be constructed with a
  `Beginner` such as `SQLDB` or `InstrumentedDB`.
- **Lease fences are terminal for the current worker.** On
  `ErrProjectorClaimRejected`, `ErrReducerClaimRejected`, or
  `ErrWorkflowClaimRejected`, stop processing. Do not retry the ack/fail as the
  stale owner.
- **Projector claim ordering preserves one active generation per scope.** Keep
  oldest-ready selection, `FOR UPDATE SKIP LOCKED`, expired-lease priority,
  duplicate-lease reclaim, same-scope supersession, and heartbeat supersession
  aligned.
- **Reducer queue NornicDB gates are narrow and intentional.** The
  `semantic_entity_materialization` gate and reducer graph-drain gate protect
  local NornicDB projection from unsafe overlap. Do not bypass them.
- **Status includes shared projection backlog.** `/admin/status` must count
  pending `shared_projection_intents` and active shared projection leases so it
  does not report healthy before reducer-owned graph edges are visible.
- **Schema order follows foreign keys.** A table definition with references
  must appear after the referenced table in `BootstrapDefinitions`.
- **AWS checkpoint and scan status writes are fenced.** Keep fencing-token
  guards so stale workers cannot overwrite newer claim state.
- **AWS runtime drift stays scoped.** Load AWS rows from one
  `(scope_id, generation_id)` and join Terraform state only through the current
  AWS ARN allowlist. Unknown or ambiguous backend ownership is not proof of
  absent configuration.
- **AWS drift finding reads stay bounded.** Require valid scope/account filters,
  validate wildcard-capable values before building `LIKE` prefixes, stay on the
  active generation, and cap list reads before querying.
- **Terraform drift address strings stay byte-identical across parser and state.**
  `flattenStateAttributes` / `coerceJSONString` must match
  `ctyValueToDriftString` exactly. Add cross-package tests for encoding
  changes.
- **Module-prefix logic uses forward-slash paths.** Use `path.Clean`,
  `path.Dir`, and `path.Join`, not `path/filepath`; these are parser-normalized
  Postgres strings, not local filesystem paths.
- **Content writer concurrency stays within the Postgres pool budget.** Peak
  demand is roughly `ESHU_PROJECTOR_WORKERS * batch_concurrency` plus collector,
  status, and heartbeat connections.

## Change routing

- **New store:** implement against `ExecQueryer`; add `New*Store(db
  ExecQueryer)`; add idempotent schema SQL when the store owns a table; register
  the table in `BootstrapDefinitions`; wrap with `InstrumentedDB` in `cmd/`
  wiring.
- **Schema change:** update the DDL, definition ordering, bootstrap tests,
  migrations when needed, recovery/status surfaces if queue or fact state moves,
  and docs that describe the contract.
- **Fact kind or column:** update insert columns, `columnsPerFactRow`,
  scanners, schema DDL, and migration/default behavior.
- **Projector or reducer queue change:** map claim ordering, transaction scope,
  retry scope, lease owner checks, stale-lease behavior, supersession behavior,
  and dead-letter behavior before editing. Tests must cover claim, heartbeat,
  ack/fail, retry, stale lease, and duplicate delivery.
- **Reducer enqueue-only call site:** use `ReducerQueue{db: s.db}` when only
  inserts are needed. Do not invent a parallel enqueuer port; the projector
  already consumes the narrow `projector.ReducerIntentWriter` interface.
- **Graph projection phase:** add the reducer phase constant, persist it through
  `GraphProjectionPhaseStateStore`, and add readiness lookup coverage when a
  reducer domain gates on it.
- **Terraform drift:** keep config row building, state row flattening, prior
  config walking, module-prefix mapping, and reducer env parsing in sync. When
  changing prior depth semantics, update `defaultPriorConfigDepth` and
  `parsePriorConfigDepth`.
- **AWS checkpoint/status/drift:** keep keys scoped, fenced, and bounded; keep
  account, region, ARN, resource parent, and page-token values out of metric
  labels.
- **Telemetry:** prefer wrapping the DB with `InstrumentedDB` and a bounded
  `StoreName`. Do not put query text, paths, ARNs, fact IDs, resource names, or
  symbols into metric labels.

## Failure modes

- High `eshu_dp_postgres_query_duration_seconds{store="queue"}` means claim
  contention or missing `fact_work_items` index coverage until proven
  otherwise.
- Duplicate running projector rows for one scope means oldest-ready selection,
  expired-lease priority, or stale duplicate reclaim may be broken.
- `ErrProjectorClaimRejected`, `ErrReducerClaimRejected`, or
  `ErrWorkflowClaimRejected` means the worker lost ownership.
- `dead_letter` growth requires `failure_class` investigation before replay
  through `RecoveryStore`.
- Missing `graph_projection_phase_state` rows usually means the projector
  `publish_phases` stage failed or the repair queue has not drained.
- `SQLSTATE 21000` on fact insert means duplicate `fact_id` values reached one
  batch.
- `SQLSTATE 22P05` or `SQLSTATE 22P02` on fact insert means JSONB sanitization
  was bypassed or a payload carried unsupported binary/control-byte content.

## Anti-patterns

- Do not hold Postgres transactions across graph writes, AWS calls, HTTP calls,
  filesystem work, or other network work.
- Do not use raw SQL string building for caller-controlled values. Use
  parameterized queries.
- Do not add `if backend == "nornicdb"` branches throughout this package.
  Backend-sensitive queue behavior belongs in the existing parameterized reducer
  queue gate pattern.
- Do not bypass `deduplicateEnvelopes` or `sanitizeJSONB`.
- Do not skip workflow, projector, reducer, AWS checkpoint, or AWS scan-status
  fencing checks.
- Do not raise `contentWriterBatchConcurrencyAutoCap` or
  `contentWriterBatchConcurrencyCap` without redoing Postgres pool-budget math
  and recording performance evidence.
- Do not reintroduce per-call `os.Getenv` reads in `ContentWriter`; env override
  resolution happens in `NewContentWriter`.
- Do not assert positional `db.execs[i]` order on concurrent entity-batch
  tests. Assert sorted batch sizes or query shapes.
- Do not diverge parser-side and state-side Terraform attribute encoding.
- Do not remove `EXECUTES`, shared projection backlog, freshness, or
  supersession behavior from one side of the contract without updating all
  readers and tests.

## Do not change without a current design record

- `fact_work_items` schema, lifecycle states, conflict keys, claim ordering, or
  recovery semantics.
- `graph_projection_phase_state` schema or phase semantics.
- `ReducerQueue.Claim` NornicDB semantic gate or activation condition.
- `BootstrapDefinitions` ordering for foreign-key-dependent tables.
- Shared projection intent identity fields.
- Terraform drift dot-path encoding or prior-config window semantics.
- Content writer concurrency caps.

## Required proof

- Run `go test ./internal/storage/postgres -count=1` for package changes.
- Run focused tests for the touched store or queue path before the full package
  test.
- Run `go run ./cmd/eshu docs verify ../go/internal/storage/postgres --limit
  1000 --fail-on contradicted,missing_evidence` for docs changes.
- Queue, lease, worker, batching, or runtime config changes also need the
  performance-evidence gate and a tracked evidence note, per root guidance.
