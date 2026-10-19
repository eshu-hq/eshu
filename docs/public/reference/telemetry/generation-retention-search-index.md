# Generation Retention Search Index Pre-Delete

Before each generation-retention pass opens its prune transaction, it deletes
the candidates' `eshu_search_index_terms` and `eshu_search_index_documents`
rows in committed batches (#7427). The prune then admits only generations the
pre-delete drained and whose per-generation advisory lock it takes; since
migration 191 no terms statement runs under the scope lock at all. The
pass-level retention series, the `predelete_search_index` and
`sweep_search_index` phases, and the `search_index_undrained` and
`advisory_lock_busy` skip reasons stay in
[Reducer And Storage Metrics](metrics-reducer-storage.md).

| Metric | Type | Meaning |
|---|---|---|
| `eshu_dp_generation_retention_search_index_batches` | histogram | Search index pre-delete batch transactions (20,000 rows at most each) one pass committed before its prune transaction (#7427); a busy, ineligible or yielded attempt rolls back and is not counted. Two batches drain an empty generation; the default 2,000,000-row cap is about 100 full batches, and the same policy cap bounds one orphan sweep. Rows deleted land on `eshu_dp_generation_retention_rows_pruned_total{table="eshu_search_index_terms"|"eshu_search_index_documents"}`. The orphan sweep's rows are not counted here: see `eshu_dp_search_index_orphan_generations` and `eshu_dp_search_index_orphan_rows_swept_total` in [metrics](metrics.md). |

## Superseded sweep (flagged, #8231)

Superseded generations stay retained while the fact-history hold keeps their
facts, but nothing reads their search index rows: every reader joins the
scope's active generation. The superseded sweep reclaims those rows —
`eshu_search_index_terms`, `eshu_search_index_documents`, and each swept
generation's single `eshu_search_index_stats` row — in committed batches of
at most 20,000 rows, oldest generation first, without changing the
fact-history hold. Exact historical diff, provenance, pinned work, and
in-flight work stay as they are.

**Flag.** `ESHU_GENERATION_RETENTION_SEARCH_INDEX_SUPERSEDED_SWEEP_ENABLED`
(bool, default `false`). With the switch off the sweep issues no SQL. The
runner runs it after the orphan sweep, at most once per poll interval, still
outside any retention transaction and holding no retention row lock.

> Enablement is HELD until the gate-(b) evidence lands: do not set this flag
> until primary dead-tuple/bloat measurements for the terms and documents
> families are attached to #8231. Build, flag, metric, log, and runbook ship
> first; no default-on, no production run before that evidence.

**Safety.** Every batch re-checks in its own snapshot that the generation row
still exists, is terminal superseded, is not the scope's live active
generation, and has no claimed, running, or retrying work — the same
fail-closed outcomes as the generation freshness check (a NULL active
pointer or a missing generation row deletes nothing). A generation pruned
between the listing and a batch leaves orphan rows for the unchanged orphan
census and sweep.

**Metric.** `eshu_dp_search_index_superseded_rows_swept_total{table}`
(counter; `table` is `eshu_search_index_terms`, `eshu_search_index_documents`,
or `eshu_search_index_stats`). Listing and sweep wall time land on the
`census_search_index_superseded` and `sweep_search_index_superseded` phases
of `eshu_dp_generation_retention_phase_duration_seconds`; sweep failures on
`eshu_dp_generation_retention_failures_total{reason="search_index_superseded_sweep"}`
without failing the retention pass.

**Logs.** One `generation retention search index superseded batch committed`
line per committed batch (`scope_id`, `generation_id`, `table`,
`batch_rows_deleted`), plus one `generation retention search index
superseded generation swept` completion line per generation and one
`generation retention search index superseded sweep completed` summary per
run (`candidates`, `rows_swept`, `batches`, `unswept_generations`).

**Rollback.** The sweep deletes derived rows only; facts are never touched.
To stop: unset the flag and roll the reducer — committed batches stay
deleted, later passes simply list nothing new. To restore a swept
generation's index (never eager): facts stay held, so the index can be rebuilt
by a future backfill projection for that generation; no automated restore
path exists today. The index writer upserts are idempotent, so a rebuild
converges without
touching any other generation. There is no eager rebuild after a sweep.
