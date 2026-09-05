# API And MCP Diagnostic Validation

## MCP/API Call Checklist

Before calling or designing an Eshu MCP/API tool:

- resolve the smallest canonical scope first (`repo_id`, `workload_id`,
  `service_id`, or `environment`)
- prefer cheap summary/count/handle calls before payload-heavy drilldowns
- confirm local MCP owner ports are current when running against a local Eshu
  service
- inspect the Eshu envelope (`truth.level`, `truth.profile`,
  `truth.freshness.state`, and `error`) before interpreting results
- bind visible, empty, and error UI states to the exact response owner and
  lifecycle phase; a selector match or unrelated successful request is not
  proof of the displayed data
- accept bootstrap or cached data only when the exact owning bootstrap response
  and its source/runtime identity are recorded
- classify slowness as transport, stale owner ports, backend health, query
  shape, payload size, or runtime-mode selection before retrying

Do not repeat the same unbounded call after a slow or hung attempt.

## Failure Classification

For dashboard/API/MCP validation, classify every failure as incorrect data or
behavior, latency, response-ownership mismatch, or harness/setup failure. Do
not combine those categories into a generic broken-surface count. Bind the
proof to the exact source hash, binary or image digest, retained-data identity,
and browser-runner hash; a stale supposedly final artifact invalidates the
claim.
