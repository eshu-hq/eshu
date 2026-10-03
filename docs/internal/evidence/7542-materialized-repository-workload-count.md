# #7542 repository context workload count

`GET /api/v0/repositories/{repo_id}/context` reports `workload_count` from
materialized graph truth: distinct `Workload` nodes connected through a
`Repository` `DEFINES` edge. Repository read-model `WorkloadNames` remain
identity and display hints for story and service fallback. They are not a count
of materialized workloads.

## Theory and performance evidence

A read-only Neo4j PROFILE of the existing count query, anchored by the unique
`Repository.id` index, returned zero materialized workloads on each of two
representative repositories with 12,403 and 7,097 files. Server query time was
5 ms and 0 ms respectively, with 6 and 7 DB hits and about 120 B of result
payload. This is server-only query timing, not API or MCP latency. The
previous read-model path avoided this graph query when a summary was available;
this change adds one indexed, scalar count read to that path. The count query
text and index are unchanged. Deployed endpoint p95 remains NOT_CHECKED.

## No-Regression Evidence:

The focused regression first failed with a retained workload name and graph
count zero: context reported one. It also failed when the graph count was two
but the summary had one name, and when a graph deadline was swallowed by the
summary. The same tests pass after context always uses the graph count. The
existing file, platform, and dependency summary behavior remains covered by
the repository context handler regression.

## No-Observability-Change:

The existing `summary_counts` repository query stage logs the four count
fields and graph read errors. The graph read uses the existing query adapter,
spans, and duration metrics. No new metric, span, status, or runtime setting is
introduced.
