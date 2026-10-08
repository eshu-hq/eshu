# #7396: count the cascade children of scope_generations

`generationRetentionRowCountsQuery` counted 13 tables but the prune's final
`DELETE FROM scope_generations` cascades into 15 more, so `BatchRowLimit`
and the retention events under-counted every prune. The fix adds one
scope-joined UNION ALL arm per child (the `fact_records` shape: join
through `scope_generations` on both `(scope_id, generation_id)`).

## Census

Seeded one superseded generation with one row in each child (two in
`code_reachability_rows`) on local Postgres 18.6 (`postgres:18-alpine`):

- current query reported: 2 rows (the `fact_work_items` only)
- cascade deleted in the 15 children: 16 rows, 0 survivors

Two corrections to the issue's 15-name list, both verified against the
migrated schema's `pg_constraint` closure: `eshu_search_index_terms_shadow`
does not exist post-migration (transient artifact of migration 039a, renamed
into `eshu_search_index_terms`), and `activation_obligations` (migration
160, landed after the issue) is an uncounted cascade child of the same
class the list predates. Net: 15 names minus the phantom plus
`activation_obligations` = 15 new arms, 28 tables total.

Performance Evidence: EXPLAIN ANALYZE on seeded bulk rows (per candidate:
20,000 terms, 5,000 documents, 5,000 vector values, 5,000 reachability
rows, 2,000 admission decisions; second run adds 18 filler generations for
76,000 terms / 28,000 reachability rows): current query 1.4 ms, extended
query 17.9 ms (25.5–26.0 ms with fillers), all on the same container. The
delta is the candidate's own rows: small-child arms probe their
`(scope_id, generation_id, ...)` indexes (Bitmap Index Scan), while the
large-child arms seq-scan at the fixture's 18–26% candidate selectivity —
the planner-correct choice there. The scope join gives every arm both
index columns, so at production selectivity the arms probe the two-column
prefix instead of skip-scanning per candidate over every scope (the plan
shape the probe-plan guard requires of `fact_records`). A generation-only
variant was measured first and rejected on that ground. No new index: the
count runs 2–3× per periodic retention pass, not on a hot path.

Observability Evidence: no new metric. The 15 table names appear as new
`table` label values on `eshu_dp_generation_retention_rows_pruned_total`
(the runner iterates `RowsPruned` with no allowlist) and as new keys in
each retention event's `row_counts` JSON: `activation_obligations`,
`admission_decisions`, `code_reachability_rows`,
`code_reachability_repository_watermarks`, `code_root_verdicts`,
`container_image_identity_cutovers`, `deferred_backfill_partition_memo`,
`eshu_search_document_projection_state`, `eshu_search_index_documents`,
`eshu_search_index_stats`, `eshu_search_index_terms`,
`eshu_search_vector_metadata`, `eshu_search_vector_scope_state`,
`eshu_search_vector_values`, `reducer_input_invalid_facts`.
`BatchRowLimit` compares against the now-complete sum, so batches admitted
near the old under-counted total may narrow sooner; that is the fix, not a
regression. Dashboard queries grouping by `table` pick the new series up
without changes.
