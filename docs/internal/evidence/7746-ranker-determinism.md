# #7746: hybrid ranker determinism and full-page ranking

This note records two defects that block the score-keyed cursor in #7746, and
their fixes. The cursor itself is later work and is not part of this change.

Branch `fix/7746-ranker-determinism`. Commits are named by subject line, not
SHA, because each rebase onto a moving `origin/main` rewrites branch SHAs. In
order:

1. `fix(searchhybrid): sum BM25 contributions in sorted term order`
2. `fix(codemodel): hybrid-rank find_code pages of 101-200 rows`
3. `fix(contentread): hybrid-rank content pages of 101-200 rows`
4. `test(searchhybrid): benchmark RankAll at 100 and 200 documents`
5. `docs(search): record #7746 ranker determinism evidence`
6. `fix(contentread): tie the content re-rank cap to the page cap`
7. `docs(search): drop the stale re-rank validation claim`
8. `feat(query): add a query.hybrid_rerank stage span`
9. `docs(search): cite rebased commits and the stage span in #7746 evidence`
10. `docs(search): name #7746 evidence commits by subject`
11. `test(searchhybrid): pin Search scores to 8 digits, not raw bits`
12. `docs(search): record the cross-architecture score limit for #7746`

Every figure below was measured on the branch before rebasing onto
`origin/main`. `git range-diff` shows the patches unchanged across each rebase,
and `git diff` between the bases is empty for every measured package
(`searchhybrid`, `searchembed`, `searchdocs`, `searchretrieval`,
`query/codemodel`, `query/contentread`, `query/tracing`). The baseline rows were
measured with the branch on `88beb12d0` and the stage-span rows with the branch
on `ef5703ec2`. Both are commits on main, and the ledger rows cite them.

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
differently. The in-process bits are stable per binary, which means per
process and per architecture, not across architectures (see the known limit
below).

RED: `TestBM25ScoreBytesStableAcrossTermOrder` uses 8 terms and 4 query-term
maps, with 200 scorings of each map. On `88beb12d0`, a main commit, it failed
in every run:
5/5 runs (ledger:7746-bm25-order-red-runs). In the first run the off-reference
counts were 454/800 runs for `bm25Score` (ledger:7746-bm25-order-red-direct),
448/800 runs for `bm25ScoredInScope` (ledger:7746-bm25-order-red-postings),
and 102/200 runs for the keyword score (ledger:7746-bm25-order-red-candidate).
The failure text is
`bm25Score bits differ from sorted-term reference in N/800 runs`.

GREEN: 0/20 runs failed after the fix (ledger:7746-bm25-order-green-runs).

This test is valid on every architecture. It compares production against a
reference computed in the same binary, with the same `termContribution` and the
same summation order. No product feeds the summing add directly, so FMA fusion
cannot treat the two sums differently. It passed on the amd64 CI race shard
whose only failure was the raw-bits golden pin described below.

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
  (`TestBackendSearchOutputIsPinned`). It covers 90 mode/limit/cap/query cases
  and pins candidate order, ranks, metadata, and failures exactly, with each
  score to 8 significant digits. Digest provenance and mutation proof are in
  "Golden pin provenance" below.
- `RankAll` errors on a blank query, a missing scope or index, or a done
  context, and both rankers keep the lexical order on any error. Overflow uses
  the existing drop-by-id rule and per-candidate truncation signal
  (`TestRankAllKeepsIndexOverflowSignal`).
- `codeHybridRerankLimit = CodeSearchRankedWindow` (200), and
  `contentHybridRerankLimit = ContentSearchMaxLimit` (200), the page caps of
  their handlers. Both maximum-page tests size the page from the page cap, so a
  ranker cap below it fails them (seeded mutation recorded in
  `fix(contentread): tie the content re-rank cap to the page cap`).
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

- Code, with commit 1 applied: 3/3 runs failed (ledger:7746-code-max-page-red-runs).
- Content, with commits 1-2 applied: 3/3 runs failed (ledger:7746-content-max-page-red-runs).

