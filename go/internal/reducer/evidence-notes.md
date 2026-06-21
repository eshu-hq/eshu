# reducer Evidence Notes

Keep this file for scoped reducer evidence that is too detailed for the package
orientation README.

## Code-Call Refresh Fence Memory Bound (#3124)

No-Regression Evidence: the baseline public-repository Helm proof for #2995
got past code-call materialization after #3122, emitted 139,352 `code_calls`
shared intents, then OOM-killed the reducer before any `code_calls` intents
completed. The root cause was the selector's refresh-fence fallback loading the
whole `(scope_id, acceptance_unit_id, source_run_id, code_calls)` pending row
set through `ListPendingAcceptanceUnitIntents`. `go test ./internal/reducer
-run TestCodeCallProjectionRunnerUsesBoundedRefreshFenceLookup -count=1`
failed before the runner used a bounded fence lookup, then passed after stores
that implement `CodeCallProjectionRefreshFenceLookup` answer the fence question
without calling the full acceptance-unit loader. The existing compatibility scan
remains for stores that do not implement the optimized lookup, and the existing
ordering tests still prove file rows defer behind covering refresh rows and
earlier whole-scope rows while later whole refresh rows do not block earlier
file partitions.

Performance Evidence: the first bounded Postgres draft removed the reducer heap
spike but still sent whole-scope rows through the file-refresh JSONB branch; in
the Helm proof, late whole-row `selection_refresh_fence_duration_seconds`
samples grew into multi-second checks. The final patch splits the lookup into a
whole-row `EXISTS` query over earlier pending rows and a file-row query for
repo-refresh coverage. The red
`TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceUsesWholeRowLookup`
case proves whole rows no longer include `jsonb_array_elements_text`, while
`TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceDoesNotFenceFileRefreshRows`
keeps the file-refresh ordering semantics aligned with the in-memory fallback.
After rolling the split-query image, steady-state code-call projection cycles
showed refresh-fence checks in tenths of a second while the full-repo
`code_calls` backlog continued draining without SQL errors or OOM restarts. The
terminal Helm proof reached `code_calls` `139861` done and `0` open, with all
other shared projection domains also at `0` open.

Observability Evidence: the change adds no metric name, metric label, span
name, queue domain, worker, lease, runtime knob, graph write route, or Cypher
statement. Operators still diagnose this path through existing code-call
projection cycle logs (`selection_duration_seconds`,
`selection_phases.refresh_fence_check_seconds`, `processed_intents`,
`blocked_readiness`, write and mark-completed durations), shared-intent backlog
queries, partition lease rows, reducer execution counters, and instrumented
Postgres query spans/duration metrics. The Postgres lookup is one scoped
`EXISTS` query over pending `shared_projection_intents`; it does not return
payload rows for the full acceptance unit.

## Service-Catalog Correlation Fanout Guardrails (#3173)

Contract Evidence: service-catalog correlation decisions now carry
`required_anchor_keys` on bounded refusal outcomes (`ambiguous`, `unresolved`,
`stale`, and `rejected`). The required anchors are closed contract names only:
`repository_id`, canonical repository URL fields, or
`git-repository-scope:<repo_id>`. The reducer still refuses name-only catalog
repository claims and ambiguous repository matches; the new field explains the
missing proof without adding raw repository URLs, repository ids, or provider
values to metric labels.

Observability Evidence: the handler summary exposes max candidate fanout,
dropped ambiguous candidates, missing-anchor entities, and required anchor
keys. `eshu_dp_service_catalog_correlations_total` remains a decision counter
with only the closed correlation outcomes. Guardrail counts use
`eshu_dp_service_catalog_correlation_guardrails_total` labeled by bounded
`guardrail` values (`candidate_fanout`, `dropped_ambiguous_candidate`, and
`missing_anchor_entity`). Focused verification:
`go test ./internal/reducer -run 'TestBuildServiceCatalogCorrelation|TestServiceCatalogCorrelation|TestPostgresServiceCatalogCorrelation' -count=1`
and `go test ./internal/reducer -count=1`.

Performance Evidence: `go test ./internal/reducer -run '^$' -bench
BenchmarkBuildServiceCatalogCorrelationDecisionsHighCardinalityFanout -benchmem
-count=3` on darwin/arm64 (Apple M4 Pro) evaluates one catalog entity against
4,096 same-canonical-URL active repositories at `6.36ms/op`, `6.71ms/op`, and
`6.52ms/op` with `13.2MB/op`. The reducer builds one repository lookup per
intent and reads matching repository ids from the indexed canonical URL bucket,
so the high-cardinality fanout path is bounded by the matching candidate set
instead of scanning every active repository for every catalog link. The focused
unit gate `TestBuildServiceCatalogCorrelationDecisionsHandlesHighCardinalityFanout`
keeps the ambiguous decision and 4,096 candidate readback exact.

