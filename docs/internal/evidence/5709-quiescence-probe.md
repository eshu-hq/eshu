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
(scope_id, collector_kind, active_generation_id)` from its migration. Worst-case
seed: 500 scopes across 5 collector kinds, all active, 20 generations each;
50,000 projector `fact_work_items` spread across scopes; scopes 1..20 retain some
`pending` work (live), the rest `succeeded` (quiescent). Script:
`docs/internal/evidence/5709-quiescence-probe.sql`.

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

Confirmed. The producer-scope quiescence probe is index-backed against the
worst-case partition with no large-table scan, sub-millisecond, and reuses the
production claim query's proven `fact_work_items` access shape. Safe to implement
as `scope_quiescence.go`. Re-run this shim if `fact_work_items_scope_generation_idx`
or the probe's predicate columns change.
