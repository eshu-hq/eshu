# #7746 PR-B: stateless cross-page relevance walk (`rank_cursor`)

This note records the theory proof, the design, and the verification for the
second #7746 change. The first change (PR #8183, merged as `8f188353f`) made
BM25 score bits stable and added `searchhybrid.Backend.RankAll`; see
[7746-ranker-determinism.md](7746-ranker-determinism.md).

Branch `feat/7746-rank-cursor`, base `8f188353f`. Commits are named by
subject line, because a rebase rewrites SHAs.

## Problem

`find_code` and `search_entity_content` page by `offset` in lexical order. The
hybrid ranker reorders rows inside one page only, so the most relevant rows of
a result can sit on its last page. A cursor over a re-rank window that changes
per page loses or repeats rows (#7725 review).

## Design

Option A from the arbiter ruling: a stateless opt-in `rank_cursor` over a
fixed 200-row lexical window, scored per request.

- `"start"` opens a walk. The handler reads the first 200 lexical rows of one
  repository (`find_code`: its content lane, never the graph lane), scores all
  of them with `RankAll`, and `rank.Walk` returns the next `limit` rows.
- Order: fused score rounded to 8 significant digits (descending), then
  `entity_id` (ascending); unscored rows last, by `entity_id`.
- The cursor (`rc1.` + base64url of 44 bytes) holds the served count, a
  served-set digest (two 64-bit lanes of per-id SHA-256, added modulo 2^64),
  a 16-byte window fingerprint, the tool, and an 8-byte checksum.
- Each page checks the fingerprint (else 409 `candidate_set_changed`) and that
  the digest of the top `served` rows of this replica's order equals the
  cursor digest (else 409 `rank_order_changed`). With both checks passing, a
  page can neither repeat nor skip a row. Exactly-once does not rest on score
  bits.
- The digest is additive, so the MCP response budget can cut a page to a
  prefix and mint the cursor for it (`rank.Advance`, `rank.TrimWalk`) without
  the window.
- Past the window the walk ends (`window_end=true`); `lexical_truncated=true`
  reports more lexical matches. There is no fallback to offset rows.

Deviations from the claim comment, each forced by a repository gate:

- `querycontract` holds exactly 40 non-test files (the dirgate cap), so the
  cursor lives in a new leaf package `go/internal/query/search/rank`, not in
  `querycontract/rank_cursor.go`.
- `internal/mcp` is pinned at 89 files, so the budget trim is a branch in
  `dispatch_budget.go` that calls `rank.TrimWalk`, not a new file.
- `codemodel/code_hybrid_rerank.go` gains `CodeWindowScorer`, because the
  find_code projection is unexported there.

## Theory proof (before implementation)

Decision: is scoring the whole 200-row window on every page affordable?
Threshold: at most 25 ms CPU and 25 MB allocation per page at 200 rows on
this host, which is no worse than one `limit=200` ranked page that already
ships.

| Benchmark | Rows | ns/op (median of 5) | Ledger |
| --- | --- | --- | --- |
| `BenchmarkCodeHybridRerank` | 200 | 19334629 | ledger:7746-rankcursor-window-score-200-ns |
| `BenchmarkCodeHybridRerank` | 100 | 9671891 | ledger:7746-rankcursor-window-score-100-ns |
| `BenchmarkRankAll` | 200 | 536699 | ledger:7746-rankcursor-rankall-200-ns |

Allocation at 200 rows: 22176553 B/op (ledger:7746-rankcursor-window-score-200-bytes).
Result: within the threshold, so the design went ahead. `RankAll` is about
2.8% of the cost; `searchhybrid.NewIndex` (double tokenization) dominates, as
the PR-A evidence found. The host load average was 11 to 19 on 16 cores during
the run, so the figures are non-gating. The Postgres read of a 201-row window
was not measured (no database here); it is the same probe as a `limit=200`
request.

Performance Evidence: a walk page costs one 200-row window score (about 19 ms
CPU and 22 MB transient allocation on linux/arm64, ledger rows above) plus one
201-row lexical probe. It adds no Cypher, graph write, SQL shape, queue,
lease, lock, or goroutine. A request without `rank_cursor` runs the unchanged
offset path; byte goldens captured on main pin both routes.

## Cross-architecture tolerance

`TestWalkAcrossArchitecturesNeverSkipsOrRepeats` scores each page on a
simulated replica: every score nudged by up to 4 ulp (the measured arm64 vs
amd64 FMA difference), with random row swaps on some seeds. Over 300 fixed
seeds, 257 walks served every row exactly once
(ledger:7746-rankcursor-arch-walks-completed) and 43 stopped with a typed 409
(ledger:7746-rankcursor-arch-walks-conflict). None repeated or skipped a row.

`TestWalkUlpNoiseAloneDoesNotChangeTheOrder` uses exact Reciprocal Rank Fusion
ties (rows with swapped rank pairs). With 8-digit quantization, ulp noise
leaves the order unchanged. With quantization removed (a mutation that was not
committed), the test failed: ulp noise broke the ties.

## Guarantees

- Guaranteed: every walkable window row is served exactly once, or the walk
  stops with a 409. On the replica that serves page k, every row of page k
  ranks after every row served before it.
- Not guaranteed: the same order on every replica. A difference that crosses
  the served boundary is a 409; one inside the served or the unserved rows is
  not visible and does no harm.

## Observability Evidence

Observability Evidence: each walk request records
`eshu_dp_query_rank_cursor_total{tool,outcome}` (closed sets; outcome is
`served`, `window_end`, `basis_mismatch`, `invalid`, or `unsupported`) and the
request-span attributes `search.rank_cursor.outcome`,
`search.rank_cursor.served`, and on a 400 or 409 `search.rank_cursor.reason`.
The scoring pass emits `query.hybrid_rerank` with `search.rerank.rows` equal
to the window size. No signal carries query text, a repository ID, or a cursor.
Tests: `TestRecordOutcomeCountsAndTagsTheRequestSpan`,
`TestRecordErrorMapsEveryCodeToItsOutcome`, `TestOutcomeLabelsAreClosed`.

## Commands

From `go/` with an isolated `GOCACHE`:

```bash
go test ./internal/query/search/rank/ -count=1 -race
go test ./internal/query/search/rank/ -run TestWalkAcrossArchitecturesNeverSkipsOrRepeats -count=1 -v
go test ./internal/query/contentread/ ./internal/query/codequery/ -run 'RankWalk|OffsetPathBytes' -count=1 -race
go test ./internal/mcp/ -run 'RankWalk|RankCursor' -count=1
go test ./internal/query/ -run TestOpenAPIDocumentsTheRankCursorWalk -count=1
go test ./internal/query/codemodel/ -run '^$' -bench BenchmarkCodeHybridRerank -benchmem -count=5
go test ./internal/searchhybrid/ -run '^$' -bench BenchmarkRankAll -benchmem -count=5
```

## Not checked

- Deployed latency, and the Postgres cost of the 201-row probe on a real
  corpus.
- A walk across real arm64 and amd64 replicas. The tolerance is proven with
  simulated ulp noise only.
