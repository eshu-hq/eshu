# reducer Evidence Notes

Keep this file for scoped reducer evidence that is too detailed for the package
orientation README.

## #4234 — eshu_search_index_terms document-keyed index (2026-06-29)

Superseded note: this section records the historical #4234 fix for the prior
per-document refresh-delete shape. #4529 later removed the document-keyed term
refresh/retire path and replaced it with a generation-scoped term clear before
page inserts, so new schemas should not keep
`eshu_search_index_terms_doc_idx` for the current writer lifecycle.

### Problem

`eshu_search_index_terms` has PK `(scope_id, generation_id, term_key,
document_id)`, whose left prefix covers BM25 term lookup. The former writer
lifecycle had two hot DELETEs filter by `(scope_id, generation_id,
document_id)`:

- refresh terms for listed documents — `document_id = ANY($3::text[])` — fired
  on **every per-page write** (hot path).
- retire terms absent from the final keep-set — `document_id <> ALL($3::text[])`
  — fired on every Finalize (retire pass).

Neither the PK nor the lookup index has `document_id` usable after
`(scope_id, generation_id)`, so the planner was forced to scan the entire
`(scope, generation)` PK slice — up to **4.75 M rows per scope** on the
43 GB / 73.5 M-row table.

### Fix

Added index:

```sql
CREATE INDEX IF NOT EXISTS eshu_search_index_terms_doc_idx
    ON eshu_search_index_terms (scope_id, generation_id, document_id);
```

Migration: `go/internal/storage/postgres/migrations/037_eshu_search_index_terms_doc_idx.sql`.

### Hermetic before/after EXPLAIN — `TestEshuSearchIndexTermsDocumentDeletePlanLive`

The gate test creates a throwaway table with the identical schema as
`eshu_search_index_terms` (same PK, no FK constraints),
seeds 300 000 rows (20 background scopes × 100 docs × 100 terms, plus 1 proof
scope × 500 docs × 200 terms), and runs `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`
on the `= ANY` DELETE for 10 target documents inside a rolled-back transaction.
All numbers below are reproduced exactly by running the gate test.

**BEFORE** (no doc index — PK used, full (scope,gen) slice scan):

```
Node Type:         Index Scan
Index Name:        <throwaway>_pkey
Index Cond:        scope_id = $1 AND generation_id = $2 AND document_id = ANY($3)
Index Searches:    1
Shared Hit Blocks: 3505   Shared Read Blocks: 0
Actual Total Time: 13.078 ms
Total Cost:        2359.95
```

The PK must traverse the full (scope,gen) range (100k rows, sorted by
term_key) to find matching document_ids. Each term_key page must be visited
even though only 10 out of 500 documents are targeted.

**AFTER** (with doc index — direct document seek):

```
Node Type:         Index Scan
Index Name:        <throwaway>_doc_idx
Index Cond:        scope_id = $1 AND generation_id = $2 AND document_id = ANY($3)
Index Searches:    1
Shared Hit Blocks: 2000   Shared Read Blocks: 5
Actual Total Time: 0.718 ms
Total Cost:        1555.42
```

**Buffer block improvement: 42.8% (3505 → 2005 blocks) in the hermetic fixture.**

The hermetic fixture is entirely hot in shared_buffers so the absolute
improvement is modest compared to cold-disk production workloads. The
structural proof is: the planner selects the doc index (asserted by the test)
and reads fewer blocks. On the production 43 GB / 73.5 M-row table with
cold data the improvement is substantially larger (supplementary one-off
observation below).

### Supplementary production-scale observation (one-off, not the gate)

A single manual run against the live database while the doc index was
temporarily created on `eshu_search_index_terms` targeting a scope `<scope>`
/ generation `<generation>` with 4 750 236 term rows:

```
BEFORE: PK slice scan, 7487 index searches, ~49 307 blocks, 358 ms
AFTER:  doc index seek, 1 index search, ~12 blocks, 0.064 ms  (~99.97% reduction)
```

This is supplementary context for the production benefit magnitude. The
gate (`TestEshuSearchIndexTermsDocumentDeletePlanLive`) is the reproducible proof.

### Write-amplification

One extra B-tree entry per term INSERT/DELETE. Each document has O(200) terms,
so the overhead is modest and clearly worthwhile given the refresh DELETE fires
on every per-page write.

### Migration-lock risk