## Search-Document Writer Bulk Batching (#3430)

Performance Evidence: the `eshu_search_document` reducer writer previously
issued O(N) serial per-document `ExecContext` calls: one `INSERT INTO
fact_records` per fact (`canonicalReducerFactInsertQuery`), one `DELETE FROM
eshu_search_index_terms` per document for term refresh, one `INSERT INTO
eshu_search_index_documents` per document, and one `INSERT INTO
eshu_search_index_terms` per document — totalling 4×N round-trips per scope.
For a scope with 159K content entities (the largest live claimed item at time of
diagnosis) this produced ~636K serial Postgres round-trips per work item.

Measured with `fakeSearchDocExecer` stub counting all `ExecContext` calls
(same harness used by the existing writer tests, darwin/arm64, Apple M5 Max):

| Documents (N) | ExecContext calls before | ExecContext calls after | Reduction |
|---|---|---|---|
| 100 | 404 | 8 | 50× |
| 500 | 2004 | 8 | 250× |
| 1000 | 4004 | 8 | 500× |
| 10000 | 40004 | 8 | 5000× |

The fix replaces the per-document loop with four bulk `unnest`-based statements
constant in count regardless of N:

1. `eshuSearchDocumentBatchFactInsertQuery` — one unnest `INSERT INTO
   fact_records` for all N rows (separate from `canonicalReducerFactInsertQuery`
   which remains unchanged and shared by all other writers).
2. `eshuSearchIndexBatchDocumentUpsertQuery` — one unnest `INSERT INTO
   eshu_search_index_documents` for all N rows.
3. `eshuSearchIndexRefreshDocumentTermsQuery` — one `DELETE … WHERE document_id
   = ANY($3::text[])` replacing N per-document DELETE statements.
4. `eshuSearchIndexBatchTermUpsertQuery` — one unnest `INSERT INTO
   eshu_search_index_terms` for all term rows across all documents.

Backend: Postgres (autocommit handle via `*sql.DB`, same as before — no
transaction boundary added, idempotency is unchanged: `ON CONFLICT (fact_id) DO
UPDATE` and `ON CONFLICT (scope_id, generation_id, document_id) DO UPDATE` and
`ON CONFLICT (scope_id, generation_id, term_key, document_id) DO UPDATE`).
Input shape: complete `[]searchdocs.Document` for one `(scope_id,
generation_id)` scope, same as before. Stats row is still written last so the
sweeper marks a scope done only after the full write succeeds.

Live evidence: at the time of diagnosis the 18 claimed scopes (all
`eshu_search_document`) had a median of 28,383 content entities and a maximum of
159,364. The 174 pending scopes had a median of 200 entities and would each
complete in sub-second wall time with the batched writer. The `idx_docs` counter
was growing at ~280 docs/20s while `succeeded` was frozen at 533, confirming
progress within whale items but no completions — consistent with 636K serial
round-trips per item holding all 4 workers.

No-Regression Evidence: `go test ./internal/reducer -run 'TestWriteEshu'
-count=1` passes all 10 tests (9 existing + 1 new regression test).
`TestWriteEshuSearchDocumentsBatchedWritesBoundedExecCount` (new) asserts that
writing N=500 documents issues strictly fewer than N/2=250 `ExecContext` calls;
it failed on the old per-document loop (2004 calls) and passes on the batched
writer (8 calls). All write semantics are byte-identical: same `fact_id`
derivation, same `stable_fact_key`, same payload JSON shape (including
`content_hash`), same retire-on-missing behaviour for both facts and index
documents, same stats upsert. `go test ./internal/reducer
./internal/storage/postgres ./cmd/reducer -count=1` (3294 tests) passes.
`golangci-lint run ./internal/reducer/... ./internal/storage/postgres/...
./cmd/reducer/...` reports 0 issues.

No-Observability-Change: the batched writer reuses the same
`startSearchIndexWriteSpan`, `recordSearchIndexMutation`,
`recordSearchIndexError`, and `recordSearchIndexWriteDuration` calls as the
per-document loop. The `eshu_dp_search_index_mutations_total` counter still
increments with the same `kind`/`operation`/`result` labels; the
`eshu_dp_search_index_write_duration_seconds` histogram still records total
write duration for the scope. No new metric instrument, metric label, span name,
log key, queue domain, worker, lease, runtime knob, or graph write route was
added. The `eshu_dp_search_index_mutations_total{kind="document",
operation="upsert"}` value now reports the full batch count (N) in a single
increment rather than N increments of 1, producing the same final counter value.
