# admission_decision_evidence retention count arm (#7700)

`generationRetentionRowCountsQuery` counts the rows each prune deletes so
`BatchRowLimit` and the retention events stay exact. `admission_decisions`
gained an arm in #7396, but its cascade grandchild
`admission_decision_evidence` (`decision_id REFERENCES admission_decisions
ON DELETE CASCADE`) stayed uncounted: every prune under-counted by the
evidence population. This note records the EXPLAIN measurement behind the
16th arm, taken before landing per the #7396 precedent.

Base: `1a311318ba` (2026-10-08).

## Shim (local only)

`postgres:18` in a throwaway container. Minimal DDL mirrors the real
definitions: `scope_generations(generation_id PK)`, `admission_decisions`
with the real `(scope_id, generation_id, ...)` index column list, and
`admission_decision_evidence` with the real `(decision_id, created_at,
evidence_id)` index. Seeded population:

- candidate `gen-old`: 3 decisions x 4 evidence rows (12 counted),
- unrelated `scope-big/gen-big`: 20,000 decisions x 5 evidence rows
  (100,000 rows), later grown to 1,000,000 evidence rows.

The measured statement is the verbatim arm text with `$1 = {gen-old}`,
under `EXPLAIN (ANALYZE, BUFFERS)`.

## Plan

Both probes stay index-only with no sequential scans:

- `Index Only Scan using admission_decisions_scope_generation_domain_idx`,
  1 index search, 4 buffer hits;
- `Index Only Scan using admission_decision_evidence_decision_idx`,
  3 searches (one per decision), 10 buffer hits.

Execution time 0.090 ms at 100k unrelated rows. After growing the unrelated
population 10x to 1,000,000 evidence rows, the identical 3 searches / 10
buffer hits: cost follows the batch, not the table.

## No-Regression Evidence

No-Regression Evidence (#7700): the arm is additive (one more `UNION ALL`
leg) and changes no existing leg. Backend `postgres:18`; input shape one
candidate generation with 3 decisions / 12 evidence rows against 20k / 1M
unrelated rows; terminal row counts exact (`TestGenerationRetentionPrunes
MigratedSchemaLive` asserts 2 doomed rows counted and deleted, 1 retained
row excluded and surviving). The grouped differential
(`TestGenerationRetentionProbeMatchesGroupedPassLive`) stays equal on the
13 oracle tables, and the plan guard
(`TestGenerationRetentionProbePlansStayOnKeyIndexesLive`) stays green in
all four states (cold-custom, cold-generic, analyzed, seeded-red).

## Observability Evidence

Observability Evidence (#7700): no new signals. The new table flows
through the existing retention-event `row_counts` map and the `RowsPruned`
per-table map, both asserted exact by the migrated-schema test above; an
operator sees `admission_decision_evidence` in the same prune event as
the other 28 tables.
