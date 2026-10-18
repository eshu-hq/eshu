# Service Catalog Runtime Re-trigger (#8169)

**A `workload_materialization` ACK re-triggers the `service_catalog_correlation`
rows whose committed decisions name its repository, in the same SQL statement,
and the #7816 runtime fence also waits on pending `repo_dependency` RUNS_ON
intents. No table, column, or migration is added.**

Refs #8169, #7816, #8218.

## Problem

`service_catalog_correlation` reads the WorkloadInstance and RUNS_ON surface of
each correlated repository. The #7816 fence defers that read while the
repository's `workload_materialization` row is in flight. Three orderings still
commit a service generation with an incomplete runtime family, and nothing
re-ran the correlation afterwards:

1. **no_producer**: the repository has no producer row yet, so the fence
   answers ready (by design, to avoid a deadlock).
2. **bound_exceeded**: every in-flight producer row was idle for 30 minutes,
   so the handler committed what it could read.
3. **Second RUNS_ON writer**: the `repo_dependency` shared projection adds
   RUNS_ON from a `shared_projection_intents` row the fence did not see.
   `PlatformKind` is part of the runtime identity, so a missed platform row is
   wrong runtime truth.

A later re-materialization of the same repository also changed the instance
set without a new catalog generation.

## Ruling

The arbiter ruled between per-edge gating on the completion event (A), a
repository-level readiness signal only (B), and a re-trigger (C). It chose C
on top of the #8218 fence:

- A emits only when the value-flow emit gate passes, fans out to every consumer
  of the producer domain, needs a v3 partial-index migration and a producer
  trigger for `repo_dependency`, and still misses no_producer.
- B alone cannot repair "committed, then the producer completed".
- The `workload_materialization` ACK is the single terminal event of both
  RUNS_ON writers: the `repo_dependency` runner writes RUNS_ON, then replays
  `workload_materialization` fail-closed, then marks its intents completed
  (`repo_dependency_projection_acceptance_cycle.go`).

The ruling allowed a precise repository-to-catalog map only after measurement
showed the global reopen costs too much. It did (see Measurement), so the
re-trigger is precise.

## Design

### Fence (read side)

`readiness/producer.Store` answers both producers in one statement
(`RuntimeProducerAndRunsOnStatesQuery`, UNION ALL, one READ COMMITTED
snapshot):

- the #8218 `workload_materialization` state per repository scope, unchanged;
- pending `repo_dependency` intents per `repository_id` with
  `payload.relationship_type = 'RUNS_ON'` and `completed_at IS NULL`.

One snapshot matters: the runner commits the RUNS_ON write, the producer
reopen, and the intent completion separately, so one snapshot sees either the
pending intent or the reopened producer row. A pending intent defers with the
same non-counting class `service_catalog_correlation_runtime_not_ready`. The
30-minute bound counts from the later of the producer row's last-workable time
and the intent's newest `created_at`.

### Re-trigger (write side)

`ackWorkloadMaterializationReducerWorkQuery` and its batch sibling are the
value-flow ACK CTEs plus `serviceCatalogRuntimeRetriggerCTEs`. The re-trigger is
not gated on the value-flow emit flag. For each acked
`git-repository-scope:<repo>`:

| Catalog row at its active generation | Match | Action |
| --- | --- | --- |
| `succeeded` | its own generation committed a `reducer_service_catalog_correlation` decision with `repository_id = <repo>` | reopen (the `ReopenSucceededReducerSetClause` columns) |
| `claimed` / `running`, not flagged | its scope has such a decision in any generation, or no decision yet | `cross_scope_replay_required = TRUE` |
| `pending` / `retrying` | none | untouched; it re-reads on its next claim |

The decisions' `repository_id` is exactly the repository set the catalog run
fences and reads runtime for (`serviceRepositoryIndex`), and
`fact_records_service_catalog_correlations_repository_idx` (migration 003)
indexes it. A first run has no decision yet, so an in-flight scope without one
is flagged conservatively. That is at most one extra run per catalog scope.

The migration 093 trigger turns a flagged row's successful ACK into pending.
It clears the flag on retry, and the retrying row re-reads anyway.

### Concurrency

