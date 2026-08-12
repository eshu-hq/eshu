# #5709 producer-scope quiescence probe — theory-proof

The cross-scope readiness gate needs a probe answering: *for a producer collector
kind, which scopes are active and have no live projector work item still running?*
A consumer whose declared producer is not yet quiescent-active must defer rather
than write an empty-join decision.

## The theory

The probe's `NOT EXISTS` body is **byte-equivalent** to the production reducer
claim query (`go/internal/storage/postgres/reducer_queue_claim_query.go:25-30`),
a hot path run on every reducer claim:

```sql
NOT EXISTS (
    SELECT 1 FROM fact_work_items AS projector_work
    WHERE projector_work.stage = 'projector'
      AND projector_work.scope_id = <scope>.scope_id
      AND projector_work.status IN ('pending','retrying','claimed','running')
)
```

So the probe is not a novel query. It reuses an already-hot, already-indexed
shape. The proof only has to **confirm** the reused shape stays index-backed when
driven from the `ingestion_scopes` side (filtered by `collector_kind`, gated on
`active_generation_id IS NOT NULL`).

## Setup

Ephemeral `postgres:16-alpine`. Faithful minimal schema: the real
`fact_work_items_scope_generation_idx (scope_id, generation_id, status,
updated_at DESC)` from migration `005_fact_work_items.sql`, and `ingestion_scopes
(scope_id, collector_kind, active_generation_id)` from its migration. Seed: 500
scopes across 5 collector kinds, all active, 20 generations each; 50,000
projector `fact_work_items` spread across scopes; scopes 1..20 retain some
`pending` work (live), the rest `succeeded` (quiescent). Script:
`docs/internal/evidence/5709-quiescence-probe.sql`.

This seed is a **plan-shape** fixture, not a worst case. It is large enough that
a sequential scan of `fact_work_items` would show up plainly against an index
probe, which is the only thing being asked. One connection, no concurrent
writers, no contention.

## Result — index-backed, sub-millisecond

`EXPLAIN (ANALYZE, BUFFERS)` of the probe for `collector_kind = 'oci_registry'`:

```
 Nested Loop Anti Join  (actual time=0.047..0.535 rows=96 loops=1)
   Buffers: shared hit=795
   ->  Seq Scan on ingestion_scopes s  (rows=100 loops=1)
         Filter: ((active_generation_id IS NOT NULL) AND (collector_kind = 'oci_registry'))
         Rows Removed by Filter: 400
   ->  Index Scan using fact_work_items_scope_generation_idx on fact_work_items projector_work
         (actual time=0.005..0.005 rows=0 loops=100)
         Index Cond: ((scope_id = s.scope_id) AND (status = ANY ('{pending,retrying,claimed,running}')))
         Filter: (stage = 'projector')
 Execution Time: 0.554 ms
```

- The `NOT EXISTS` inner side is an **Index Scan on
  `fact_work_items_scope_generation_idx`**, anchored on `scope_id` — **no
  sequential scan of the 50,000-row `fact_work_items` table**. This is the
  requirement: the probe rides the existing production index.
- The `ingestion_scopes` outer side is a Seq Scan of the small (500-row) scope
  table; the planner declines the `collector_kind` index because 100/500 rows
  match, which is correct and cheap (4 buffers).
- **0.554 ms** for the whole probe over 500 scopes × 50,000 work-items —
  correctly returning 96 quiescent `oci_registry` scopes and excluding the 4
  with live projector work.

## Verdict

Confirmed. The producer-scope quiescence probe is index-backed with no
large-table scan, sub-millisecond on the seed above, and reuses the production
claim query's proven `fact_work_items` access shape. Safe to implement as
`scope_quiescence.go`. Re-run this shim if
`fact_work_items_scope_generation_idx` or the probe's predicate columns change.

## Round 2 — also reporting the registered scopes

The first version returned only the quiescent scope ids, which left the caller
unable to tell "no collector of this kind runs here" from "a collector of this
kind is still working". Both came back as an empty set, and the readiness floor
read both as not-ready. A deployment with no OCI registry collector configured
therefore deferred every `ci_cd_run_correlation` intent to the 30-minute bound.

So the probe now returns every registered scope of the kind with a `quiescent`
flag. Same one round trip. The theory to prove was that adding the flag keeps the
`fact_work_items` access index-backed. It does not, if you write it the obvious
way.

**Disproven: the flag as a target-list `NOT EXISTS`.**

```sql
SELECT s.scope_id, (s.active_generation_id IS NOT NULL AND NOT EXISTS (...)) AS quiescent
FROM ingestion_scopes AS s WHERE s.collector_kind = ANY($1)
```

```
 Seq Scan on ingestion_scopes s  (actual time=5.100..5.128 rows=100 loops=1)
   SubPlan 2
     ->  Seq Scan on fact_work_items projector_work  (actual time=0.004..5.054 rows=285 loops=1)
           Rows Removed by Filter: 49715
 Execution Time: 5.160 ms
```

