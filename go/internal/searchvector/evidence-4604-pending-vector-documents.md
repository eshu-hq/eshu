# Pending Search-Vector Document Build Evidence (#4604)

## Root Cause

The search-vector side runner already listed pending repository scopes, but the
builder then loaded every active search document in each selected scope. A scope
with one missing or stale vector row could therefore rewrite the whole
repository's vector sidecar rows. At corpus scale this amplified the reducer
tail: the July 3, 2026 15-minute graph-permit run completed 92 vector sweeps
over 1,225,794 document/vector rows while the live search-document index held
988,600 documents at the stop point.

## Fix

`postgres.EshuSearchDocumentStore` now exposes `ListPendingVectorDocuments`,
which applies the same ready/disabled metadata and value-row predicate as the
pending-scope selector, but scoped to one repository and bounded by the builder
document limit. `searchvector.Builder` prefers that narrower reader when the
document store supports it. The legacy active-document paging path remains as a
fallback for non-Postgres adapters and older tests.

The pending-document query intentionally does not use `OFFSET`: writing ready
rows shrinks the pending set, so offset pagination can skip unbuilt documents.
Each runner sweep reads one bounded pending page per scope; subsequent sweeps
pick up any remaining missing/stale documents.

## No-Regression Evidence:

`go test -timeout 180s ./internal/searchvector/... ./internal/storage/postgres/... ./internal/reducer/... ./cmd/reducer -count=1`
passed locally after the change.

Focused regression coverage:

- `TestBuilderUsesPendingVectorDocumentsWhenAvailable` proves a builder with
  five active documents and two pending documents calls
  `ListPendingVectorDocuments` once, does not call `ListActiveDocuments`, and
  writes only the two pending vector rows.
- `TestEshuSearchDocumentStoreListsPendingVectorDocuments` proves the Postgres
  query is active-generation scoped, provider/model/version scoped, requires a
  value row for ready metadata, treats disabled metadata as complete, and has no
  `OFFSET` clause.
- Existing #4430 batching tests still prove each built page uses batched
  metadata/value upserts rather than per-document round trips.

## Performance Evidence:

Baseline: the July 3, 2026 full-corpus bounded run on current main
(`bfbdd0a0`) with NornicDB image `eshu-nornicdb-main:5646d7ee` stopped after
15 minutes with 92 search-vector sweeps processing 1,225,794 documents/vectors.
Those sweeps spent 751.803s total: 328.672s in query/load, 14.960s in
embedding, and 380.182s in writes. The same stop snapshot had 988,600 indexed
search documents, so the side runner was doing repeated already-ready work.

After local proof: the focused builder regression reduces the exercised write
set from all active documents in a selected scope to exactly the pending
documents returned by the Postgres-capable reader. On the test input, the
effective build cardinality changes from five active documents to two pending
documents, with zero active-document fallback reads.

After bounded remote proof: `search-vector-pending-docs-cached-cap15-20260703T1105Z`
ran the full 895-repository corpus for the same 900s cap, same NornicDB image
(`eshu-nornicdb-main:5646d7ee`), and same tuned worker/graph-permit profile as
the baseline. The run stopped at the cap with `open=872 total=3065
succeeded=2193 failed=0 dead_letter=0 retrying=0`, `source_local=222
succeeded`, and `eshu_search_document=119 succeeded`. Search-vector sweeps
processed 201,224 documents/vectors across 12 sweeps, down from 1,225,794
documents/vectors across 92 sweeps in the baseline, an 83.6% reduction in
vector-build document work. Search-vector write time fell to 55.414s, but
query/load still consumed 546.025s; the next bottleneck is the pending-document
selection query, not vector upsert volume.

Follow-up selector-shape proof: the slow capped run included sweeps where
query/load consumed 210-212s while embedding consumed about 0.5s and batched
writes consumed 9-12s. The pending-document reader now scans
`eshu_search_index_documents`, the reducer-maintained one-row-per-document
BM25 read model, instead of scanning `fact_records` JSON payloads for the same
curated documents. The search-index document row now stores the same
`content_hash` used by vector metadata so stale-vector detection keeps the
provider/model/version/content identity check. Existing rows are backfilled
from the paired `fact_records` payload at schema bootstrap; new rows are written
by the search-document reducer writer. Local proof:
`go test -timeout 120s ./internal/storage/postgres ./internal/reducer
./internal/searchvector ./cmd/reducer -run
'Test(EshuSearchDocumentStoreListsPendingVectorDocuments|BootstrapDefinitionsIncludeEshuSearchIndex|BootstrapDefinitionsBoundEshuSearchIndexTermKeys|WriteEshuSearchDocumentsMaintainsPersistedSearchIndex|BuilderUsesPendingVectorDocumentsWhenAvailable|SearchVector|ProductionWiring)'
-count=1` and `go test -timeout 240s ./internal/searchvector/...
./internal/storage/postgres/... ./internal/reducer/... ./cmd/reducer -count=1`
passed locally.

Remote selector proof on `search-vector-index-docs-cap15-20260703T115240Z`
showed the persisted-document reader lowered many sweeps but still left an
ORDER BY-driven full-scope sort under live write load. The run was stopped
early to preserve the live database after 9 search-vector sweeps, 102,848
documents/vectors, `query_load=271.805s`, `embed=1.090s`, and
`write=33.669s`. Two selector phases were still too slow:
25 scopes / 12,000 documents at `query_load=174.500s`, and
79 scopes / 36,489 documents at `query_load=86.762s`.

