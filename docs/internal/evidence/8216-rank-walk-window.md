# #8216: cache the hybrid rank_cursor window score across pages

Child of #8017. Branch `drive/8216-rank-walk-window`, base `origin/main` at
`abb5cae71` (plus prerequisite commit `823ccd540`, which qualifies two
`Result{}` leftovers from the #6626 alias retirement and changes no
behavior).

## Problem

The stateless `rank_cursor` walk (#7746, `rank-walk.md`) re-reads the first
200 lexical rows on every page and re-scores all of them. A walk at page size
L pays about 200/L full-window scoring passes. The scorer reads the full
stored body (the `source_cache` read clip runs after the re-rank in
`CodeSearchPagePayloadFromPage`), so a cold page cost grows with body size.

## Decision: cache the window score, not clip before scoring

Two options from the issue: clip each body before scoring, or cache the
window score across pages with a staleness rule.

A same-shape probe with 60,000-byte bodies (temporary
`BenchmarkFindCodeRankWalkProbe60k`, removed before commit) showed the
scoring input does NOT plateau past the 4096-byte `boundedContext`
projection bound: `ScoreCodeWindow` alone measured 21 ms at 3 KB bodies and
89 ms at 60 KB, because the per-row projection lowercases and scans the full
body for the sensitive-context exclusion before the bound applies. Clipping
to 4096 before scoring would therefore skip that exclusion on bodies past
the clip and let secret-bearing rows into the searchable index: a security
behavior change. Clipping below 4096 would change scores and the hybrid
order for every row: a relevance change. Both were rejected.

The chosen change caches the whole-window score in the ranker instance with
a key-exact staleness rule: the key covers the ranker identity, the
repository, the query, and every scoring input byte of every window row in
walk order (`codemodel.KeyWindowScores`). Any edit, scope, query, ranker, or
reorder change misses and re-scores. `rank.Walk` still checks the live
window basis and order on every page, so exactly-once rows and the 409
`basis_changed`/`order_changed` behavior are unchanged by construction: a
hit returns bit-identical scores. Eviction past the 64-entry LRU bound is
the only invalidation and only costs a rescore, so there is no TTL. The
cache is per ranker instance (per process), so cross-architecture score
variance can never mix. Both lanes share it: find_code through
`CodeHybridRanker.ScoreCodeWindow`, `search_entity_content` through
`ContentHybridRanker.ScoreEntityWindow`.

## Performance Evidence: one scoring pass per walk instead of one per page

`BenchmarkFindCodeRankWalkPage` before (count=3, commit `823ccd540`) and
after (count=5, commit `05aea699a`), linux/arm64, go1.26.9, shared host (wall
times non-gating; byte and alloc figures are the load-independent change).
Ledger rows: `docs/internal/measurements.jsonl`, ids `8216-rankwalk-*`.
The before run reproduces the #7746 baseline shape on this host
(`7746-rankcursor-page-200-ns`, 18.8 ms on its host, 26.2 ms here).

| Page (200-row window) | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs | After allocs |
| --- | --- | --- | --- | --- | --- | --- |
| `page`, about 3 KB bodies | 26191365 | 4703705 | 23662478 | 3137551 | 14462 | 3839 |
| `page_body4090`, 4,090-byte bodies | 29629060 | 5301551 | 31555578 | 2984050 | 14791 | 3638 |

After figures are medians of 5; the reported `page` average is dominated by
warm iterations (the first iteration scores cold, the rest hit the cache),
which is exactly the repeat-page cost the issue asks to bound.

A new `BenchmarkFindCodeRankWalkSequence` walks all four pages at limit=50
with cursor chaining on a fresh ranker (page 1 cold, pages 2-4 cached):
median 35199902 ns/op and 33828102 B/op over 3 trials, against about
104.8 ms derived for the same walk before (4 x 26191365 ns/op: every page
did identical scoring work, so the product is the walk cost with the same
start and end events).

## Observability Evidence: a served page without the rerank span is a cache hit

No new metric and no span change (telemetry-coverage verifier passes
unchanged). A scoring pass emits the existing `query.hybrid_rerank` span
with `search.rerank.rows` equal to the window size; a repeat window returns
before the span opens, so a served hybrid page with no rerank span was
served from the cached score. `eshu_dp_query_rank_cursor_total` and the
request-span `search.rank_cursor.*` attributes are recorded per request as
before, on hits and misses alike. In-process hit/miss counts are exposed
for tests via `WindowScoreCache.Stats`, not as an operator metric.

## Commands

From `go/` with `TMPDIR` and `GOTMPDIR` pointed at a roomy directory (the
default `/tmp` filled up under parallel agents and failed the link):

```bash
go test ./internal/query/codemodel/ ./internal/query/contentread/ ./internal/query/search/rank/ ./internal/query/codequery/ -count=1
go test -race ./internal/query/codemodel/ -run 'WindowScore|ScoreCodeWindow' -count=1
go test -race ./internal/query/contentread/ -run 'ScoreEntityWindow|RankWalk' -count=1
go test ./internal/query/codequery/ -run '^$' -bench 'BenchmarkFindCodeRankWalkPage' -benchmem -count=5
go test ./internal/query/codequery/ -run '^$' -bench 'BenchmarkFindCodeRankWalkSequence' -benchmem -count=3
ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh
```

Walk contracts green: the `rank` package suite, `codequery`
`RankWalk|OffsetPathBytes|LaneFlip` (44 pass events), `contentread`
`RankWalk|RankCursor`, and the full `codemodel`, `contentread`, and
`codequery` packages, plus the new cache unit tests (hit, miss on every
input class, bound, clone isolation, concurrent use with `-race`) and the
per-lane no-rescore tests (`TestScoreCodeWindowServesRepeatWindowFromCache`,
`TestScoreEntityWindowServesRepeatWindowFromCache`: identical scores across
pages, exactly one rerank span for a repeat window, a second span after a
body edit).

## Not checked

- Deployed latency and the Postgres cost of the 201-row reads on a real
  corpus: the live gate (`verify-golden-corpus-gate`, port 15432) was held
  by another session for the whole task, so no live or Docker measurement
  was possible. The scaled-input dimension (60 KB bodies, 15x the read
  clip) was measured locally instead.
- `EXPLAIN` of the walk reads for the same reason. The read shape is
  unchanged by this diff (same 201-row lane probes per page).
