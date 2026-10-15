# #7746: hybrid ranker determinism and full-page ranking

This note records two defects that block the score-keyed cursor in #7746, and
their fixes. The cursor itself is later work and is not part of this change.

Branch `fix/7746-ranker-determinism`, base `origin/main` `88beb12d0`.

## Defect 2: BM25 score bits depended on map iteration order

`searchhybrid.(*Index).bm25ScoredInScope` and `(*Index).bm25Score` used
`for term := range queryTerms` to add per-term BM25 contributions. Go
randomizes map iteration, and float addition is not associative. A document
that matched three or more query terms could therefore get different score
bits, and a different tie order, on identical requests. #7737 fixed the same
class in the persisted Postgres index with an ordered `SUM`.

Fix: both functions now sum in sorted term order (`sortedQueryTerms`). The
in-process and Postgres paths each produce stable bits. They are not equal to
each other, because they order on different keys and group the BM25 formula
differently.

RED: `TestBM25ScoreBytesStableAcrossTermOrder` uses 8 terms and 4 query-term
maps, with 200 scorings of each map. On `88beb12d0` it failed in every run:
5/5 runs (ledger:7746-bm25-order-red-runs). In the first run the off-reference
counts were 454/800 runs for `bm25Score` (ledger:7746-bm25-order-red-direct),
448/800 runs for `bm25ScoredInScope` (ledger:7746-bm25-order-red-postings),
and 102/200 runs for the keyword score (ledger:7746-bm25-order-red-candidate).
The failure text is
`bm25Score bits differ from sorted-term reference in N/800 runs`.

GREEN: 0/20 runs failed after the fix (ledger:7746-bm25-order-green-runs).

Other float accumulations in `searchhybrid` were checked. None of them depends
on order:

- `rrfFuse` (`rrf.go`) iterates a slice of ranked lists in fixed order.
- `cosineSimilarity` iterates vector slices.
- The approximate vector retriever writes one independent score per map key
  and does not accumulate.
- `searchembed.HashEmbedder.Embed` already sorts terms before it accumulates.

## Defect 1: pages of 101 to 200 rows were never hybrid-ranked

The handoff assumed that rows past the 100-document index cap were dropped as
overflow and appended unranked. The code shows a worse failure.
`CodeHybridRanker.Rerank` and `ContentHybridRanker.rankDocuments` set
`searchretrieval.Request.Limit = len(docs)` and called
`searchretrieval.ValidateRequest`. `Backend.Search` validates again.
`searchretrieval.maxLimit` is 100, so a page with more than 100 projectable
rows failed validation and the ranker returned `applied=false`.

Before the fix, a `find_code` content-fallback page, or a
`search_entity_content` / `search_file_content` page, with 101 to 200
projectable rows:

- kept its whole lexical (offset) order;
- carried no `search_backend=hybrid` row marker and no `page_position`;
- reported `source_backend=postgres_content_store` for `find_code`, not
  `hybrid_content_store`.

That page still paid to project the rows, embed them, and build the index
before validation threw the work away. The 100-document cap
(`codeHybridRerankLimit`) was also below the 200-row page ceiling
(`CodeSearchRankedWindow`). The content ranker's 200 cap and the public docs'
"hard cap 200" did not hold.

Fix:

- New `searchhybrid.Backend.RankAll(ctx, query, scope)` ranks every in-scope
  document of the request-local index in hybrid mode. Its fusion pool is the
  whole index. It is bounded by the index's `MaxDocuments` cap, not the public
  retrieval limit.
- `RankAll` shares an unexported `rank` core with `Search`. A golden digest of
  `Search` output, taken before the refactor, still matches after it
  (`TestBackendSearchOutputIsPinned`): 90 mode/limit/cap/query cases, with
  candidate order, score bits, and metadata.
- `RankAll` errors on a blank query, a missing scope or index, or a done
  context, and both rankers keep the lexical order on any error. Overflow uses
  the existing drop-by-id rule and per-candidate truncation signal
  (`TestRankAllKeepsIndexOverflowSignal`).
- `codeHybridRerankLimit = CodeSearchRankedWindow` (200). The content ranker
  keeps `contentHybridRerankLimit = 200`.
- `searchretrieval.maxLimit` stays 100. `semantic_search`, `searchpostgres`, and
  `searchnornicdb` validation is unchanged.

