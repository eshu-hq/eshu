# Source cache read-time clip (#7171)

Child C of #7129. `find_symbol`, `inspect_code_inventory`, `find_code`, and
`search_entity_content` clip each row's `source_cache` to 4,096 bytes at read
time and report the clip; the two MCP defaults that returned that field at 25
rows drop to 20. The stored body is unchanged. The contract is the public
[Source Cache Read Clip](../../public/reference/http-api/source-cache-clip.md)
page.

Performance Evidence: the change is response shaping on hot read paths, so the
claim measured is dispatch response size at default arguments, using the
dispatcher's own `estimateResponseBytes` (both wire copies counted) against the
256 KiB budget. The fixtures are real handlers (`CodeHandler`, `ContentHandler`)
over a fake store, with the measured production row shapes: 40 rows in the
store, three of them 61,625 B, the rest 12 KiB, each with about 1.5 KB of
non-source row overhead. A default-args reply is 20 rows for `find_symbol` and
`inspect_code_inventory` and 10 for `search_entity_content`.

| Tool (default args) | Clip disabled | Clip, default 25 | Clip, default 20 (shipped) | Budget |
| --- | --- | --- | --- | --- |
| `find_symbol` | 845,050 B | 281,110 B | 225,150 B | 262,144 B |
| `inspect_code_inventory` | 851,490 B | 289,170 B | 231,590 B | 262,144 B |
| `search_entity_content` | 552,352 B | n/a (default 10) | 94,392 B | 262,144 B |

The fixture rows carry 100-character docstrings, which the row shape echoes about
five times, for roughly 1.5 KB of non-source overhead per row. The bound these
figures support is therefore: a clipped `source_cache` plus about 1.5 KB of other
row content fits at the shipped defaults. It is not a bound on every row.

The clip alone does not fit `find_symbol` or `inspect_code_inventory` at the old
default of 25; both the clip and the lower default are needed, and the shipped
rows are 86% and 88% of the budget. Each figure is one deterministic run, not a
sample: the fixture has no timing, ordering, or cache dependence. The clip
disabled and default-25 columns are the seeded mutations recorded below, run on
the same fixture as the shipped column.

CPU cost of the clip: `BenchmarkClipRowsSourceCache` (20 rows of 61,625 B
two-byte-rune bodies, including building the 20 row maps) took 91 us per page
and 160 allocations on the local development laptop
(`go test ./internal/query/querycontract -bench BenchmarkClipRowsSourceCache -benchtime=20000x`).
The cut scans at most 4,096 bytes per row regardless of stored length. No new
graph, Postgres, queue, or lease work is added: the clip runs on rows already
read, after the page is trimmed and after the hybrid re-rank.

No-Regression Evidence: no graph or Postgres statement, index, lock, or queue
path is touched, so there is no query plan to compare. The rows the store
returns are identical; only the response body shrinks. Ranking is unchanged
because the hybrid re-rank reads the full stored body and the clip runs
afterwards (`TestSearchEntityContentClipsAfterHybridRerank`; clipping before the
re-rank fails it). `get_entity_content` and `get_file_lines`, the drill-down
for a clipped row, are not clipped. The golden snapshot
`testdata/golden/e2e-20repo-snapshot.json` loses no required field: the clip
markers are optional additions, verified by `go test ./cmd/golden-corpus-gate`.

Observability Evidence: operators see the clip in the response itself.
Every response of the four routes carries `source_cache_clip_bytes` and
`source_cache_clipped_rows`, and every clipped row carries `source_cache_clipped`,
`source_cache_clip_bytes`, and `source_cache_total_bytes`. The existing
`eshu_dp_mcp_response_bytes` histogram and `eshu_dp_mcp_response_over_budget_total`
counter (see `docs/public/reference/telemetry/mcp-response-budget.md`) record the
per-tool reply size, so a tool drifting back toward the budget shows on the
histogram before any caller sees `mcp_response_over_budget`. No metric, span, or
log key is added.

## Residual: row metadata is not clipped

Only `source_cache` is clipped. `metadata` is returned whole, and a long
`docstring` is echoed by the row's semantic blocks about five times. At 20 rows
the two-copy wire size exceeds the 262,144 B budget once the average non-source
row content passes about 2.9 KB, so a default-args reply over such rows fails
closed with `mcp_response_over_budget`; its guidance names `get_entity_content`
for a clipped row. Bounding row metadata is filed as
[#7234](https://github.com/eshu-hq/eshu/issues/7234). This PR does not claim a
constructive bound on every row.

## Seeded mutations

Each was applied to a copy, run, and restored; the shipped tree passes all of them.

- Clip disabled (`BoundedSourceCache` never clips): the three budget fixtures
  fail over budget (sizes in the table), the sparse-marker test fails for all
  four tools, and the rerank test fails.
- Clip without row markers: the marker unit tests and the four-tool marker test
  fail (`source_cache_total_bytes` absent).
- A wrong UTF-8 cut (`source[:limit]`): `TestBoundedSourceCacheNeverSplitsACodePoint`
  and the four-tool marker test fail on the multi-byte row.
- Clip before the re-rank: `TestSearchEntityContentClipsAfterHybridRerank`
  fails.
- Default still 25 in the route: both budget fixtures fail (281,110 and 289,170
  bytes) and the route-default test fails.
- Schema default still 25: `TestAdvertisedLimitDefaultMatchesRouteDefault` fails.