`CREATE INDEX IF NOT EXISTS` takes a table-level lock during the build phase.
Acceptable for fresh-corpus bootstrap (empty table). An operator adding this
index to a populated production database should use `CREATE INDEX CONCURRENTLY`
out-of-band to avoid locking writers.

### Classification

**Handler win + Wall-clock win**: the planner chose a correct but expensive
full-slice scan. The doc index makes the DELETE semantically identical while
reducing buffer I/O and execution time.

### Observability

**No-Observability-Change:** existing `eshu_dp_search_index_mutations_total`
and `eshu_dp_search_index_write_duration_seconds` metrics cover both paths and
will reflect the improvement automatically.

**No-Regression Evidence:** golden-corpus gate passes; unit test suite
(3643 tests across `postgres` and `reducer` packages) passes.

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
3. `eshuSearchIndexClearGenerationTermsQuery` — one `DELETE` for the
   `(scope_id, generation_id)` term slice before page writes.
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

## Search-Term Lookup Index Write Amplification (#4529 follow-up)

Performance Evidence: post-#4536 current-main bounded proof still showed
`eshu_search_document` term writes as the top measured reducer cost:
`index_term_upsert_seconds=629.308s` across 24 cycles, avg `26.221s`, max
`44.342s`, and `0.005964s` per written document. Schema inspection found
`eshu_search_index_terms_lookup_idx(scope_id, generation_id, term_key)` was
duplicating the left-prefix lookup already provided by the primary key
`(scope_id, generation_id, term_key, document_id)`. The branch removes that
extra B-tree maintenance from new schemas and adds a concurrent drop migration
for existing schemas. Local proof is the red/green schema contract
`TestBootstrapDefinitionsAvoidRedundantSearchTermLookupIndex` plus the
skipped-by-default live Postgres plan proof
`TestEshuSearchIndexTermsBM25LookupUsesPrimaryKeyPrefixLive`, which creates a
throwaway table with only the primary key and verifies BM25 term lookup uses the
primary-key prefix without a sequential scan. Live throwaway Postgres 16 proof
reported the BM25 lookup plan using the table primary key with 42 shared buffer
hits, no lookup index, and no sequential scan; the sibling document-delete plan
proof still used `eshu_search_index_terms_doc_idx` and reduced shared blocks
from 2,837 to 231 on the same bounded fixture.

Remote bounded proof: run
`4529-drop-term-lookup-current-main-20260702222508` tested Eshu commit
`87d0a8c01946` against NornicDB `main` image `eshu-nornicdb-main:5646d7ee` with
the same current corpus/backend profile as the post-#4536 current-main sample
and stopped after 25 `eshu_search_document` cycles. It wrote 105,845 documents;
`index_term_upsert_seconds` was 457.219s total, avg 18.289s, max 23.518s, and
0.004320s per written document. Compared with the post-#4536 current-main proof
(avg 26.221s, max 44.342s, 0.005964s/doc), this is about 30% lower average
term-write time, 47% lower max term-write time, and 28% lower term-write cost
per document. The run was bounded and cleaned up its Compose project and
volumes immediately after evidence collection.

No-Regression Evidence: the migration changes only index maintenance for
`eshu_search_index_terms`. Table columns, primary key, active-generation reads,
BM25 term equality, reducer worker counts, queue semantics, vector writes, graph
writes, and API/MCP response contracts are unchanged. The primary key continues
to serve `(scope_id, generation_id, term_key)` BM25 joins. A later #4529
generation-clear slice removes the document-keyed term refresh/retire path and
drops `eshu_search_index_terms_doc_idx`; this lookup-index slice did not depend
on that later lifecycle change.

No-Observability-Change: no metric, span, log key, queue domain, worker,
runtime knob, route, or graph query changes. Operators still diagnose this path
through `eshu_search_document` cycle logs,
`eshu_dp_search_index_write_duration_seconds` operation labels, Postgres query
duration instrumentation, and Postgres wait-event samples.

## Search-Term COPY Fast Path (#4529 follow-up)

