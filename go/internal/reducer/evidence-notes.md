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

## Correlation/Identity Writer Bulk Batching (#3435)

Follow-up to the #3430/#3434 search-document batching above. The audit in #3435
found four more reducer writers that issued the search-document writer's old
per-row pattern: one `canonicalReducerFactInsertQuery` `ExecContext` inside a
document/decision/resource loop, i.e. O(N) serial Postgres round-trips per work
item. They are lower-volume than search-docs today, so this is a capacity/no-
regression hardening, not a fix for an active starvation incident.

Batched writers (per-row loop replaced with a bounded chunked unnest insert):

| Writer | Per-row arg | Was | Now |
|---|---|---|---|
| `ci_cd_run_correlation_writer.go` | one decision | N execs | ceil(N/1000) execs |
| `container_image_identity_writer.go` | one canonical decision | N execs | ceil(N/1000) execs |
| `sbom_attestation_attachment_writer.go` | one decision | N execs | ceil(N/1000) execs |
| `cloud_inventory_admission_writer.go` | one admitted resource | N execs | ceil(N/1000) execs |

All four now build a `[]reducerFactRow` and call the new shared
`reducerBatchInsertFacts` (`reducer_fact_batch_insert.go`), which sends rows in
bounded chunks of `reducerFactBatchSize` (1000) via `reducerFactBatchInsertQuery`
— a writer-local unnest `INSERT INTO fact_records` whose column list, conflict
key, and `ON CONFLICT (fact_id) DO UPDATE` set are byte-equivalent to the shared
`canonicalReducerFactInsertQuery`. The shared query is unchanged so its other
13 per-row callers are untouched. `cloud_inventory_admission` keeps its
per-resource `collector_kind` (each resource can be a different provider) by
setting `reducerFactRow.CollectorKind` per row inside the loop.

The 1000-row chunk bounds the bind-parameter count: 15 columns × 1000 = 15000
parameters, well under the Postgres 65535 ceiling, and caps per-statement memory
and lock footprint for a single scope. Each chunk is one statement on the same
autocommit `*sql.DB` handle the writers already used — no transaction boundary
added, idempotency unchanged (`ON CONFLICT (fact_id) DO UPDATE`), so a retry or
two concurrent workers admitting the same generation converge on the same rows.

No writers were left as-is. Every one of the four flagged writers loops over a
caller-supplied slice whose size is not statically bounded (decisions/resources
per scope generation), so each carries the same structural O(N) risk if scope
sizes grow; none is a fixed-size/single-row write, so batching all four is
warranted rather than gratuitous.

No-Regression Evidence: `go test ./internal/reducer ./internal/storage/postgres
./cmd/reducer -count=1` passes (3310 tests). New bounded-exec regression tests
assert N=400 decisions/resources issue exactly `ceil(N/reducerFactBatchSize)`=1
`ExecContext` call instead of 400:
`TestWriteCICDRunCorrelationsBoundedExecCount`,
`TestWriteContainerImageIdentityDecisionsBoundedExecCount`,
`TestWriteSBOMAttestationAttachmentsBoundedExecCount`,
`TestWriteCloudInventoryAdmissionBoundedExecCount`. They fail on the old per-row
loop (400 calls) and pass on the batched writers (1 call).
`TestReducerBatchInsertFactsChunksByBatchSize` proves real chunk splitting:
`reducerFactBatchSize*2+7` rows produce exactly 3 statements, each ≤1000 rows,
with row order and `fact_id` identity preserved across chunk boundaries;
`TestReducerBatchInsertFactsEmptyIssuesNoStatements` proves an empty scope
issues zero round-trips. The existing per-row positional-arg assertions in the
reducer and `internal/storage/postgres` cloud-inventory tests were updated to
decode the batched parallel arrays back into per-row records (same `fact_id`,
`fact_kind`, payload JSON assertions); the convergence simulator
`convergentFactStore` keys on each `fact_id` in the batched array so the
concurrent-worker convergence test still proves no MERGE/duplicate races.
`go vet` and `golangci-lint run ./...` report 0 issues.

No-Observability-Change: these four writers emitted no metric, span, or log of
their own (they returned an `EvidenceSummary` string the handler logs); the
batched writers preserve that exact return contract and add no new metric
instrument, metric label, span name, log key, queue domain, worker, lease, or
runtime knob. `ESHU_REDUCER_WORKERS` is unchanged — concurrency was not reduced
as a mitigation.

