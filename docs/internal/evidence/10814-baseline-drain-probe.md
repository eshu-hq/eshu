# #10814 NVD baseline drain-probe EXPLAIN evidence

Refs #10814 (arbiter ruling in the issue comments: the probe adds no index,
proved by an EXPLAIN on at least 50,000 analysed rows from the real enqueue
path, on a query derived from the shipped constant, with `collector_kind =`
on the workflow-item leg). Related: #10728 (pacing design), #10746 (service
wiring), #10817 (the QA preflight owns the QA EXPLAIN half). Environment
identifiers are redacted per #7803.

This note was re-created 2026-11-23 after #10990 (part of #9315) pruned the
planner-slice note with 222 other unanchored evidence files. Its anchor is the
`nvdBaselineDrainProbeTimeout` godoc on `NVDBaselineDrainProbeQuery`, which
points at "the evidence note for #10814".

## What was EXPLAINed

The shipped constant `NVDBaselineDrainProbeQuery`
(`go/internal/storage/postgres/vulnerability/nvd_baseline_drain_probe.go`),
verbatim, never a copy, through the committed live test
`TestNVDBaselineDrainProbePlanUsesKeyedIndexes`. Each probe case runs under
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` twice: once as a custom plan with
literal binds, once as a forced generic plan (`plan_cache_mode =
force_generic_plan`, `PREPARE ... ($3)` carrying `collector_kind`). The test
fails on any Seq Scan, on any index read outside the allowed keyed set, on
more than one index search per scan, and on any Index Cond that does not bind
its key columns; it also asserts a shared-buffer bound of 96 per execution.

## Data shape

Disposable PostgreSQL with the full production bootstrap (363 migrations),
fixture built 2026-11-23, `ANALYZE` on all four tables before probing. Queue
rows come from the real enqueue paths (`ProjectorQueue.Enqueue`, the
production `supply_chain_impact` intent builder through
`ReducerQueue.Enqueue`, `WorkflowControlStore.EnqueueWorkItems`); scopes and
generations are bulk-inserted and terminal statuses applied in bulk afterwards
with a production-like skew, because driving tens of thousands of claim/ack
cycles adds nothing to a plan proof. The test refuses a fixture under 50,000
`fact_work_items`, 50,000 `workflow_work_items`, or thousands of
`scope_generations`. Measured:

| Table | Rows | How it was built |
|---|---|---|
| `fact_work_items` | 52,905 | projector items via real `ProjectorQueue.Enqueue`, `supply_chain_impact` reducer items via the production intent builder and real `ReducerQueue.Enqueue`, plus other-domain reducer items |
| `workflow_work_items` | 51,452 | collector items plus registry items via real `EnqueueWorkItems` |
| `scope_generations` | 31,454 | git plus NVD baseline generations, including same-scope successors (superseded-by-active, pending, failed) |

Six chunk states, each under a custom and a forced generic plan:

| Chunk state | Custom: shared buffers, server ms | Generic: shared buffers, server ms |
|---|---|---|
| named generation superseded | 39, 0.745 | 34, 0.163 |
| failed successor | 28, 0.721 | 23, 0.130 |
| pending successor | 43, 0.224 | 35, 0.119 |
| drained chunk | 39, 0.449 | 34, 0.128 |
| reducer item open | 39, 0.202 | 34, 0.112 |
| scope absent | 15, 0.276 | 15, 0.110 |

Every scan is a keyed index scan on an allowed index: `fact_work_items`
through `fact_work_items_scope_generation_idx` on `(scope_id, generation_id)`
(three legs: projector, reducer, newest-projector),
`workflow_work_items` through
`workflow_work_items_collector_scope_generation_updated_idx` with
`collector_kind` leading (`collector_kind = 'vulnerability_intelligence'` on
the custom plan, `collector_kind = $3` on the generic plan, both with
`scope_id` and `generation_id` bound), `scope_generations` through
`scope_generations_active_scope_idx` on `scope_id` (active leg),
`scope_generations_scope_generation_idx` on `(scope_id, generation_id)`
(named leg), `scope_generations_scope_idx` on `(scope_id, status)` (pending
leg), and `scope_generations_scope_latest_lookup_idx` on `scope_id` with
`LIMIT 1` (newest leg). No Seq Scan anywhere; at most one index search per
scan. All twelve executions stay under the 96-buffer bound (worst case 43).

The whole probe call (begin, read-only, timeout, query, rollback) over 1,200
calls on that fixture: p50 0.686 ms, p99 1.088 ms, max 17.622 ms. Logged by
the test, never asserted: wall time on a shared host is evidence, not a gate.

## Machine

Local disposable PostgreSQL 18.7 (Alpine, aarch64) in Docker, `shared_buffers`
128 MB, `fsync` on, `effective_cache_size` 4 GB, full 363-migration bootstrap
per proof database, dropped at test cleanup. Host darwin/arm64 (Apple M5 Max).
Server times are one run on a shared laptop, so read them as an order of
magnitude; buffer counts are the stable claim.

## No-Regression Evidence

The probe is a new read-only path and changes no worker, lease, claim, batch,
or queue write. It reads `scope_generations`, `workflow_work_items`, and
`fact_work_items` by key in a read-only transaction with no row locks under a
transaction-local 3 second statement timeout, so only an ACCESS EXCLUSIVE
table lock can hold it up, and then it fails closed as
`ErrNVDBaselineDrainTimeout`. Load is at most one probe per incomplete year
with a checkpoint per planner tick (25 for a 2026 run). No index was added:
every leg already had its keyed index, and the planner's choice of
`scope_generations_scope_generation_idx` over the primary key for the named
read is pinned by the same test.

## Observability Evidence

None new. The probe emits no metric, span, or log of its own; the timeout
returns the typed `ErrNVDBaselineDrainTimeout` for the #10746 service to count
and log when it loads the drain map, and the hold reasons page through the
planner's existing `eshu_dp_nvd_baseline_plan_holds_total{reason}` counter.

## Explicitly out of scope: the QA EXPLAIN

The ruling's second half — one read-only EXPLAIN of the probe on QA —
needs AWS login and belongs to the #10817 enable preflight, which the
coordinator owns. Nothing in this note ran against a QA environment.
