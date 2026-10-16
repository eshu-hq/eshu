# Generation Retention Search Index Pre-Delete

Before each generation-retention pass opens its prune transaction, it deletes
the candidates' `eshu_search_index_terms` and `eshu_search_index_documents`
rows in committed batches (#7427). The prune then admits only generations the
pre-delete drained and whose per-generation advisory lock it takes, so the
foreign-key cascade under the scope lock finds no search index row. The
pass-level retention series, the `predelete_search_index` and
`sweep_search_index` phases, and the `search_index_undrained` and
`advisory_lock_busy` skip reasons stay in
[Reducer And Storage Metrics](metrics-reducer-storage.md).

| Metric | Type | Meaning |
|---|---|---|
| `eshu_dp_generation_retention_search_index_batches` | histogram | Search index pre-delete batch transactions (20,000 rows at most each) one pass committed before its prune transaction (#7427); a busy, ineligible or yielded attempt rolls back and is not counted. Two batches drain an empty generation; the default 2,000,000-row cap is about 100 full batches. Rows deleted land on `eshu_dp_generation_retention_rows_pruned_total{table="eshu_search_index_terms"|"eshu_search_index_documents"}`. The orphan sweep's rows are not counted here: see `eshu_dp_search_index_orphan_generations` and `eshu_dp_search_index_orphan_rows_swept_total` in [metrics](metrics.md). |
