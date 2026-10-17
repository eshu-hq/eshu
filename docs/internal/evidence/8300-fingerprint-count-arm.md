# #8300 fingerprint count arm

`generationRetentionRowCountsQuery` counts the rows each prune deletes so
`BatchRowLimit` and the retention events stay exact. Migration 193 (#8283)
added `deployable_unit_reopen_fingerprints` with `work_item_id REFERENCES
fact_work_items(work_item_id) ON DELETE CASCADE`: a cascade grandchild of
the pruned generation, like the key sidecars in #7396 and #7784. Every prune
of a fingerprinted work item under-counted by its fingerprint rows. A catalog
test now lists every table the prune deletes through a cascading foreign key,
transitively from `fact_work_items` and `scope_generations` (25 tables,
zero exemptions), and requires an arm.

## Shim (local only, deleted after measuring)

Local `postgres:18-alpine` container; isolated migrated schema from the real
bootstrap definitions. One doomed generation with 5,000 work items and 5,000
fingerprint rows, one live generation with 1,000 work items, plus 10 filler
scopes with 5,000 fingerprinted work items each, so the doomed set is ~8% of
the table. The measured statement is the verbatim arm text with
`$1 = {doomed}`, under `EXPLAIN (ANALYZE, BUFFERS)`.

## Plan

The count comes back exact (ledger:8300-fingerprint-arm-exact-count). The work
leg probes `fact_work_items_scope_generation_idx` on both columns; the
fingerprint leg is an index-only scan of
`deployable_unit_reopen_fingerprints_pkey` on
`work_item_id = work.work_item_id`, on the custom plan
(ledger:8300-fingerprint-arm-custom-plan-ms) and under
`force_generic_plan`
(ledger:8300-fingerprint-arm-generic-plan-ms). At 83% fixture selectivity the
planner hash-joins a sequential scan instead, the planner-correct choice per
the #7396 precedent. No new index: the primary key is the probe.

## No-Regression Evidence (#8300):

The arm is additive (one more `UNION ALL` leg) and changes no existing leg.
Backend `postgres:18`; terminal row counts exact
(`TestGenerationRetentionCountsReopenFingerprintsLive` asserts the reported
count equals the rows the prune deletes, and the catalog test asserts every
cascade child is armed). The grouped differential
(`TestGenerationRetentionProbeMatchesGroupedPassLive`) stays equal on the
oracle tables with the probe pinned at 36 tables, and the plan guard
(`TestGenerationRetentionProbePlansStayOnKeyIndexesLive`) stays green.

## No-Observability-Change (#8300):

No new signals. The new table flows through the existing retention-event
`row_counts` map and the `RowsPruned` per-table map, both asserted exact by
the live test above; an operator sees it in the same prune event as the
other tables.