Behavior change: offset paging changes only for requests whose page has 101 to
200 projectable rows while the ranker is enabled. The ranker is built only
when the runtime's semantic search is on, so deployments without it see no
change. Such a page is now reordered by fused rank like any smaller page. Its
rows carry `search_backend=hybrid` and `page_position`, the row's index in the
offset order. The MCP budget page reads `page_position` to cut a reordered
page back to a prefix of the offset order (#7725). This fixes the documented
behavior. It is not the cursor feature.

RED before each ranker fix:

- Code, on `bc3fd3767`: 3/3 runs failed (ledger:7746-code-max-page-red-runs).
- Content, on `1d98d612e`: 3/3 runs failed (ledger:7746-content-max-page-red-runs).

Failure text:

```text
code_hybrid_rerank_test.go:80: Rerank applied = false, want true for a projectable maximum page
content_hybrid_rerank_max_page_test.go:65: RerankEntities applied = false, want true for a projectable maximum page
content_hybrid_rerank_max_page_test.go:93: RerankFiles applied = false, want true for a projectable maximum page
```

GREEN: `TestCodeHybridRerankRanksAllRowsOfAMaximumPage` and
`TestContentHybridRerankRanksAllRowsOfAMaximumPage` (entities, files) pass.
All 200 rows come back `search_backend=hybrid`, and the strongest match,
which has the highest id, ranks first.

## Performance Evidence

Host: shared 16-core arm64, Go 1.26.9, load average 6 to 23 during the runs.
Wall-clock numbers are not gating. Bytes and allocations are deterministic and
are the comparison. Each line is 5 runs (`-count=5 -benchmem -run '^$'`).

`BenchmarkCodeHybridRerank` (`go/internal/query/codemodel`) runs
`CodeHybridRanker.Rerank` end to end over N projectable rows with about 3 KB
`source_cache` bodies. That covers projection, index build with hash embedding,
ranking, and reorder.

| Rows | Commit | ns/op (non-gating) | B/op | allocs/op | applied |
|---|---|---|---|---|---|
| 100 | `88beb12d0` (origin/main) | 9819476 to 10089606 | 11107153 to 11107287 | 6005 to 6006 (ledger:7746-code-rerank-100-main-allocs) | 1 |
| 100 | `bc3fd3767` (BM25 order) | 9660761 to 9712469 | 11107175 to 11107184 | 6006 (ledger:7746-code-rerank-100-bm25-allocs) | 1 |
| 200 | `bc3fd3767` (cap 100) | 11050028 to 11128917 | 11439134 to 11439157 (ledger:7746-code-rerank-200-before-bytes) | 7153 (ledger:7746-code-rerank-200-before-allocs) | 0 |
| 100 | `1d98d612e` (RankAll) | 9879470 to 11106963 | 11107211 to 11107411 | 6006 to 6008 | 1 |
| 200 | `1d98d612e` (RankAll) | 20664122 to 21345694 | 22176291 to 22176468 (ledger:7746-code-rerank-200-after-bytes) | 11586 to 11587 (ledger:7746-code-rerank-200-after-allocs) | 1 |

What the numbers show:

- The BM25 order fix adds one allocation (the sorted term slice) and about
  24 B per re-rank.
- A 100-row page costs the same as before.
- A 200-row page now costs about 1.94x the bytes (22176305 vs 11439145 B/op),
  1.62x the allocations (11587 vs 7153), and about 1.9x the wall time
  (about 21 ms vs 11 ms) of the old path. The old path did most of the work and
  then discarded it. The page is now actually ranked. This costs more CPU per
  maximum page, and nothing in this change is faster.
- The cost grows linearly with rows: about 110 KB and 56 allocations per added row.

`BenchmarkRankAll` (`go/internal/searchhybrid`) runs `Backend.RankAll` alone
over a prebuilt index with the shipped hash embedder and about 3 KB bodies:

| Documents | ns/op (non-gating) | B/op | allocs/op |
|---|---|---|---|
| 100 | 211057 to 211958 | 99542 to 99543 (ledger:7746-rankall-100-bytes) | 261 |
| 200 | 456091 to 459124 | 189942 to 189943 (ledger:7746-rankall-200-bytes) | 667 |

RankAll is about 2% of a re-rank. A memory profile of the 200-row re-rank puts
93.9% of allocated bytes in `searchhybrid.NewIndex`. Tokenization
(`strings.FieldsFunc`) takes 41.7% of bytes, and each document's text is
tokenized twice: once for BM25 and once inside the hash embedder. That is the
lever if the re-rank cost matters. This change does not touch it.

The work stays bounded by `codeHybridRerankLimit` / `contentHybridRerankLimit`
(200 documents) and by the existing 2 s re-rank timeout. It adds no Cypher, no
SQL, no graph write, no queue, no lease, and no goroutine.

## No-Observability-Change

No-Observability-Change: no span, metric, or log covers the in-process
re-rank stage; the `rg` search for rerank telemetry in `internal/query/codequery`,
`internal/query/codemodel`, and `internal/query/contentread` found none. The
existing response signals stay as they are: the per-row `search_backend=hybrid`
marker, `page_position`, and for `find_code` the `source_backend=hybrid_content_store`
envelope field. They now also appear on 101 to 200 row pages, where an
operator previously saw only lexical markers. The BM25 order fix changes no
runtime shape. The cap change is bounded by the existing
`CodeSearchRankedWindow` constant.

## Commands

From `go/` with `GOCACHE=/tmp/g7746cache GOTMPDIR=/tmp/g7746`:

```bash
go test ./internal/searchhybrid/ -run TestBM25ScoreBytesStableAcrossTermOrder -count=20
go test ./internal/searchhybrid/... -count=1
go test ./internal/query/codemodel/ -run TestCodeHybridRerankRanksAllRowsOfAMaximumPage -count=3 -v
go test ./internal/query/contentread/ -run TestContentHybridRerankRanksAllRowsOfAMaximumPage -count=3 -v
go test ./internal/query/codemodel/ -run '^$' -bench BenchmarkCodeHybridRerank -benchmem -count=5
go test ./internal/searchhybrid/ -run '^$' -bench BenchmarkRankAll -benchmem -count=5
```
