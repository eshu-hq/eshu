# impact/ownership

## Purpose

Scoped-grant ownership checks for the #5167 impact path routes:
`POST /api/v0/impact/trace-resource-to-code`,
`POST /api/v0/impact/explain-dependency-path`, and
`POST /api/v0/impact/trace-exposure-path`. Those walks cross nodes with no
`repo_id` (CloudResource, TerraformStateResource, Platform, ...), so a scoped
caller's grant is applied per node over the bounded page, in Go, with at most
three small owner statements.

## Ownership rules

| Class | Owned when |
| --- | --- |
| `Repository` | its `id` is granted |
| `Workload`, `TerraformResource`, `TerraformModule`, `KubernetesWorkload`, `Function`, `SqlTable`, `ShellCommand` | its `repo_id` is granted (Go) |
| `WorkloadInstance` | its `repo_id` is granted, or it has a `DEPLOYMENT_SOURCE` edge to a granted `Repository` (statement, keyed on `wi.id`: the writer sets no `uid`) |
| `CloudResource` | a `WorkloadInstance` with a granted `repo_id` `USES` it (statement) |
| `TerraformStateResource` | a `TerraformResource` with a granted `repo_id` `MATCHES_STATE` it (statement) |
| anything else | never (deny by default) |

A path is kept only when every node on it is owned. An anchor that is not
owned renders as unknown. An empty grant issues no graph call.

## Statements and budget

The three statements are grant-free owner projections, anchored on the page's
own keys (`WHERE n.<key> IN $uids`, keyed node first) and returning each key's
owning repository id; the grant is applied in Go. Their cost therefore depends
on the number of checked keys, not on the caller's grant size: about 0.15 ms
per key at `ChunkSize` 50 on the pinned NornicDB build. Per-chunk cost grows
with the square of the key list, so chunks stay small. `MaxCheckedKeys` (4500)
caps the distinct statement-checked keys per request, and `RowLimit` (800)
caps one chunk's owner rows; the cap is at least the
largest page a route builds, so an ordinary page is never capped. Keys past it
are ungranted and the response reports `truncated`. The measurements, and the
grant-anchored shapes they ruled out, are in
`docs/internal/evidence/5167-impact-grant.md`.

## Dependencies

`querycontract` (grant filter, graph port, row decoders), `impact/deployment`
(resolved anchor and path identity types), and `internal/telemetry`. It never
imports the query root, `impact`, or a graph driver.

## Telemetry

- `eshu_dp_query_impact_scoped_paths_withheld_total{route,reason}`
- `eshu_dp_query_impact_ownership_check_duration_seconds{route,node_label,outcome}`
- a Warn log `impact ownership check capped` with the grant size and cap
  whenever a page exceeds the budget.
