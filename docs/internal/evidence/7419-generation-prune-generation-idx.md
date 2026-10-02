# Generation-Prune Generation-ID Index Evidence (#7419)

Three cascade children of `scope_generations` carry `generation_id` but no
index that leads with it:

- `fact_replay_events`: only index is `(work_item_id, created_at)`.
- `graph_projection_phase_state`: `generation_id` is the fourth column of the
  primary key `(scope_id, acceptance_unit_id, source_run_id, generation_id,
  keyspace, phase)`.
- `graph_projection_phase_repair_queue`: same primary-key shape.

Every prune deletes one generation row
(`deleteScopeGenerationsForRetentionQuery`, `DELETE FROM scope_generations
WHERE generation_id = ANY($1)`), which checks each of these tables through
its `generation_id REFERENCES ... ON DELETE CASCADE` foreign key, and the
retention row-count query probes each by `candidate.generation_id`. With
nothing to seek by, each probe reads the whole table per pruned generation,
scaling with total corpus size regardless of the pruned generation's size.

Migrations `156_fact_replay_events_generation_idx.sql`,
`157_graph_projection_phase_state_generation_idx.sql`, and
`158_graph_projection_phase_repair_queue_generation_idx.sql` add plain
`(generation_id)` btrees, each as a sole `CREATE INDEX CONCURRENTLY IF NOT
EXISTS` statement. The probes are equality matches with no ordering, so a
single-column btree is the whole contract.

## Fixture (local only)

`postgres:16.15` in a throwaway container (`eshu-7419-idx-proof`), real child
DDL from migrations 006/012/013 with stub parents, `VACUUM ANALYZE` after
seeding.

| Table | Rows | Generations | Total size | New index |
| --- | --- | --- | --- | --- |
| `fact_replay_events` | 200,000 | 200 (1,000 each) | 38 MB | 1,440 kB |
| `graph_projection_phase_state` | 50,000 | 200 (250 each) | 11 MB | 376 kB |
| `graph_projection_phase_repair_queue` | 50,000 | 200 (250 each) | 13 MB | 376 kB |

The 50k phase-table scale matches the issue's ops-qa census (53,135 rows in
`graph_projection_phase_state`). Statements are measured with `EXPLAIN
(ANALYZE, BUFFERS, TIMING OFF)`; the prune DELETE runs inside
`BEGIN ... ROLLBACK` on this fixture database.

## Before / after

Per-prune probe `SELECT COUNT(*) FROM <table> WHERE generation_id = 'gen-7'`
(one of 200 generations), plus the rolled-back single-generation prune.

| Probe | Before (no index) | After (index) |
| --- | --- | --- |
| replay events count | seq scan, 1,856 buffers, 17.0 ms | bitmap index scan, 1,003 buffers, 2.5-3.7 ms warm |
| phase state count | seq scan, 569 buffers, 4.1 ms | index-only scan, 253 buffers, 0.6-0.9 ms warm |
| repair queue count | seq scan, 717 buffers, 4.6 ms | index-only scan, 253 buffers, 0.7-0.9 ms warm |
| prune `DELETE FROM scope_generations` | 23.7 ms (cascade checks scan all three) | 1.9 ms |

After shapes:

```text
->  Bitmap Index Scan on fact_replay_events_generation_idx
      Index Cond: (generation_id = 'gen-7'::text)
->  Index Only Scan using graph_projection_phase_state_generation_idx
      Index Cond: (generation_id = 'gen-7'::text)
->  Index Only Scan using graph_projection_phase_repair_queue_generation_idx
      Index Cond: (generation_id = 'gen-7'::text)
```

### Write amplification

Rolled-back batched inserts through the production column shapes, single run
each arm: 10k replay rows 266.2 ms without / 264.7 ms with; 2k phase-state
rows 33.4 / 33.3 ms; 2k repair rows 35.9 / 34.9 ms. No measurable delta at
this scale (heap + FK checks dominate); the standing cost is ~2.2 MB of
indexes at this fixture scale. Not measured: sustained writer throughput on a
larger corpus; these tables are small and grow with generations, not facts.

## Migration build strategy

A concurrent build takes `ShareUpdateExclusiveLock`, which does not block
`INSERT`/`UPDATE`/`DELETE`, so writers keep running during the build. Each
file holds exactly one statement so the migration coordinator runs it in
autocommit without the bootstrap `lock_timeout` (#7004) and drops an invalid
same-name index before a retry. `IF NOT EXISTS` makes a re-run a no-op. One
file per index per `coordination.IsSoleConcurrentIndexStatement`: PostgreSQL
rejects a concurrent build inside a multi-statement string. The three shipped
files were applied verbatim to the fixture (`CREATE INDEX` each, index names
confirmed in `pg_indexes`).

## No-Regression Evidence

No-Regression Evidence (#7419): the change is three new indexes and no query
text changes, so no reader gets a different result. The per-prune probes go
from whole-table reads to seeks (table above); the pruned generation's rows
are unchanged, only reached cheaper. The cascade still deletes exactly the
same rows. No lease, claim, queue, or transaction path changes. Out of scope
by triage, not by oversight: `fact_work_items`, `semantic_extraction_jobs`,
and `shared_projection_acceptance` also probe by `generation_id` in the
row-count query but already carry `(scope_id, generation_id, ...)` prefixes
that serve their scope-joined arms; the issue names only the three tables
with no usable prefix, and this change adds nothing elsewhere.

## Observability Evidence

No-Observability-Change: no metric, span, log, or status signal is added. The
indexes are observable through `pg_stat_user_indexes`
(`idx_scan` on the three `*_generation_idx` indexes) and the existing
generation-retention timing.
