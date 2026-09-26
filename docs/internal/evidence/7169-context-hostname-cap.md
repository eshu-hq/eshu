# #7169 context and trace hostname cap evidence

## Problem

`get_workload_context`, `get_service_context` and `trace_deployment_chain`
went over the MCP response budget on one ops-qa service. That service carries
671 `hostnames` and 671 `entrypoints`. Before this change,
`buildServiceDeploymentOverviewWithContext`
(`go/internal/query/service/story_overview.go`) copied both arrays into
`deployment_overview` as well as the top-level payload. The arrays and their
copies were 92% of the response, and `network_paths` (one row per entrypoint)
had no cap.

## Change on the hot path

`story_overview.go` stops copying the `hostnames` and `entrypoints` arrays into
`deployment_overview`, and keeps only `hostname_count` and `entrypoint_count`.
`WorkloadContextResultLimits` and the trace response limits cap the top-level
arrays, `network_paths` and the trace lists at 50. They report the true
totals (`hostname_count`, `entrypoint_count`, `network_path_count`), and they
name the cut in `partial_reasons` and in the trace `*_limits` blocks. No graph
or Postgres query changes: the same rows are read, and the cap runs after
every derivation.

No-Regression Evidence:
- Metric: est2x, which counts both MCP wire copies, against the 262,144-byte
  MCP budget from `go/internal/mcp/dispatch_budget.go`.
- Baseline: about 1,010,650 bytes for context and 1,055,860 bytes for trace,
  both over budget. These come from the first RED run of the branch's
  earliest budget test, before any cap. Its fixture had 671 hostnames and 671
  entrypoints but no instance and no `network_paths`, and the test assembled
  the context handler's tail itself instead of calling the real
  `GetWorkloadContext`. Nobody measured the later fixture shape (671 network
  paths, one matching instance, the real handler) before the change, so these
  figures show the scale of the overflow, not a like-for-like before/after.
- After the change, `entity/workload_context_hostname_cap_test.go` drives the
  real `GetWorkloadContext` with 671 hostnames, 671 network paths and one
  matching instance. It and `service/context_hostname_budget_test.go` assert
  that both responses fit the budget, with 50 rows per capped list and the 671
  totals reported. The budget test first asserts that the uncapped fixture is
  over budget, so it can tell the two apart.
- Safety: the overview edit only removes two map entries per response. It adds
  no allocation, query or round trip.
- Backend: none. The tests use fake graph readers, and the change runs after
  the reads.
- NOT_CHECKED: an ops-qa re-measure of the outlier service after deploy.

No-Observability-Change: no metric, span or log changes. The operator-visible
signal is in the response itself:
- the `result_limits` counts and `truncated`;
- the `hostnames_truncated`, `entrypoints_truncated` and
  `network_paths_truncated` partial reasons;
- the trace `hostname_limits`, `entrypoint_limits` and `network_path_limits`
  blocks.

The existing MCP budget error (`mcp_response_over_budget`) stops for these
tools on services of this size.