## Search-Document Streaming Load + Bounded Write (#3440)

Classification (per eshu-diagnostic-rigor): Wall-clock + Correctness win. The
streamed path unblocks generation completion (active->completed flips) by
removing the multi-minute single-work-item stalls that held all reducer workers.

Stage: reducer `eshu_search_document` source load + projection + write.

Input shape: whale repository scope, ~159K content entities and ~94MB file
content across ~11K files (the largest live claimed item at diagnosis; a single
file can be ~52MB).

No-Regression Evidence: before this change the `EshuSearchDocumentSourceLoader`
issued two unbounded `SELECT`s per work item —
`loadEshuSearchDocumentEntitiesQuery` (all `content_entities` for the repo,
`ORDER BY entity_id` with no `LIMIT`, producing a ~50MB external-merge sort spill
and a 159K-row Go slice) and `loadEshuSearchDocumentFilesQuery` (all
`content_files` including the full `content` column, ~94MB into a Go slice). Both
were accumulated into one `reducer.SearchDocumentProjectionInput`, projected as a
whole, and written in one shot. Live reducer logs showed single search-doc work
cycles of 145s and 1075s (18 min); the reduce queue drained at ~tens/10min,
generations never flipped active->completed (live: active=982, completed=3), and
the projector sat idle behind a large `pending_projection` backlog.

The change re-architects the path to keyset-paginated streaming load ->
per-page project -> per-page insert -> single finalize-retire:

1. Loader: `StreamSearchDocumentSources` resolves the scope's `repo_id` once,
   then keyset-paginates entities by `entity_id` (the PRIMARY KEY:
   `WHERE repo_id=$1 AND entity_id > $cursor ORDER BY entity_id LIMIT $pageSize`)
   and files by `relative_path` (part of `PRIMARY KEY (repo_id, relative_path)`).
   Walking the indexed key in order with a per-page `LIMIT` removes the unbounded
   `SELECT` and the ~50MB external-merge sort, and bounds each read to one page.
   No new index was added; both keyset predicates ride existing PKs. Entity page
   size is 2000 rows; file page size is 256 rows with a 16 MiB content
   byte-budget early flush so a page of large files cannot itself exhaust memory.
2. Writer: split into `BeginEshuSearchDocumentWrite` -> session `InsertPage`
   (insert-only: bulk fact upsert + index-doc upsert + per-doc term refresh +
   bulk term upsert; NO retire) -> `Finalize` (single authoritative
   retire-by-absence over the union written-id keep-set for facts, index terms,
   and index documents, then stats). Accumulating only the keep-sets (~tens of
   bytes per id, ~13MB for 159K ids vs. 94MB content) keeps peak content memory
   bounded to one page while preserving the existing generation-authoritative
   retire semantics. `WriteEshuSearchDocuments` is retained and re-implemented as
   `Begin -> InsertPage(all) -> Finalize`, so all prior writer tests pass
   unchanged and the single-shot path stays byte-identical (proved by
   `TestWriteEshuSearchDocumentsEqualsStreamingOnePage`).

Idempotency/retry: per-page inserts remain idempotent (`ON CONFLICT … DO
UPDATE` by `fact_id` and `(scope_id, generation_id, document_id)` and the term
key). A retry re-streams and re-upserts, and the deferred retire keeps the
generation authoritative. The empty-write edge still clears stale rows: an empty
union keep-set retires every fact/index row for the generation
(`TestStreamingSearchDocumentWriteEmptyFinalizeRetiresAll`).

Failing-first tests (red before the change, green after):
- `TestEshuSearchDocumentHandlerStreamsBoundedPages` — handler projects+inserts
  once per loader page and runs Finalize (retire) exactly once with aggregated
  evidence counts equal to the sum across pages.
- `TestStreamingSearchDocumentWriteRetiresOnceWithUnionKeepSet` — no
  retire-by-absence runs during `InsertPage`; exactly one fact retire runs at
  `Finalize` with the union keep-set.
