# readiness/producer

Postgres implementation of `servicecatalog.RuntimeProducerReadiness` for the
#7816 service catalog runtime fence.

## Why it exists

The service catalog correlation handler builds each service's runtime
evidence from the correlated repository's `WorkloadInstance` nodes. Those
nodes are written by the repository's `workload_materialization` reducer work.
No queue ordering runs `service_catalog_correlation` after that work, so on a
first drain the handler could read zero instances just before the producer's
`MERGE` and commit an empty runtime family that nothing repairs. The handler
now asks this store first and defers while the producer is in flight.

## The statement

`RuntimeProducerStatesQuery` counts each requested scope's producer rows at
its **active** generation, by status, and returns
`newest_in_flight_workable_at`: the max over the pending, retrying, claimed,
and running rows only of `GREATEST(COALESCE(reopened_at, created_at),
COALESCE(last_attempt_at, created_at), COALESCE(visible_at, created_at))`
(NULL, read as zero, when none is in flight). The handler measures its
30-minute bound per repository from that timestamp.

A row's last-workable time is the latest of its cycle start (`created_at`, or
`reopened_at` after a reopen), its last claim (`last_attempt_at`), and its
scheduled visibility (`visible_at`, a retry's next attempt). The real queue
writes these columns this way:

| Queue step | Columns it moves |
| --- | --- |
| enqueue | `created_at` |
| claim | `last_attempt_at`, `claim_until`, `updated_at` |
| retry, counting or non-counting | `visible_at` and `next_attempt_at` (now plus backoff), `updated_at` |
| heartbeat | `claim_until`, `updated_at` |
| reopen of a succeeded row | `reopened_at`, `visible_at`, `updated_at` |

So a producer that keeps polling its own corpus fence moves `last_attempt_at`
and `visible_at` on every attempt and never reaches the bound, while a row no
worker has touched for 30 minutes does. A retry still sleeping anchors in the
future. The statement never reads `updated_at`, which heartbeats move.

| Producer rows at the active generation | Handler reads it as |
| --- | --- |
| any `pending`, `retrying`, `claimed`, or `running` | not ready: defer |
| only terminal rows | ready |
| none (no workloads, no active generation) | ready (`no_producer`) |

Rows of a newer pending generation or an older superseded generation never
match, because the join pins `work.generation_id = scope.active_generation_id`.
A repository id maps to the `git-repository-scope:<repo_id>` scope the git
collector registers. A scope with no row is absent from the result.

## Plan evidence

Performance Evidence: measured on a disposable Postgres 18 with all 196
numbered migrations applied and `fact_work_items` populated to 1,057,536 rows
(10,000 git scopes, 3 to 4 generations each, 34 reducer domains per
generation; status mix succeeded 785,163, dead_letter 170,065, pending
38,972, retrying 21,202, claimed 21,152, running 20,982). The plan is an index scan on
`ingestion_scopes_pkey`, then a nested loop into
`fact_work_items_scope_generation_idx` on `(scope_id, generation_id)` with
stage and domain filtered on the active generation's 35 rows. There is no
sequential scan, and the generic and custom plans match. One scope executes in
0.12 ms, fifty scopes in 2.5 ms. `pgbench` with 4 clients for 15 s ran 283,482
single-scope lookups: p50 0.189 ms, p95 0.298 ms, p99 0.532 ms. No new index
is needed. `TestReducerContentionGateRuntimeProducerStatesPlanUsesScopeGenerationIndex` fails
if the plan stops using the index (seeded by dropping it).

Amendment A2 re-measure, with the last-workable aggregate: disposable
Postgres 18.6, all 196 numbered migrations, 1,057,631 `fact_work_items` rows
(10,000 git scopes, 3 to 4 generations, 34 reducer domains and one projector
row per generation; 6,266 of the 50,230 pending or retrying rows reopened;
1,028,658 attempted rows carry `last_attempt_at` and the 21,257 retrying rows
a scheduled `visible_at`). The plan is unchanged: `ingestion_scopes_pkey`,
then a nested loop into `fact_work_items_scope_generation_idx` with stage and
domain filtered on 35 rows, no sequential scan, generic and custom plans
alike, and the same buffer reads as the A1 statement (18 shared hits for one
scope, 914 for fifty). `pgbench` with 4 clients, 8 s per run, three rounds
interleaving the A1 and A2 statements on a shared host (load average 25 to
60): one scope mean latency 0.257 to 0.260 ms for A2 against 0.254 to 0.260
ms for A1; fifty scopes 1.664 to 1.696 ms against 1.650 to 1.687 ms. The
added columns are heap columns of rows already fetched.

Observability Evidence: the handler records each decision on
`eshu_dp_service_catalog_runtime_readiness_total{domain,outcome}` and logs
each deferral and each bound overrun; this store adds no signal of its own.

## Tests

- `store_test.go`: repo-to-scope mapping, dedupe, empty input issues no
  query, and every failure returns an error, never an empty state.
- The live proofs sit in the parent package so the reducer contention gate
  runs them (`TestReducerContentionGate*`, which needs `ESHU_POSTGRES_DSN`):
  `service_catalog_runtime_producer_store_live_test.go` pins the
  active-generation-only semantics and the plan shape, and
  `reducer_queue_service_catalog_runtime_*_live_test.go` drive the real
  queue (frozen attempt count, the producer-anchored elapsed bound, a
  reopened producer, mid-check completion, concurrent workers).
