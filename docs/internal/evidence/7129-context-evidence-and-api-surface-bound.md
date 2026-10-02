# #7129 workload and service context evidence and API surface bound

## Problem

After #7169 capped hostnames and entrypoints, `get_workload_context` and
`get_service_context` were still over the 256 KiB MCP response budget on
populated ops-qa services. The last ops-qa re-measure (issue comment of
2026-09-27, image `sha-1de13f4`) put two workloads with 72 and 78 endpoint
edges at 344,412 and 287,308 bytes (est2x, both wire copies counted).

Two payload families carried the bytes:

- `deployment_overview.api_surface.endpoints`, a second copy of the rows
  already shipped at the top-level `api_surface.endpoints`.
- `deployment_evidence.artifacts`. The graph read allows 50 outgoing plus 50
  incoming artifact rows, so the list holds up to 100 rows of about 1.4 KB
  each. `delivery_paths`, `delivery_workflows` and `shared_config_paths`
  (content-derived) had no cap on the context routes at all.

The content-fallback `buildServiceAPISurface` also had no endpoint cap; only
the graph path stopped at 50.

## Change on the hot path

`WorkloadContextResultLimits` (`querycontract/context_limits.go`) now calls
`capContextBudgetRows` (`querycontract/context_limits_budget.go`) for the
workload and service context surfaces. It runs after every consumer of the full
lists has read them, so the cut changes only what ships. It cuts
`api_surface.endpoints` and the four `deployment_evidence` lists to
`ContextStoryItemLimit` (50) on a copy of the evidence map, drops the endpoint
rows from `deployment_overview.api_surface` (counts stay; `endpoints_shipped_at`
names the top-level list), and reports totals and cuts:

- totals: `api_surface.endpoint_count`, `deployment_evidence.artifact_count`,
  `result_limits.artifact_count`, and `deployment_evidence.raw_limits` (count,
  limit and truncated per cut list);
- disclosure: a `partial_reasons` entry per cut, and `result_limits.truncated`.
  The same reasons now also report a list whose graph read had already stopped
  at its own bound (`api_surface.detail_truncated`,
  `deployment_evidence.artifacts_truncated`), which earlier responses never
  surfaced;
- reachability: `evidence_index.*.resolved_ids` still lists every artifact, so a
  cut row is fetched with `get_relationship_evidence`.

The story surface emits none of these lists and is untouched. No graph or
Postgres query changes: the same rows are read.

No-Regression Evidence:
- Metric: est2x, both MCP wire copies counted, against the 262,144-byte budget
  from `go/internal/mcp/dispatch_budget.go`.
- Fixture: `entity/workload_context_budget_test.go` drives the real
  `GetWorkloadContext` handler with 78 endpoint edges (50 rows read), and the
  deployment-evidence read filled to its cap in both directions (100 artifact
  rows, every column populated). Before the change it measures 451,114 bytes;
  after, 250,690 bytes. The first figure reproduces the over-budget class
  measured on ops-qa; it is a fixture figure, not an ops-qa figure.
- Seeded violation: with the `capContextBudgetRows` call disabled, the handler
  test and three querycontract tests fail; restored, they pass.
- Safety: the change copies two map headers and slices three row lists after
  the reads. No allocation of note, no query, no round trip, no new lock.
- Backend: none. The tests use fake graph readers.
- NOT_CHECKED: an ops-qa re-measure. The host was unreachable from the
  development laptop on 2026-10-02 (connection timeout), so no measurement
  against a live backend was possible in this change. The post-deploy re-measure
  of both tools on the two outlier workloads stays owed.

No-Observability-Change: no metric, span or log changes. The operator-visible
signal is in the response: `partial_reasons`, `result_limits.truncated` and
`artifact_count`, and `deployment_evidence.raw_limits`. The existing MCP budget
signals (`eshu_dp_mcp_response_bytes`, `eshu_dp_mcp_response_over_budget_total`)
show whether either tool still crosses the budget.