Failure text, with the line numbers the test files had then:

```text
code_hybrid_rerank_test.go:80: Rerank applied = false, want true for a projectable maximum page
content_hybrid_rerank_max_page_test.go:65: RerankEntities applied = false, want true for a projectable maximum page
content_hybrid_rerank_max_page_test.go:93: RerankFiles applied = false, want true for a projectable maximum page
```

GREEN: `TestCodeHybridRerankRanksAllRowsOfAMaximumPage` and
`TestContentHybridRerankRanksAllRowsOfAMaximumPage` (entities, files) pass.
All 200 rows come back `search_backend=hybrid`, and the strongest match,
which has the highest id, ranks first.

## Known limit: score bits across architectures

A first version of the golden pin hashed raw `math.Float64bits` scores. It
passed on the arm64 dev host and failed on the amd64 CI race shard. Diffing the
two 424-line transcripts showed 21 score lines differing by at most 4 ulp
(for example `401361fb535fb255` on amd64 vs `401361fb535fb254` on arm64). Order,
ranks, metadata, and failures were identical on every line.

The cause is FMA fusion, which the Go spec permits. On arm64 the compiler fuses
the `termContribution` denominator `tf + k1*(1-b+b*len/avg)` into `FMADDD`
(`go build -gcflags=-S`). On amd64 at the CI's default `GOAMD64=v1` the same
code compiles to separate `MULSD`/`DIVSD`/add instructions with no `VFMADD`.

So BM25 score bits are stable per binary, which means per process and per
architecture. Across architectures a score can differ by a few ulp. Two scores
within that noise can tie-break differently on different replicas. This
matters for the later score-keyed cursor in #7746: a cursor issued by an arm64
replica and resumed on an amd64 replica must not compare exact score bits. It
needs either an architecture-independent score (for example explicit
`float64(...)` conversions, which the Go spec says prevent fusion, at every
product in the BM25, cosine, and hash-embedding paths) or a key that does not
rely on score bits.

## Golden pin provenance

- The transcript renders each score with `%.8g` (`searchGoldenScoreDigits`).
  Everything else is unchanged.
- The digest `b3fa9fa441889008d0e3e8ac6650ea620ea00ae9a6b8f3ea12fe364d0288d413`
  came from the new test file run against the pre-refactor `Search`. That run
  used a throwaway worktree detached at the branch commit
  `fix(searchhybrid): sum BM25 contributions in sorted term order`, whose
  `backend.go` has no `RankAll`, on arm64. The same digest results at the head
  of the branch. The baseline is that commit, not `origin/main`, because the
  sorted-order fix itself changes BM25 scores relative to main.
- Cross-architecture proof without amd64 hardware: the raw-bits amd64 CI
  transcript (digest `5a2d53df…`, matching the CI failure line) and the raw-bits
  arm64 transcript (digest `bff1157a…`), each re-rendered with `%.8g`, both
  hash to `b3fa9fa4…`.
- Margin: shifting any of the 307 nonzero scores (334 score lines) by up to
  262144 ulp leaves its `%.8g` rendering unchanged, so the observed 4 ulp is
  far from a rounding boundary.
- Not a tautology (seeded mutations, each restored):
  - `limit+1` -> `limit+2` in `Search` gives
    `Search transcript digest = 7c0b96fe…, want b3fa9fa4…`.
  - `defaultK1` 1.2 -> 1.2000001 gives digest `823f33ea…`, and 1.2 ->
    1.20000001 gives digest `a24a125c…`. Both fail.
  - 1.2 -> 1.200000001, a relative change of about 1e-9, passes. The pin
    resolves score changes of about 1e-8 relative or larger.

## Performance Evidence

Host: shared 16-core arm64, Go 1.26.9, load average 6 to 23 during the runs.
Wall-clock numbers are not gating. Bytes and allocations are deterministic and
are the comparison. Each line is 5 runs (`-count=5 -benchmem -run '^$'`).

