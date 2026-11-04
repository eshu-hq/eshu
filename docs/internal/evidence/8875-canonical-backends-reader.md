# Canonical terraform-backends reader proof (#8875 lane 1)

## Question

The canonical terraform-backends reader swept ~200,000 rows per match
(3.0M shared-buffer hits, 15.3 s warm) at production scale. The lane-1
change adds the predicate-matched partial index
`fact_records_terraform_backends_active_idx` (migration 273) and marks the
`repo_local_paths` CTE `AS MATERIALIZED`. This note records the
before/after evidence and the contribution split.

## Fixture A/B (reproduced 2026-11-03)

Performance Evidence: 2x2 EXPLAIN (ANALYZE, BUFFERS) on a scratch
PostgreSQL 18.6 database (local Compose host, `pg_dump --schema-only`
of `ingestion_scopes`, `scope_generations`, `fact_records` plus manual
fixture seed: 2 active generations, 3,404 file facts with 3,400 decoys,
ANALYZE before each run; old-SQL variant derived from the shipped
constant by removing only the MATERIALIZED keyword; index toggled by
DROP/CREATE with the migration-273 predicate; 1 warmup + 3 measured
runs per cell, medians reported):

| SQL variant | index present | shared buffers (median) | exec ms (median) |
|---|---|---|---|
| old (CTE inlined) | yes | 163 | 2.68 |
| new (MATERIALIZED) | yes | 163 | 2.71 |
| old (CTE inlined) | no | 310 | 3.61 |
| new (MATERIALIZED) | no | 310 | 3.52 |

Row counts are byte-identical across all four cells (402 = 402); the
committed differential (`TestTF8875CanonicalRowDifferentialLive`)
asserts the exact 404-row full fixture plus both owners of a shared
locator hash and a plan that seeks the new index.

Contribution budget: the index arm accounts for the whole fixture-scale
win (310 to 163 buffers, -47%). MATERIALIZED contributes nothing at
402-outer-row fixture scale (identical buffers with and without it in
both index states), which is expected: the rescan it removes scales
with the outer-row count, and the production EXPLAIN that motivated it
shows 9,188 re-executions with 8,657,033 rows removed by the outer
join filter (recorded in the `backend_canonical.go` comment and the
migration-273 header). The deployed magnitude follows that live
attribution (5.9 s / 1.1M-hit `backend_generations` arm inside the
15.3 s statement); fixture timings are host-specific and are not a
deployed wall-time claim. Production re-measurement is follow-up work.

Why safe: the SQL delta is one keyword (`AS` to `AS MATERIALIZED`);
materializing a side-effect-free single-reference CTE cannot change
the row set, only the rescan. The index is partial to the exact CASE
predicate the served queries filter on, so it carries B-tree entries
only for backends-bearing rows; write cost is one predicate evaluation
per ingested file fact. Exactness is gated, not argued: the derived
COUNT probe is built from the shipped constant (never hand-copied)
with a byte-prefix guard, and the differential runs green on a fresh
bootstrap.

## Observability

No-Observability-Change: the change is confined to SQL text, one
comment, migration-273 DDL, and tests. No span, metric, or log line is
added, removed, or re-keyed on the reader path; existing query
telemetry keeps its shape.
