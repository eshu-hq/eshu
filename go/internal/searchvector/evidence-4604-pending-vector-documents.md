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

## No-Observability-Change:

The change adds no route, graph query, queue table, worker, lease, runtime
knob, metric instrument, or metric label. Operators continue to diagnose the
path through existing search-vector sweep logs and
`eshu_dp_search_vector_build_phase_seconds` split timing for scheduling,
query/load, embed/build, and write/upsert. The existing fields should show
lower `document_count`, `vector_count`, `query_load_seconds`, and
`write_upsert_seconds` when scopes have mostly ready vectors.
