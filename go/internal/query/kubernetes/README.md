# kubernetes

## Purpose

Serves `GET /api/v0/kubernetes/correlations`: reducer-owned Kubernetes
workload correlations (workload to image and source digest). Also owns the
Postgres store the supply-chain runtime probe reads for current Kubernetes
workloads. See `doc.go` for the contract.

## Ownership boundary

Owns the route, its request validation, the correlation read model over
`reducer_kubernetes_correlation` facts, and the runtime workload inventory
store. The reducer that writes the facts
(`internal/reducer/kubernetescorrelation`), the supply-chain probe that calls
the inventory store (`internal/query/supply/chain`), and the MCP tool in
`internal/mcp/kubernetes` are outside this package.

This is not `internal/query/querycontract/kubernetes`, which holds the
Kubernetes selector contract other query families share. A file that needs
both imports one of them under an alias.

## Layout

- `handler.go` — `Handler`, `CorrelationResult`, limit and anchor
  validation, access scoping and paging.
- `correlations.go` — `WorkloadCorrelationStore`, `CorrelationFilter`,
  `CorrelationRow`, `CorrelationQueryer`, `PostgresCorrelationStore` and its
  SQL.
- `runtime_workload_store.go` — `PostgresRuntimeWorkloadStore` and
  `BuildRuntimeWorkloadQuery`.
- `capability.go` — `Capability` and `Support`, the single declaration of the
  `kubernetes.correlations.list` row.
- `handler_tracing.go` — this package's handler span seam.

## Dependencies

`internal/query/querycontract`, `internal/query/supply/chain` (the probe port
types and candidate budgets), `internal/query/tracing`, `internal/telemetry`.
`supply/chain` does not import this package, so there is no cycle.

## Telemetry

Span `telemetry.SpanQueryKubernetesCorrelations` per request, with
`http.route` and `eshu.capability` attributes, from the shared handler tracer.
Unchanged by the #6642 move.

## Gotchas / invariants

- The store interface is named `WorkloadCorrelationStore`, not
  `CorrelationStore`: `internal/mcp`'s route-serves-data registry matches store
  types by substring, and a bare `CorrelationStore` is a substring of every
  other family's correlation store type (`CICDRunCorrelationStore`,
  `CatalogCorrelationStore`, `ObservabilityCorrelationStore`, ...).
- `internal/mcp/route_serves_data_registry_routes.go`,
  `route_serves_data_registry.go` and `route_serves_data_structural_test.go`
  pin this route's file paths and type names; a rename or file move here must
  update them.
- `kubernetes_runtime_workload_store_fairness_live_test.go` and the
  supply-chain runtime-probe performance pair stay in root: they wire root's
  `SupplyChainHandler` to this package's store.

## Move evidence (#6642)

`kubernetes.go`, `kubernetes_correlations.go`,
`kubernetes_runtime_workload_store.go` and four of their tests moved here as
`handler.go`, `correlations.go`, `runtime_workload_store.go`,
`correlations_test.go`, `runtime_workload_store_test.go`,
`runtime_workload_store_bench_test.go` and
`runtime_workload_store_live_test.go` (`git mv`). Exported names dropped the
`Kubernetes` prefix the path now carries. Root forwarders became direct
`querycontract` and `supplychain` calls, and root's two unused
`supplyChainKubernetesRuntimeProbeMax*` constants were deleted.

## No-Regression Evidence

No-Regression Evidence: both SQL statements, their parameters, the 200-row cap,
the keyset cursor and the decode path are unchanged; only package qualifiers
and names differ.

## No-Observability-Change

No-Observability-Change: same span name and tracer as the root handler; no
metric or log changes.

## Related docs

- [Remote validation](../../../../docs/internal/remote-validation/prod-kubernetes-correlations.md)
