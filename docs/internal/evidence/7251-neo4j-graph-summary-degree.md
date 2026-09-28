# #7251 Neo4j graph-summary degree page

The repository-scoped graph-summary packet previously fetched up to 50,001
physical `CALLS` edges and ranked the hot Functions in Go. Neo4j now groups and
ranks that bounded edge set before returning at most `limit + 1` Functions.
NornicDB retains the original raw-edge read and Go ranking because its
aggregate and ordering behavior needs separate live proof.

## Theory and measured query cost

Performance Evidence: On the same archived ops-qa Neo4j repository with
45,495 Functions and 39,649 physical `CALLS` edges, the raw edge read took
632/680 ms and 878,988 DbHits; the guarded aggregate took 458/377 ms and
550,060 DbHits in alternating read-only `PROFILE` samples. This is query-level
evidence only; built API/MCP cold and warm p95 is **NOT_CHECKED**.

The read-only ops-qa Neo4j proof used the largest archived argument set: a
repository with 12,403 files, 45,495 Functions, and 39,649 physical `CALLS`
edges. All 45,495 Function UIDs were present and distinct. The raw result had
39,649 distinct UID pairs. Go's ranking of those rows and the exact production
Neo4j query agreed on all 101 ordered Function keys, IDs, paths, names, and
incoming/outgoing degrees. The production query returned `raw_edges=39649`
and `invalid_uid_edges=0`.

Alternating read-only `PROFILE` samples on the same repository measured raw
edge query server times of 632 and 680 ms, with 878,988 DbHits in each run.
The candidate with the raw-edge count and missing-UID guard took 458 and
377 ms, with 550,060 DbHits in each run. The exact production query took
462 ms in a separate sample. These samples support a smaller graph read and
response, but are not built API/MCP latency or a cold/warm p95 claim. The
remote `cypher-shell` client wall time includes several seconds of Kubernetes
exec and process startup and is not an API latency comparison. Ops-qa received
no graph write, DDL, settings change, deploy, or `ANALYZE`.

## Local Go hub-ranking benchmark

Performance Evidence: The retained Go hub-ranking path was measured with the
same benchmark fixture on `origin/main` `62176d64f6` and this branch. The
fixture has 12,403 distinct Function keys, 39,649 edge rows, and a 25-item
page; this is synthetic and is not an ops-qa endpoint measurement. On an
Apple M5 Max with `GOMAXPROCS=2` and `GOFLAGS=-p=2`, six warmed samples per
revision in base/current/current/base order (`-benchtime=3x -count=3`) gave
49.58 ms/op median for the old row-before-sort path (44.74-57.38 ms) and
24.25 ms/op median for typed rank-before-row materialization (22.20-26.66
ms), a 51.1% lower median. Allocated bytes went from 25,188,464 to
13,309,272 B/op, and allocations from 161,058 to 617 allocs/op. The
benchmark test file was copied unchanged into a disposable baseline worktree;
only the benchmark ran there, with the baseline production source unchanged.
This measures the Go ranking function, not graph read, network transfer, or
API/MCP cold and warm p95.

## Correctness and local proof

The query counts the 50,001st raw edge before pair deduplication. The handler
keeps the existing 50,000-edge fail-closed result, the `limit + 1` truncation
marker, and the repository grant check before the graph read. An absent or
empty Function UID triggers the original Go ranking path, which falls back to
the Function ID. An empty graph returns the count row and an empty hot list.
The query coalesces absent paths and names to empty strings to match Go's tie
order.

New Go tests were red before the Neo4j branch for routing, raw-edge overflow,
and missing-UID fallback, then green after it. A live test on disposable
`neo4j:2026-community` was red when a null path sorted after a non-null path;
after the coalesce fix, its optimized result equaled the unchanged raw-edge
Go result. A separate local fixture had four physical `CALLS` edges but three
distinct pairs, with the expected total degrees 4 and 2. It also returned an
empty-graph count row and flagged an edge whose source UID was absent. The
disposable containers were stopped and removed.

Final local checks after the null-order edit:

```text
go test ./internal/query -count=1                                      PASS
go test -tags live_infra_scope_neo4j ./internal/query -run '^TestLiveGraphSummaryNeo4jDegreePage$' -count=1  PASS
go test ./internal/queryplan -count=1                                  PASS
bash scripts/verify-query-plan-regression.sh                           PASS (24 indexes; live profiles ran)
ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh  PASS
gofumpt -l changed Go files                                           empty
git diff --check                                                        PASS
```

The live test requires a disposable Neo4j and the explicit
`ESHU_INFRA_SCOPE_NEO4J_LIVE=1` and
`ESHU_GRAPH_SUMMARY_NEO4J_FIXTURE=1` guards. The production read uses the
existing graph query span, raw-edge count, and overflow attributes, with a
Neo4j aggregation marker and missing-UID fallback marker. The response shape,
truth envelope, and NornicDB query text are unchanged.

Observability Evidence: The production Neo4j handler adds
`eshu.query.graph_summary.neo4j_degree_aggregate` and
`eshu.query.graph_summary.missing_uid_fallback` to the existing graph query
span. The bounded raw-edge count and overflow attributes remain available for
diagnosis; no deployed span sample has been checked yet.

## Remaining acceptance proof

Built endpoint cold and warm p95 on the same archived arguments is
**NOT_CHECKED**. The owner has not deployed this branch. Do not claim the
issue's under-one-second target or close #7251 from query PROFILE and fixture
proof alone. A same-argument API/MCP sweep after the owner's deploy is required.
