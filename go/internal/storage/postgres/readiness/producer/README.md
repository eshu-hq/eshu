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
`newest_in_flight_cycle_started_at`: `max(COALESCE(reopened_at, created_at))`
over the pending, retrying, claimed, and running rows only (NULL, read as
zero, when none is in flight). The handler measures its 30-minute bound per
repository from that timestamp. It never reads `updated_at`, which a retry or
a re-claim moves; `created_at` is the enqueue time and `reopened_at` the start
of a reopened cycle.

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

Amendment A1 re-measure, with the cycle-start aggregate added: disposable
Postgres 18.6, all 196 numbered migrations, 1,057,631 `fact_work_items` rows
(10,000 git scopes, 3 to 4 generations, 34 reducer domains and one projector
row per generation; 6,266 of the 50,230 pending or retrying rows reopened). The plan is
unchanged: `ingestion_scopes_pkey`, then a nested loop into
`fact_work_items_scope_generation_idx` with stage and domain filtered on 35
rows, no sequential scan, generic and custom plans alike. One scope 0.08 ms
(0.07 ms unextended on the same data), fifty scopes 1.4 ms (1.45 ms
unextended), an absent scope 0.02 ms. The aggregate reads columns of rows
already fetched, so it adds no buffer reads (21 shared hits for one scope
either way).

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
