# Versioned search-vector scope readiness evidence (#4233)

## Root cause

The original scheduler derived pending vector scopes by scanning active search
document facts across the corpus. The versioned scope-state candidate removed
that scheduler scan and added a terminal-count shortcut, but the preserved
896-repository run exposed six remaining failures:

1. Once terminal metadata count reached document count, the exact completion
   branch returned to `fact_records` JSON. A 3,121-document scope was still
   active after 159 seconds.
2. The vector builder recomputed `embedding_content_hash` after loading a JSON
   document instead of retaining the projected document's `content_hash`.
   Invalid UTF-8 is replaced during JSON persistence, so those tokens differed
   permanently. All 98 live mismatches contained the Unicode replacement
   character.
3. A `search_vector_ready` row published before later document projections
   remained visible while 28 vector scopes were building; the first guard also
   missed the earlier interval where an active document projection itself was
   `building` or `failed`.
4. The fixed 500-document per-scope selector cap repeated the same large-scope
   index walk hundreds of times after the pending set narrowed to one scope.
5. The one-time upgrade seeder retained a corpus-wide exact-ready proof over
   `fact_records`, making reducer startup inherit the disproven query shape.
6. Scope finalization used revision/fence CAS, but vector metadata and value
   writes did not re-check those tokens. A delayed worker could therefore
   overwrite rows after a newer worker had advanced the scope fence.

## Theory proof

The theory was measured before the production changes:

- Go JSON round-trip shim: the old searchable-text hash changed across invalid
  UTF-8 persistence; canonical valid UTF-8 produced identical before/after
  hashes.
- Preserved remote state: all 98 hash mismatches contained `U+FFFD`.
- OLD exact completion query: more than 159 seconds for 3,121 documents before
  cancellation.
- NEW exact completion query over `eshu_search_index_documents`: 29.020 ms for
  the same scope, 150.184 ms for an incomplete 253,206-document scope, and
  938.760 ms for a complete 99,877-document scope.
- OLD watermark probe returned one row while vector scopes were pending. The
  guarded probe returned zero rows in 0.590 ms with 158 shared-buffer hits.
- With one current projection changed transactionally from `ready` to
  `building`, the first guard still returned the old watermark in 5.538 ms;
  the corrected predicate returned zero rows in 2.535 ms. The transaction was
  rolled back.
- On the remaining 253,206-document scope, a 500-row pending read took
  1.491 seconds and a 10,000-row read took 1.544 seconds. The larger page did
  20 times the useful work for 3.6 percent more query time. Its document JSON
  payload was 12 MB, with a 1,225-byte average and 3,439-byte maximum row.
- Reconstructing all 896 projection rows from the persisted index took 690 ms.
  A correlated synchronous exact-ready seed was cancelled at 2 minutes 24
  seconds, and a global aggregate alternative hit its 120-second timeout. A
  conservative `building` seed inserted all 831 non-empty scopes in 45.6 ms.
- A rolled-back stale-worker shim advanced a vector scope to a newer ready
  build and then applied an older worker's metadata write. The scope remained
  ready while the exact completion audit found one incomplete document and the
  readiness watermark still returned a row. This proved that finalization CAS
  alone did not fence the underlying writes.

## Fix

- `searchhybrid.DocumentText` normalizes invalid UTF-8 before hashing and
  embedding.
- `EshuSearchDocumentRow` carries the persisted projection `ContentHash`.
  Vector metadata and values retain that token, including for data written by
  older binaries.
- `ScopeVectorComplete` keeps the terminal-count shortcut and runs its exact
  branch over `eshu_search_index_documents`, the same persisted projection the
  builder reads.
- The query-side watermark probe returns a row only when no active ready
  document projection has missing, non-ready, or stale-revision vector scope
  state for the configured identity.
- The batch runner keeps the default 500-row per-scope limit for broad sweeps,
  then divides a 10,000-document budget across fewer than 20 remaining scopes.
  One scope is capped at 10,000 documents. Explicit non-default limits do not
  expand.
- Startup seeds current scopes as `building`; exact verification and ready CAS
  remain owned by the bounded scheduler. Successful scope finalization counts
  as durable progress, avoiding the no-progress sleep during upgrade catch-up.
- Projection ready/failed CAS statements match the caller's generation ID as
  well as the active generation, revision, and build fence.
- The runner carries the `BeginBuilding` fence into every vector metadata and
  value row. Both batch statements independently join the active ingestion
  generation, ready projection revision, current building vector-scope fence,
  and projected document content hash before inserting or updating a row.
- `BeginBuilding` itself selects only the active ready projection revision and
  cannot regress a newer or already-ready vector scope. Ready publication uses
  exact fence equality and rechecks the current ready projection revision.
- Projection publication counts unique document IDs rather than attempted
  writes, so duplicate IDs cannot inflate `document_count` and hold the count
  gate permanently below completion.

## Accuracy and concurrency proof

The live Postgres semantic matrix compares the new exact branch with the old
fact reference for still-building, complete, ready-without-value, stale-hash,
disabled, and retired-extra cases. The verdicts match for every case. The
count-gate EXPLAIN shows the exact branch is never executed while terminal
count is below document count.

Focused race coverage runs the search, vector, Postgres, query, reducer, and
reducer-command packages with `-race`. Existing revision and build-fence tests
continue to cover generation advance, stale projection revision, retry, and
concurrent finalize behavior. A live Postgres regression first writes value and
metadata rows with the current fence, advances the fence, and then attempts to
overwrite both rows with the stale fence. The current writes land and both stale
writes affect zero rows; the original vector, metadata failure class, and update
timestamps remain unchanged. The same live test publishes a newer ready scope,
then proves that both an older projection revision and a delayed duplicate of
the already-ready revision are rejected without changing its revision, fence,
or state. Vector metadata and value stores retain their 500-row SQL statement
boundaries.

## Finished-work proof

The rebuilt reducer was restarted against the preserved worst-case remote tail
with one 253,206-document scope and 105,737 vectors remaining. It drained the
tail in 47.5 seconds. Full pages built 10,000 vectors per sweep in 4.0 to 6.6
seconds, followed by one 737-document page and a zero-pending sweep. The fixed
500-document cap would have required 212 repeated selector walks for the same
tail; the adaptive cap required 11.

The terminal audit reported:

- 2,583,358 projected documents, metadata rows, and vector-value rows;
- zero projection-to-vector content-hash mismatches;
- zero ready metadata rows without a matching vector value;
- 831 of 831 current non-empty repository scopes vector-ready at the current
  projection revision;
- 13,034 succeeded queue items and no other queue status;
- a newly published readiness watermark after convergence; and
- no residual long-running completion query.

The focused package tests, live Postgres semantic matrix, and scoped race tests
passed before the rebuilt-binary proof. `make pre-pr` then exercised the
repository-selected build, vet, tests, exactness, telemetry, package-doc, and
race lanes; its initial pass found only formatting, which was corrected before
the final gate rerun.

## Observability

No metric, span, label, route, worker, or runtime knob changes. Operators use
the existing `search vector build sweep completed` fields and
`eshu_dp_search_vector_build_phase_seconds` split for scheduling, query/load,
embed/build, and write/upsert. The read surface continues to report
`pending_search_vector`; the probe now removes false-fresh results from stale
watermarks.