PostgreSQL 16 de-correlates the subquery and hashes it, so the `scope_id`
condition disappears and the whole 50,000-row table is scanned once. **5.16 ms
against 0.30 ms**, and the cost now tracks the size of `fact_work_items` rather
than the number of scopes. That is the exact degradation the shape test warns
about.

**Shipped: the anti-join kept in a CTE, joined back.**

```sql
WITH registered AS (SELECT s.scope_id, s.active_generation_id FROM ingestion_scopes AS s WHERE s.collector_kind = ANY($1)),
     quiescent  AS (SELECT s.scope_id FROM registered AS s WHERE s.active_generation_id IS NOT NULL AND NOT EXISTS (...))
SELECT registered.scope_id, quiescent.scope_id IS NOT NULL AS quiescent
FROM registered LEFT JOIN quiescent ON quiescent.scope_id = registered.scope_id
```

```
 Hash Right Join  (actual time=0.098..0.440 rows=100 loops=1)
   Buffers: shared hit=795
   ->  Nested Loop Anti Join  (actual time=0.045..0.377 rows=96 loops=1)
         ->  CTE Scan on registered s  (rows=100 loops=1)
               Filter: (active_generation_id IS NOT NULL)
         ->  Index Scan using fact_work_items_scope_generation_idx on fact_work_items projector_work
               (actual time=0.003..0.003 rows=0 loops=100)
               Index Cond: ((scope_id = s.scope_id) AND (status = ANY ('{pending,retrying,claimed,running}')))
 Execution Time: 0.477 ms
```

The Nested Loop Anti Join and the Index Scan survive, and `shared hit=795`
matches the first version's buffer count exactly. Aliasing the CTE `AS s` is what
keeps the `NOT EXISTS` body byte-identical to the reducer claim query's fence.

Five consecutive runs of each on the same seed, median execution time:

| query | median | plan |
| --- | --- | --- |
| quiescent-only (before) | 0.300 ms | Nested Loop Anti Join + Index Scan |
| registered + flag, CTE (after) | 0.338 ms | Nested Loop Anti Join + Index Scan |
| registered + flag, target-list (rejected) | 5.16 ms | Seq Scan on `fact_work_items` |

The 0.04 ms difference is the hash join of 100 rows. Two other arms, single runs:
a collector kind with no registered scope returns in **0.019 ms** with the inner
side `never executed`, and at 200 scopes of one kind the planner flips *both* the
before and after queries to a hash anti-join over a sequential scan
(4.39 ms before, 3.89 ms after) — a cardinality decision that already existed on
`main` and is not introduced here.

Limits, same as round 1: plan shape on a synthetic seed, one connection, no
contention arm, single database. It shows the predicates stay index-resolvable.
It is not a scale or concurrency measurement.

Executed correctness for the new shape is covered separately by
`TestProducerScopeQuiescenceLive`
(`go/internal/storage/postgres/scope_quiescence_live_test.go`), which runs the
real query against a real Postgres and pins the three states apart: kind absent,
registered with live projector work, registered and drained.

## Evidence markers (#5709 substrate)

Round 2 (registered + quiescent in one query) does not change these markers: the
probe still emits nothing of its own, and its access shape is unchanged — same
Nested Loop Anti Join, same Index Scan, same 795 shared buffers, 0.300 ms to
0.338 ms median on the same seed.

No-Regression Evidence: this PR is declarative cross-scope substrate — the
dependency contract, the readiness error type + failure class, the quiescence
probe, and the failure-class enrollment. Nothing consumes any of it at runtime
yet (no handler returns `crossScopeProducerNotReadyError`, no claim path calls
`ProducerScopeQuiescence`), so it adds no runtime path and cannot regress one.
The two primitives that will run once wired are proven against Postgres 16 for
when they are: the quiescence probe rides `fact_work_items_scope_generation_idx`
with an Index Scan (no seq scan) at 0.554 ms on the seeded 500-scope × 20-gen
× 50k-`fact_work_items` seed (the `EXPLAIN (ANALYZE, BUFFERS)` above), and the
`attempt_count`-freeze CASE holds for the enrolled class
(`docs/internal/evidence/5709-attempt-count-freeze.md`). Baseline vs after: the
whole `internal/reducer` + `internal/storage/postgres` test suites are green
before and after; input shape is the seeded shape above; terminal queue state
is unchanged (no new work items enqueued). Why safe: zero-behavior-change — the
declarations have no consumer in this PR.

No-Observability-Change: no metric, span, or log is added or removed. The two new
reducer files are declared in `docs/public/observability/telemetry-coverage.md`
with No-Observability-Change markers; `scope_quiescence.go` and the enrollment
emit nothing of their own and stay inert until a later slice wires them, at which
point the existing reducer queue retry/attempt telemetry covers the deferral.