Performance Evidence: post-#4544 current-main bounded proof still showed
`index_term_upsert` as the remaining `eshu_search_document` long pole:
385.248s of 451.919s total cycle time across 21 cycles, avg 18.345s, max
27.962s, and 0.004145s per written document. Local Postgres 16 throwaway proof
(`ESHU_SEARCH_INDEX_TERM_COPY_LIVE=1`) compared the current unnest INSERT shape
against pgx `CopyFrom` on the same `eshu_search_index_terms` table shape:
300,000 rows, primary key `(scope_id, generation_id, term_key, document_id)`,
and document index `(scope_id, generation_id, document_id)`. The production
SQLDB copy helper wrote the same rows to a throwaway table in 2.062689875s
versus 2.842917542s for the unnest INSERT, about 27% lower local write time. A
separate local order proof rejected changing the sort key: `ORDER BY term_key,
document_id` remained fastest at ~953 ms average for 300,000 rows versus
~1,264 ms for `document_id, term_key`, so the COPY fast path preserves the
primary-key locality order instead of moving the bottleneck to the PK index.

No-Regression Evidence: `TestWriteEshuSearchDocumentsUsesTermCopierWhenAvailable`
was written first and failed before the writer exposed the copier path, then
passed after the writer sorted term rows by `(term_key, document_id)` and used
`CopySearchIndexTerms` when available. Existing term-key and idempotency guards
still pass: `TestWriteEshuSearchDocumentsUsesBoundedSearchTermKeys`,
`TestWriteEshuSearchDocumentsTermInsertAvoidsConflictUpdate`, and
`TestWriteEshuSearchDocumentsBatchedWritesBoundedExecCount`. The live proof
`TestEshuSearchIndexTermCopyFromLive` creates throwaway tables and verifies both
paths write the same row count before comparing timings.

Observability Evidence: the reducer still records
`eshu_dp_search_index_write_duration_seconds{operation="term_upsert"}` and the
`eshu_search_document` completion log still emits `index_term_upsert_seconds`.
The `InstrumentedDB.CopySearchIndexTerms` wrapper records the same
`eshu_dp_postgres_query_duration_seconds{operation="write",store="reducer"}`
metric shape used by normal Postgres writes and wraps COPY in a
`postgres.copy_from` span. No queue domain, worker count, lease behavior,
runtime knob, table schema, graph write, route, or API/MCP contract changes.

## Search-Term Generation Clear And Document-Index Removal (#4529 follow-up)

Benchmark Evidence: post-#4544 current-main bounded proof still showed
`index_term_upsert` as the remaining `eshu_search_document` long pole:
385.248s of 451.919s total cycle time across 21 cycles, avg 18.345s, max
27.962s, and 0.004145s per written document. A COPY protocol experiment was a
weak win only: normalized total cycle cost moved from 0.004863s/doc to
0.004545s/doc and term-upsert cost from 0.004145s/doc to 0.003846s/doc, while a
2s Postgres locktrace still showed `Lock/extend` on
`eshu_search_index_terms_pkey`. Focused Postgres 16 scratch benchmarks then
isolated the write-amplifying index layout: with 16 concurrent workers and
1.344M rows, the current primary key plus document-keyed secondary index wrote
in 3.544963993s, while the same primary-key-only shape wrote in 2.35473377s,
about 33.6% faster. Hash partitioning alone was weaker, improving the same
class of concurrent insert proof by about 6.8% to 9.9%.

The branch changes the reducer write lifecycle so search terms are cleared once
per `(scope_id, generation_id)` before page inserts, then terms are inserted
without the per-page document-keyed refresh delete and finalize no longer runs a
document-keyed term retire. This preserves generation-authoritative semantics:
retrying a generation starts from an empty term slice, each page inserts current
terms, document rows/facts still retire by the full keep-set at finalize, and
active-generation reads ignore superseded generations. Existing same-scope
reducer queue conflict fencing remains the idempotency boundary; no reducer
worker count, lease, batch size, queue domain, graph write, route, or API/MCP
contract changes.

No-Regression Evidence: `TestWriteEshuSearchDocumentsClearsGenerationTermsOnce`
fails if a page write uses a document-keyed term delete or clears terms more
than once. `TestEshuSearchIndexTermClearUsesGenerationPrefix` locks the clear
query to `(scope_id, generation_id)` and rejects `document_id`.
`TestEshuSearchIndexRetireDocumentsNoLongerRetiresTerms` proves finalize does
not retire term rows by document.
`TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex` proves existing
schemas get `DROP INDEX CONCURRENTLY IF EXISTS
eshu_search_index_terms_doc_idx`.