- `TestStreamSearchDocumentSourcesPaginatesEntitiesWithLimit` /
  `…PaginatesFilesWithLimit` — bounded keyset queries carry `LIMIT`, the cursor
  strictly advances, and all rows are yielded with no gaps/duplicates.

Verification: `go test ./internal/reducer ./internal/storage/postgres
./cmd/reducer -count=1` (3311 tests) passes; `go vet` on the same packages is
clean; `golangci-lint run ./...` reports 0 issues.

Observability Evidence: the per-cycle operator signal is unchanged in name and
shape. The handler still emits the `eshu search document projection cycle
completed` structured log with `scope_id`, `generation_id`, `considered`,
`included`, `skipped`, `written`, `retired`, `duration_seconds`, and `domain`,
and still records `CanonicalWrites` / `CanonicalWriteDuration`. The
`considered`/`included`/`skipped` counts are now aggregated across streamed pages
(`SearchDocumentCurationSummary.merge`) so the summary stays accurate. The writer
still emits the `eshu_dp_search_index_mutations_total`,
`eshu_dp_search_index_errors_total`, and
`eshu_dp_search_index_write_duration_seconds` instruments and the
`SpanReducerEshuSearchIndexWrite` span; `InsertPage` and `Finalize` each open one
such span, so an operator now sees per-page write spans plus one finalize span
rather than a single combined span. Operators read per-cycle `duration_seconds`
and `written`/`considered` to confirm bounded work; note that `duration_seconds`
now reflects the full streamed cycle while peak memory and per-page latency are
bounded. No new metric name, label, queue domain, worker, lease, or runtime knob
was added.

## Refresh-First Intent Dedup Ordering (#3451)

Performance Evidence: baseline — `shared_projection_intents` for domains
`inheritance_edges` and `sql_relationships` accumulated 15,031 pending intents
(12,227 + 2,804) that were flat for an extended period. The root cause:
`LatestIntentsByRepoAndPartition` in `shared_projection_batch_selection.go`
re-sorted deduplicated candidates by `(created_at ASC, intent_id)` only,
discarding the `is_refresh_intent DESC` primary ordering the SQL
(`listPendingDomainPartitionIntentsSQL`) established in #3474. Refresh intents
created after their paired edge intents ranked at positions 957–1,492 in a
1,496-row partition. With `SelectPartitionBatch` truncating at 200 rows, all 66
refresh intents for `inheritance_edges` and all 5 for `sql_relationships` were
permanently excluded from every batch — the refresh fence never opened, per-edge
rows remained deferred indefinitely, and `pending_projection` outstanding did not
drain. After promoting `is_refresh_intent DESC` to the primary sort key in the
in-memory comparator (darwin/arm64, local stack), all 66 `inheritance_edges`
refresh intents completed within ~2 minutes of deploying the fixed image, and
the pending count began dropping from 12,227 at a rate of ~500–1,000 intents per
polling interval, confirming the fence opened and edge intents were being
selected and written. Backend: Postgres 16 via `shared_projection_intents`
table; input shape: 1,496-row partition for `(scope_id, repo_id)`,
`is_refresh_intent` generated column from `payload->>'action'='refresh'`. The
fix is a pure comparator change in the in-memory dedup step — zero SQL, zero
schema, zero allocation change.

No-Regression Evidence: `TestLatestIntentsByRepoAndPartitionKeepsRefreshFirst`
was written first (red: `latest[0].IntentID` was `"edge-old"` before the fix,
proving the refresh intent was buried). After promoting the refresh-first sort,
the test is green (`latest[0].IntentID = "refresh"`). Full suite:
`go test ./internal/reducer ./internal/storage/postgres -count=1` — 3,351 tests
pass. `go vet ./internal/reducer/... ./internal/storage/postgres/...` clean.
`golangci-lint run ./internal/reducer/... ./internal/storage/postgres/...`
reports 0 issues.

No-Observability-Change: `LatestIntentsByRepoAndPartition` is a pure in-memory
dedup function. The fix changes only the `sort.SliceStable` comparator — no
metric name, metric label, span name, log key, queue domain, worker, lease,
runtime knob, graph write route, or Cypher statement was added, changed, or
removed. Operators continue to observe this path via existing
`pending_projection` outstanding counts, partition lease rows, reducer
`processed_intents`/`blocked_readiness` cycle logs, and
`selection_duration_seconds` instrumentation.