- Lock order in the ACK: the producer row (the ACK UPDATE), then the catalog
  rows in `work_item_id COLLATE "C"` order (a MATERIALIZED `FOR NO KEY UPDATE`
  CTE, the fanout's order). Nothing locks a catalog row and then a producer
  row. Two concurrent ACKs wait on row locks and cannot cycle.
- One UPDATE with CASE on the locked row, the fanout's `scheduled` shape. Two
  UPDATE CTEs, one to reopen and one to flag, would lose a row whose snapshot
  status was `claimed` and which committed `succeeded` while this statement
  waited. The reopen CTE rejects the snapshot row, and the flag CTE's recheck
  rejects the new version.
- A reopen of a pending row is a no-op, so N ACKs coalesce into one catalog
  run. The fence then defers that run until the correlated producers drain.
- The reopen commits with the ACK or not at all. An unacked producer
  re-claims after lease expiry and re-ACKs, which re-triggers.

### Accuracy

An unchanged repository produces no new generation: the generation id is the
evidence fingerprint, a repeat write is `ON CONFLICT DO NOTHING`, and
`service_changed_since` reports nothing for it. A changed repository's next
generation carries the runtime family, including `PlatformKind`.

## Measurement

Performance Evidence: disposable Postgres 18 with all 213 migrations;
1,134,684 `fact_work_items` rows (10,000 git scopes, 3 or 4 generations, 34
reducer domains); 875,000 `shared_projection_intents` (4,973 pending); 3,000
catalog rows (1,000 at an active generation); 1,000,000 unrelated
`fact_records` plus 16,500 catalog decisions, one central catalog naming 500
repositories. Each run used `pgbench -M prepared` (pgx caches statements) with
BEGIN, the ACK, and ROLLBACK, for 10 s, on a shared host.

| Statement | c1 mean | c4 mean | Host load |
| --- | --- | --- | --- |
| baseline workload ACK | 0.154 ms | 0.159 ms | ~20 |
| global re-trigger, 1,000 succeeded catalog rows reopened per ACK | 60.1 ms | 183.1 ms | ~20 |
| global re-trigger, steady state (catalog rows already pending) | 2.98 ms | not run | ~20 |
| precise re-trigger, prototype | 0.305 ms | 0.333 ms | ~20 |
| precise re-trigger, steady state | 0.232 ms | not run | ~20 |
| RUNS_ON intent lookup, 1 repository | not run | 0.120 ms | ~20 |
| RUNS_ON intent lookup, 50 repositories | not run | 0.976 ms | ~20 |

The global reopen serialized concurrent ACKs on the same catalog row locks, at
25 tps under 4 clients. Its steady state built a hash over every
`ingestion_scopes` row. Each reopen also re-runs every catalog handler on every
workload ACK. The precise plan uses index paths only:
`fact_records_service_catalog_correlations_repository_idx` (bitmap),
`ingestion_scopes_active_generation_idx`, `fact_work_items_scope_generation_idx`,
`fact_work_items_stage_domain_status_idx` for in-flight rows, a hashed subplan
for the in-flight decision match, and the primary key for the lock. The RUNS_ON
lookup reads `shared_projection_intents_repo_run_idx`; no plan has a sequential
scan. The shipped statement's measured cost and host load are recorded in the
PR.

Observability Evidence: `eshu_dp_service_catalog_runtime_retrigger_total{domain,outcome}`
(`reopened`, `replay_flagged`, `none`). One info log per ACK that reopened or
flagged a row. `producer_kind` and `runs_on_intents_pending` on the fence logs.
See [Service Catalog Runtime Re-trigger](../../public/reference/telemetry/service-catalog-runtime-retrigger.md).

## Proof

Real-queue tests against Postgres, `TestReducerContentionGate*` prefix:

- (a) `...RetriggerRepairsNoProducerGeneration`: G1 at no_producer, then the
  workload ACK reopens the catalog row. G2 carries 2 runtime rows and G1 is
  superseded. The unchanged repository keeps its generation, with 0 runtime
  `added`.
- (b) `...RetriggerFlagsClaimedRow`: the flag is set on the claimed row, its
  ACK becomes pending, and the next run commits the runtime rows.
- (c) `...FenceWaitsOnRunsOnIntent`: a pending RUNS_ON intent defers with
  nothing written. After the runner's replay and completion, the catalog
  commits.
- (d) `...ConcurrentAcksReopenOnce`: two ACKs on separate connections cause no
  deadlock, one reopen, one run, and one generation.
- Batch ACK and telemetry: `...RetriggerBatchAckAndTelemetry`.
- (g) The drift guard classifies `canonical_relationships.go` as the
  `repo_dependency` shared-intent producer.

## Rollback

Revert the ACK SQL and the store lookup. There is no schema to roll back.

## Not in scope

`ingestion_reopen_correlation.go` (#8082), the RUNS_IMAGE/CAN_PERFORM
retract-skip rule (#8167), and the Module writer (#8168).
