# OpenAPI Schema Fragments

Shared OpenAPI JSON Schema fragments used by both `openapi`'s components
block and the route fragments under `openapi/paths/`.

This package exists for one reason: `openapi/spec.go` imports every family
package under `paths/`, so a schema fragment needed by both an
`openapi/components_*.go` file and a `paths/<leaf>` file cannot live in
package `openapi` — `paths/<leaf>` would then import `openapi` while
`openapi` already imports `paths/<leaf>`, an import cycle. `schema` sits
below both, importing neither, so both sides can depend on it without one.

Layout:

- `evidence_boundaries.go` — `EvidenceBoundaries`, the shared response
  fragment disclosing which Postgres-only reducer domains a graph-sourced
  read surface omits from its answer.
- `runtime_topology_limits.go` — the unexported boundedCollectionLimits (the generic
  limit / observed-count / truncation shape) and
  `ImpactRuntimeTopologyLimits`, which composes three copies of it for
  instances, platform edges, and provisioned platforms.
- `impact_k8s_resource_limits.go` — `ImpactK8sResourceLimits`, the
  completeness-metadata fragment for merged and deduplicated
  `k8s_resources` rows.
- `supply_chain_runtime_context.go` — `SupplyChainRuntimeContext`, the
  read-time-resolved runtime context fragment shared by the supply-chain
  findings-list and impact-explain responses.

Today `openapi/components_workload_session.go` consumes
`ImpactRuntimeTopologyLimits`, and `openapi/paths/supplychain/impact_findings.go`
and `impact_explain.go` consume `SupplyChainRuntimeContext`. A new fragment
belongs here only when it has a consumer in `openapi` (or one of its
`components_*.go` files) AND a consumer in some `paths/<leaf>` package — a
fragment used by a single leaf belongs in that leaf instead, and a fragment
used only by `openapi` belongs in `components.go`.

## Move evidence

These four files moved here verbatim from the query root
(`openapi_evidence_boundaries_schema.go`, `openapi_runtime_topology_limits.go`,
`openapi_impact_k8s_limits.go`, and `openapi_supply_chain_runtime_context.go`,
Issue #6060 lane C, #6642): only the package clause, file names, and the
importing consumers' `openapi/schema` import path changed. The JSON each
constant produces is unchanged.
