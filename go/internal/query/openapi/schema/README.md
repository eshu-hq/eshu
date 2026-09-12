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
  read surface omits from its answer; consumed by the `impact`,
  `repository` and `search` leaves.
- `runtime_topology_limits.go` — the unexported boundedCollectionLimits (the generic
  limit / observed-count / truncation shape) and
  `ImpactRuntimeTopologyLimits`, which composes three copies of it for
  instances, platform edges, and provisioned platforms.

Today `openapi/components_workload_session.go` and `openapi/paths/impact/routes.go`
both consume `ImpactRuntimeTopologyLimits` — a parent and a leaf, which is
exactly the case this package exists for; `EvidenceBoundaries` is consumed by
three leaves (`impact`, `repository`, `search`) that cannot import each other
either. A new fragment belongs here only when two or more of `openapi`'s
packages that cannot import each other — the parent and a `paths/<leaf>`, or
two or more different `paths/<leaf>` packages — genuinely need it. A fragment
used by only one such package belongs there instead, and a fragment used only
by `openapi` belongs in `components.go`.

## Move evidence

These two files moved here verbatim from the query root
(`openapi_evidence_boundaries_schema.go` and `openapi_runtime_topology_limits.go`,
Issue #6060 lane C, #6642): only the package clause, file names, and the
importing consumers' `openapi/schema` import path changed. The JSON each
constant produces is unchanged.

`openapi_impact_k8s_limits.go` and `openapi_supply_chain_runtime_context.go`
also moved here in that same pass, then moved again — as `impact.K8sResourceLimits`
and `chain.RuntimeContext` (`paths/supply/chain/`) — once the consumer census showed each had
only one leaf consumer, not the parent-and-leaf or multi-leaf case this
package exists for.
