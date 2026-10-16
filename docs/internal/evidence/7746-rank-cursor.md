# #7746 PR-B: stateless cross-page relevance walk (`rank_cursor`)

This note records the theory proof, the design, and the verification for the
second #7746 change. The first change (PR #8183, merged as `8f188353f`) made
BM25 score bits stable and added `searchhybrid.Backend.RankAll`; see
[7746-ranker-determinism.md](7746-ranker-determinism.md).

Branch `feat/7746-rank-cursor`, branched from `8f188353f` and rebased onto
`711ed3122`. Commits are named by subject line, because a rebase rewrites
SHAs; a ledger row cites a commit that is on the branch.

## Problem

`find_code` and `search_entity_content` page by `offset` in lexical order. The
hybrid ranker reorders rows inside one page only, so the most relevant rows of
a result can sit on its last page. A cursor over a re-rank window that changes
per page loses or repeats rows (#7725 review).

## Design

Option A from the arbiter ruling: a stateless opt-in `rank_cursor` over a
fixed 200-row window, read and ordered per request. The fork-6 ruling
(option B) set the lane rule.

- `"start"` opens a walk. `find_code` picks the lane on page 1 exactly as an
  offset request does (`selectCodeSearchLane`: the graph lane first, the
  content lane when the graph has no rows or no graph is configured).
  `search_entity_content` always uses the content lane. The handler reads the
  first 200 rows of that lane and `rank.Walk` returns the next `limit` rows.
- Orders: `graph_name` (the graph lane's own order, so its pages equal the
  offset pages), `hybrid` (fused score rounded to 8 significant digits,
  descending, then `entity_id`; unscored rows last), and `lexical` (the
  content lane's own order with `exact=true`, no ranker, or no scorable row).
- The cursor (`rc1.` + base64url of 45 bytes) holds the version, the tool,
  the lane, the served count, a 16-byte basis fingerprint, a served-set
  digest (two 64-bit lanes from disjoint byte ranges of per-id SHA-256, added
  modulo 2^64), and an 8-byte checksum. Advance refuses a repeated id.
- Each page checks the lane and the basis fingerprint (tool, lane, order,
  ranker identity, scope, every row id and content), else 409
  `basis_changed`, and that the digest of the top `position` rows of this
  replica's order equals the cursor digest, else 409 `order_changed`. With
  both checks passing, a page can neither repeat nor skip a row. Exactly-once
  does not rest on score bits.
- The digest is additive, so the MCP response budget can cut a page to a
  prefix and mint the cursor for it (`rank.Advance`, `rank.TrimWalk`) without
  the window.
- Past the window the walk ends (`window_end=true`); `lexical_truncated=true`
  reports more matches. There is no fallback to offset rows.

Deviations from the claim comment, each forced by a repository gate:

- `querycontract` holds exactly 40 non-test files (the dirgate cap), so the
  cursor lives in a new leaf package `go/internal/query/search/rank`, not in
  `querycontract/rank_cursor.go`.
- `internal/mcp` is pinned at 89 files, so the budget trim is a branch in
  `dispatch_budget.go` that calls `rank.TrimWalk`, not a new file.
- `codemodel/code_hybrid_rerank.go` gains `CodeWindowScorer`, because the
  find_code projection is unexported there.
- The find_code walk and the lane seam live in a new
  `codequery/rank_walk.go` (the directory is exempt from the dirgate cap by
  its `doc.go` nolint). An earlier version inlined the walk in `handler.go`
  and pushed it to 498 lines; `handler.go` is 410 lines with the dispatch
  only.

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

Performance Evidence: the scoring theory above covers only the score. The
whole hybrid page, measured with `BenchmarkFindCodeRankWalkPage/page`
(`ScoreCodeWindow`, `MapRows`, `Walk`, and `MapPage` over 200 rows of about
3 KB), costs 18801708 ns/op (ledger:7746-rankcursor-page-200-ns) and
23662388 B/op with 14462 allocs/op (ledger:7746-rankcursor-page-200-bytes).
That is inside the 25 ms and 25 MB threshold. The walk work alone
(`walk_only`) is 901265 ns/op (ledger:7746-rankcursor-walk-only-200-ns) and
2477631 B/op with 3427 allocs/op (ledger:7746-rankcursor-walk-only-200-bytes),
mostly the per-row content strings and the basis hash inputs. A `graph_name`
or `lexical` page scores nothing and pays only the walk work.

Store reads per walk page match the offset page of the same lane at a
201-row probe: `find_code` reads the graph lane (one graph query), and when it
is empty the content lane (a name search and a source search, each with a
201-row limit); `search_entity_content` reads one 201-row lexical probe. The
walk adds no Cypher shape, graph write, SQL shape, queue, lease, lock, or
goroutine. A request without `rank_cursor` runs the unchanged offset path;
byte goldens captured on main pin both routes.

## Cross-architecture tolerance

`TestWalkAcrossArchitecturesNeverSkipsOrRepeats` scores each page on a
simulated replica: every score nudged by up to 4 ulp (the measured arm64 vs
amd64 FMA difference), with random row swaps on some seeds. Over 300 fixed
seeds, 257 walks served every row exactly once
(ledger:7746-rankcursor-arch-walks-completed) and 43 stopped with a typed 409
(ledger:7746-rankcursor-arch-walks-conflict) with `order_changed`. None
repeated or skipped a row.

`TestWalkUlpNoiseAloneDoesNotChangeTheOrder` uses exact Reciprocal Rank Fusion
ties (rows with swapped rank pairs). With 8-digit quantization, ulp noise
leaves the order unchanged. With quantization removed (a mutation that was not
committed), the test failed: ulp noise broke the ties.

## Guarantees

- Guaranteed: every walkable window row is served exactly once, or the walk
  stops with a 409. On the replica that serves page k, every row of page k
  ranks after every row served before it.
- Not guaranteed: the same `hybrid` order on every replica. Score bits are
  stable per CPU architecture only, so replicas of mixed architectures can
  answer `order_changed` on every page. A difference inside the served or the
  unserved rows is not visible and does no harm. `graph_name` and `lexical`
  walks read no scores.

## Observability Evidence

Observability Evidence: each walk request records
`eshu_dp_query_rank_cursor_total{tool,outcome,reason}` (closed sets; outcome
is `served`, `window_end`, `basis_mismatch`, `invalid`, or `unsupported`;
reason is `none`, `basis_changed`, `order_changed`, or a 400 reason) and the
request-span attributes `search.rank_cursor.outcome`, `.reason`, `.position`,
and on a served page `.lane` and `.order`.
The scoring pass emits `query.hybrid_rerank` with `search.rerank.rows` equal
to the window size. No signal carries query text, a repository ID, or a cursor.
Tests: `TestRecordPageCountsAndTagsTheRequestSpan`,
`TestRecordErrorMapsEveryCodeToItsOutcome`, `TestOutcomeLabelsAreClosed`.

## Commands

From `go/` with an isolated `GOCACHE`:

```bash
go test ./internal/query/search/rank/ -count=1 -race
go test ./internal/query/search/rank/ -run TestWalkAcrossArchitecturesNeverSkipsOrRepeats -count=1 -v
go test ./internal/query/contentread/ ./internal/query/codequery/ -run 'RankWalk|OffsetPathBytes|LaneFlip' -count=1 -race
go test ./internal/mcp/ -run 'RankWalk|RankCursor' -count=1
go test ./internal/query/ -run TestOpenAPIDocumentsTheRankCursorWalk -count=1
go test ./internal/query/codemodel/ -run '^$' -bench BenchmarkCodeHybridRerank -benchmem -count=5
go test ./internal/searchhybrid/ -run '^$' -bench BenchmarkRankAll -benchmem -count=5
```

## Not checked

- Deployed latency, and the store cost of the 201-row reads on a real
  corpus.
- A walk across real arm64 and amd64 replicas. The tolerance is proven with
  simulated ulp noise only.