`BenchmarkCodeHybridRerank` (`go/internal/query/codemodel`) runs
`CodeHybridRanker.Rerank` end to end over N projectable rows with about 3 KB
`source_cache` bodies. That covers projection, index build with hash embedding,
ranking, and reorder.

| Rows | Branch state (commits applied) | ns/op (non-gating) | B/op | allocs/op | applied |
|---|---|---|---|---|---|
| 100 | none, `88beb12d0` on main | 9819476 to 10089606 | 11107153 to 11107287 | 6005 to 6006 (ledger:7746-code-rerank-100-main-allocs) | 1 |
| 100 | 1 (BM25 order) | 9660761 to 9712469 | 11107175 to 11107184 | 6006 (ledger:7746-code-rerank-100-bm25-allocs) | 1 |
| 200 | 1 (cap 100) | 11050028 to 11128917 | 11439134 to 11439157 (ledger:7746-code-rerank-200-before-bytes) | 7153 (ledger:7746-code-rerank-200-before-allocs) | 0 |
| 100 | 1-2 (RankAll) | 9879470 to 11106963 | 11107211 to 11107411 | 6006 to 6008 | 1 |
| 200 | 1-2 (RankAll) | 20664122 to 21345694 | 22176291 to 22176468 (ledger:7746-code-rerank-200-after-bytes) | 11586 to 11587 (ledger:7746-code-rerank-200-after-allocs) | 1 |
| 200 | 1-8 (stage span) | 20163058 to 22256571 | 22176643 to 22176827 (ledger:7746-code-rerank-200-span-bytes) | 11592 to 11593 (ledger:7746-code-rerank-200-span-allocs) | 1 |

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
- The stage span adds 5 allocations and about 413 B per re-rank with the
  default no-op tracer, measured with commits 1-8 applied.

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

## Observability Evidence

Observability Evidence: each re-rank pass that reaches the ranking work emits
a `query.hybrid_rerank` span, a child of the request span, through
`tracing.StartHybridRerankSpan` (commit 8). Its attributes are
`search.rerank.tool` (`find_code`, `search_entity_content`, or
`search_file_content`), `search.rerank.rows` (the page row count, at most 200),
and `search.rerank.applied` (whether the page was reordered). The span's
duration is the stage's CPU cost inside a slow request, and `applied=false`
with a large `rows` value is the old lexical fallback. No span is emitted when
the ranker is disabled or has fewer than two rows to rank. The name is
registered in `go/internal/telemetry` (`TestSpanNames`) and documented in
`docs/public/reference/telemetry/traces.md` and the telemetry-coverage per-route
span row. Tests: `TestStartHybridRerankSpanEmitsTheStageContract`,
`TestCodeHybridRerankEmitsStageSpan`, `TestCodeHybridRerankDisabledEmitsNoSpan`,
and `TestContentHybridRerankEmitsStageSpan` assert the name, parent, and
attributes for applied and fallback passes. The response signals remain: the
per-row `search_backend=hybrid` marker, `page_position`, and for `find_code`
`source_backend=hybrid_content_store`. The BM25 order fix changes no runtime
shape.

## Commands

From `go/` with `GOCACHE=/tmp/g7746cache GOTMPDIR=/tmp/g7746`:

```bash
go test ./internal/searchhybrid/ -run TestBM25ScoreBytesStableAcrossTermOrder -count=20
go test ./internal/searchhybrid/... -count=1
go test ./internal/query/codemodel/ -run TestCodeHybridRerankRanksAllRowsOfAMaximumPage -count=3 -v
go test ./internal/query/contentread/ -run TestContentHybridRerankRanksAllRowsOfAMaximumPage -count=3 -v
go test ./internal/query/codemodel/ -run '^$' -bench BenchmarkCodeHybridRerank -benchmem -count=5
go test ./internal/searchhybrid/ -run '^$' -bench BenchmarkRankAll -benchmem -count=5
go test ./internal/query/tracing/ ./internal/query/codemodel/ ./internal/query/contentread/ -run 'HybridRerank' -count=1 -v
```
