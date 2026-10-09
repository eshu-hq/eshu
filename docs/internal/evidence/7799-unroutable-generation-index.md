# Evidence: #7799 — the unroutable-intents count arm misses malformed-scope rows the generation-only reap deletes

Scope: `generationRetentionRowCountsQuery`
(`go/internal/storage/postgres/generation_retention_row_counts_sql.go`), the
`shared_projection_unroutable_intents` leg, and the reap
`deleteSharedProjectionUnroutableIntentsForGenerationsQuery`
(`generation_retention_sql.go`). The fix reshapes the leg to the reap's
generation-only envelope and adds one index, migration
`167_unroutable_intents_generation_idx.sql`:
`shared_projection_unroutable_intents (generation_id)`.

This is a **Prove-The-Theory-First** record. The envelope mismatch was
reproduced in a scratch schema, the generation-only leg was measured with
and without the index, and the shipped shape was measured under custom
and generic plans. No DDL ran outside the scratch schema and the test
database.

## Method

Scratch schema on local Postgres 18.6 (`SELECT version()`:
PostgreSQL 18.6 on x86_64-pc-linux-gnu), 2026-10-09. Tables
`scope_generations` (minimal) and `shared_projection_unroutable_intents`
(migration 098 DDL verbatim), seeded with 200 scopes x 50 generations (10k
generations) and ~200k unroutable rows (~20 per generation), plus one
empty-scope and one wrong-scope row on prunable generations. Candidate
batch: 20 generations (`gen:1:1`..`gen:1:20`); DELETE batch: 5
generations. All statements `EXPLAIN (ANALYZE, BUFFERS)` after
`VACUUM ANALYZE`.

## Results

| Shape | Plan | Buffers | Time |
|---|---|---|---|
| Scope-joined leg (current arm) | Nested loop, index-only probe of the scope-leading index per candidate | hit=101 | 0.174 ms |
| Generation-only leg, no new index | Nested loop, 902 index searches skip-scanning the scope-leading index | hit=7696 | 11.480 ms |
| Generation-only leg + (generation_id) index, custom plan | Nested loop, 20 index-only probes of the new index | hit=61 | 0.229 ms |
| Same leg, generic plan (PREPARE, 5-candidate batch, 6 executions) | Nested loop, 5 probes each, identical all 6 | hit=11 | ~0.05 ms |
| DELETE, no new index | Bitmap heap + bitmap index scan | hit=649 | 4.519 ms |
| DELETE + (generation_id) index | Bitmap heap + bitmap index scan on the new index | hit=313 | 0.199 ms |

Correctness on the seeded malformed rows: the scope-joined leg counts
`gen:1:1 = 0, gen:1:2 = 0`; the generation-only leg counts
`gen:1:1 = 2, gen:1:2 = 1`, matching the rows the DELETE reaps.

## Rejected alternatives

- Generation-only leg with no new index: the 902-search skip-scan grows
  with scopes, not the batch — the cliff the file's own #7396 comment
  warns about. Rejected on the measured 7696 buffers.
- Correlated scalar subquery leg (the #7784 package-keys precedent):
  measured stable (117 buffers for 5 candidates, generic-plan proof),
  but the plain JOIN is equally stable here (11 buffers, no reorder
  across 6 generic executions) and keeps the existing arm shape, so the
  smaller diff wins.
- Leaving the envelopes different with a documented bound: rejected —
  the index closes the mismatch for both the count and the reap at one
  small btree entry per unroutable insert, a table written only on
  routing failures.

## Performance Evidence

Performance Evidence: on the 200k-row scratch schema the generation-only
leg without the index skip-scans the scope-leading index (902 searches,
7696 buffers, 11.480 ms for a 20-generation batch); with migration 167
it is a nested-loop probe of the new btree (20 searches, 61 buffers,
0.229 ms custom plan; 11 buffers stable across six generic-plan
executions of a 5-candidate batch), and the 5-generation reap DELETE
drops from 649 to 313 buffers.

## Observability Evidence

No-Observability-Change: the change reshapes one count leg to the
envelope the reap already uses and adds one btree index. The count runs
under the retention cycle's existing instrumentation, and the per-table
event `row_counts` and `rows_pruned_total{table}` counter now agree for
this table with no new signal.

## Guard-corpus note

The numbers above are from the scratch schema, not the repo's guard
corpus: no guard-corpus target seeds 200k unroutable rows, and the
migrated-schema exact-count test pins the envelope (well-formed plus
both malformed shapes) rather than the plan. The plan contract —
generation-only probe of the (generation_id) index, cost following the
batch — is what the table above proves.
