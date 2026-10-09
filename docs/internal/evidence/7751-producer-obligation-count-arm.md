# producer_activation_obligations retention count arm (#7751)

`generationRetentionRowCountsQuery` counts the rows each prune deletes so
`BatchRowLimit` and the retention events stay exact. Migration 163 gave
`producer_activation_obligations` a `generation_id REFERENCES
scope_generations ON DELETE CASCADE` with PK `(generation_id, scope_id)`,
but the census had no arm for the table: every prune of a generation
holding an obligation row under-counted by 1. This note records the
EXPLAIN measurement behind the 16th cascade-child arm, taken before
landing per the #7396 precedent.

Base: `3b03f018ed` (2026-10-09).

## Shim (local only)

`postgres:18` in a scratch database on the shared dev container. Minimal
DDL mirrors the real definitions: `scope_generations(generation_id PK,
scope_id)` and `producer_activation_obligations` with the real
`(generation_id, scope_id)` PRIMARY KEY. Seeded population:

- candidate `gen-old`: 1 obligation row (1 counted),
- unrelated `scope-big/gen-big`: 2,000 generations x 1 obligation row,
- grown to 20,001 rows (PK bounds each generation to 1 row).

The measured statement is the verbatim arm text with `$1 = {gen-old}`,
under `EXPLAIN (ANALYZE, BUFFERS)`.

## Plan

Both probes stay index-backed with no sequential scans at 20k unrelated
rows:

- `Index Scan using scope_generations_pkey`, 1 index search, 3 buffer hits;
- `Index Only Scan using producer_activation_obligations_pkey`,
  1 index search, 0 heap fetches.

Execution total 9 buffer hits against 20,001 unrelated rows (7 hits with
the table at 1 row; the delta is B-tree depth). Cost follows the batch,
not the table.

## No-Regression Evidence

No-Regression Evidence (#7751): the arm is additive (one more `UNION ALL`
leg) and changes no existing leg. Backend `postgres:18`; input shape one
candidate generation with 1 obligation row against 2k then 20k unrelated
rows; terminal row counts exact
(`TestGenerationRetentionPrunesMigratedSchemaLive` asserts 1 doomed row
counted and deleted, and the probe pair count pins 35-table coverage).
The grouped differential
(`TestGenerationRetentionProbeMatchesGroupedPassLive`) stays equal on the
13 oracle tables, and the plan guard
(`TestGenerationRetentionProbePlansStayOnKeyIndexesLive`) stays green in
all four states (cold-custom, cold-generic, analyzed, seeded-red).

## Observability Evidence

Observability Evidence (#7751): no new signals. The new table flows
through the existing retention-event `row_counts` map and the `RowsPruned`
per-table map, both asserted exact by the migrated-schema test above; an
operator sees `producer_activation_obligations` in the same prune event
as the other 34 tables.