Observability Evidence: operators continue to diagnose this path through the
existing `eshu_search_document` completion log fields,
`eshu_dp_search_index_write_duration_seconds{operation="term_refresh"}` for the
generation clear, `operation="term_upsert"` for term insertion, term-retire
mutation counters for rows removed by the generation clear, Postgres query
duration spans, and wait-event samples. No new metric name, high-cardinality
label, worker knob, runtime knob, queue domain, or graph signal is introduced.

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
   (insert-only: generation term clear once, bulk fact upsert, index-doc upsert,
   bulk term upsert; NO retire) -> `Finalize` (single authoritative
   retire-by-absence over the union written-id keep-set for facts and index
   documents, then stats). Accumulating only the keep-sets (~tens of bytes per
   id, ~13MB for 159K ids vs. 94MB content) keeps peak content memory bounded to
   one page while preserving the existing generation-authoritative retire
   semantics. `WriteEshuSearchDocuments` is retained and re-implemented as
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

## Code-Import Repo-Edge Stale Retract + baseUrl Fabrication Guard (#3651)

These two fixes address review-bot findings on merged PR #3645
(code-import repo→repo `DEPENDS_ON` edges, issue #3642). Both touch the
code-import projection hot path that emits shared repo-dependency intents, so
they are recorded here.

Performance Evidence: the change adds no new graph write, Cypher statement,
worker, lease, batch, or queue-claim path. P1 adds one extra in-memory pass,
`BuildCodeImportRepoEdgeRefreshIntents`, over the same `file` fact envelopes the
upsert builder already scans — O(files × imports per file) with a single
`map[string]struct{}` of consumer ids and an early `break` per consumer once it
is marked covered, so it is at worst a second linear sweep and emits at most one
retract row per consumer that produced no upsert. The handler appends those
refresh rows to the existing `UpsertIntents` call rather than issuing a second
write, so the shared-intent write path keeps the same row-batch shape and the
downstream DEPENDS_ON `MERGE` keyed on `(source_repo, target_repo)` stays
idempotent. P2 is a constant-time guard (`isRepoRelativeResolvedSource`: a
`strings.Contains` plus three `strings.HasPrefix` checks) inside the existing
`codeImportEntrySource` selection, with no new allocation. Baseline before the
fix: a full snapshot that dropped all of a consumer's resolved imports wrote
zero rows and left the prior `projection/code-imports` edge graph-visible
forever; after the fix the same snapshot writes exactly one retract row for that
consumer and the lane removes the stale edge. Input shape: per-repo `file` facts
bounded by the git-repository scope (unchanged from #3645); owner index bounded
by the active package-registry generation (unchanged). Backend: NornicDB default
canonical graph via the shared repo-dependency projection lane.

No-Regression Evidence: both fixes were written test-first (red before green).
P1 red cases: `TestBuildCodeImportRepoEdgeRefreshIntentsEmitRetractForZeroOwners`,
`...CoveredConsumerExcluded`, `...SelfOnlyConsumerRetracted`, `...Idempotent`,
and handler-level
`TestCodeImportRepoEdgeHandlerEmitsRefreshIntentWhenOwnerlessFullSnapshot` all
failed with `BuildCodeImportRepoEdgeRefreshIntents` undefined, then passed after
the builder and handler wiring landed; the pre-existing
`TestCodeImportRepoEdgeHandlerSkipsUnresolvedImport` was updated to assert the
now-correct single retract call. P2 red cases:
`TestBuildCodeImportRepoDependencyIntentsTsConfigBaseUrlDropped` (a TS file whose
`resolved_source` is `src/components/Button` with an owned package named `src`
in the index) produced one fabricated edge before the fix and zero after, and
the `TestCodeImportEntrySourceRepoRelativeResolvedSourceDropped` table confirms
the fallback keeps bare/scoped specifiers while dropping repo-relative paths.
Full suite: `go test ./internal/reducer -count=1` — 2,370 tests pass.
`go build ./...` succeeds. `gofmt -l` on all changed files is clean.
`golangci-lint run ./internal/reducer/...` reports 0 issues.

Observability Evidence: P1 adds one operator counter sample,
`code_import_repo_edges` with `outcome="refreshed_no_owner"`, emitted via
`emitRefreshCounter` so operators can see how many consumers were retracted for
lost ownership in a generation; the existing `considered`/`written`/`skipped_*`
outcome counters and the structured `code import repo edge projection completed`
log are unchanged. No new metric name, span name, queue domain, worker, lease,
runtime knob, or Cypher statement is introduced. P2 changes only which import
specifier feeds owner lookup and surfaces through the existing
`skipped_relative`/`skipped_no_owner` counters already on this path, so no
observability surface changes for it.