On the preserved remote database, the ordered query for the largest pending
scope scanned and sorted all 86,746 search-index documents before returning
500 rows: `EXPLAIN (ANALYZE, BUFFERS)` reported 494.709ms quiesced execution.
Removing the ordering, which is not required for vector correctness because the
query is already bounded and idempotent, let Postgres stop after enough pending
rows: the same scope returned 500 rows in 82.789ms quiesced execution. A bounded
remote corpus rerun is still required before claiming wall-clock improvement
for the orderless selector rewrite.

Current local follow-up: the reducer runner now prefers a batch-capable vector
builder when the production wiring exposes one. Instead of issuing one
`ListPendingVectorDocuments` query per selected scope, the production
`searchvector.Builder` calls `ListPendingVectorDocumentsForScopes` once with the
selected active scopes. The Postgres query keeps the same active-generation,
provider profile, source class, model id, vector-index version, content-hash,
ready-value, and disabled-row semantics, but reads from a `VALUES`-backed
selected-scope CTE and one `JOIN LATERAL` pending-document probe per selected
scope. Each lateral probe remains capped by the configured per-scope document
limit, uses `eshu_search_index_documents`, has no `ORDER BY` or `OFFSET`, and
does not scan `fact_records` JSON payloads. This preserves the old per-scope
builder path as a fallback for stores without the batched interface.

Local proof for the batch selector:

- `TestSearchVectorBuildRunnerUsesBatchBuilderForPendingScopes` failed before
  the runner preferred the batch builder, then passed after it issued one
  batch build request for all selected scopes and zero serial per-scope build
  calls.
- `TestBuilderBatchUsesBatchedPendingVectorDocumentsWhenAvailable` failed
  before `Builder.BuildBatch` used the batch pending-document store, then
  passed after the builder issued one multi-scope pending read, no per-scope
  pending reads, and no active-document fallback reads. The same coverage keeps
  equal document IDs from different scopes distinct in vector upserts.
- `TestBuilderBatchRejectsMixedSourceKinds` rejects batch requests that do not
  share the same source-kind filter, preventing a mixed request from silently
  using the first scope's filter for the whole batch.
- `TestEshuSearchDocumentStoreListsPendingVectorDocumentsForScopes` proves the
  selected-scope SQL shape: `VALUES` CTE, `JOIN LATERAL`, active-generation
  join, selected scope/generation/repo predicates, per-scope `LIMIT`, persisted
  search-index-document source, and no `ORDER BY`, `OFFSET`, `fact_records`, or
  `fact.payload`.

Remote batch-selector proof: `search-vector-batch-selector-cap15-20260703T140409Z`
ran the same 895-repository corpus, same NornicDB image
(`eshu-nornicdb-main:5646d7ee`), and same tuned worker/graph-permit profile
after the batch selector change at commit
`e7378d9cb09fb48a6efc1eefce3839e7dc64eaa2`. The run reached the 900s cap with
`open=108 total=2340 succeeded=2232 failed=0 dead_letter=0 retrying=0`,
`source_local=169 succeeded`, and `eshu_search_document=156 succeeded`.
Search-vector sweeps processed 492,060 documents/vectors across 36 sweeps with
zero disabled or failed vector documents. The vector phase spent 618.345s
total: `scheduling=307.128s`, `query_load=147.689s`, `embed=5.780s`, and
`write=156.328s`. The worst query-load sweep was 11.872s for 100 scopes and
9,877 documents; the earlier pending-document reader proof had a 211.633s max
query-load spike while processing fewer total vectors.

Readout: the batch selector materially reduced the search-vector selector tail.
Compared to `search-vector-pending-docs-cached-cap15-20260703T1105Z`, vector
query/load dropped from 546.025s to 147.689s while processed vector rows rose
from 201,224 to 492,060. Compared to the original current-main baseline,
search-vector document/vector work is down from 1,225,794 to 492,060 rows and
write/upsert is down from 380.182s to 156.328s. Correctness stayed clean in the
capped window: failed, dead-letter, retrying, disabled vector, and failed vector
counts were all zero.

The next measured bottleneck is no longer search-vector pending-document
selection. The same capped run spent 1,638.790s across 156 search-document
projection cycles, with 1,281.019s in `index_term_upsert_seconds` and a 96.958s
max term-upsert cycle. That follow-up belongs in the search-document term
upsert path rather than in another search-vector selector rewrite.

## No-Observability-Change:

The change adds no route, graph query, queue table, worker, lease, runtime
knob, metric instrument, or metric label. The follow-up selector change adds
one search-index document column but no new runtime signal shape, and the batch
selector only changes how selected pending scopes are loaded into the existing
builder. Operators continue to diagnose the path through existing search-vector
sweep logs and `eshu_dp_search_vector_build_phase_seconds` split timing for
scheduling, query/load, embed/build, and write/upsert. The existing fields
should show lower `document_count`, `vector_count`, `query_load_seconds`, and
`write_upsert_seconds` when scopes have mostly ready vectors, and the batch
selector proof should specifically lower the serial per-scope query-load tail.
