# Impact OpenAPI Paths — Agent Instructions

Scope: `go/internal/query/openapi/paths/impact/` (package `impact`,
top-level files).

## Ownership

This leaf owns the change- and blast-radius-impact OpenAPI fragments
(#6060, lane C): `routes.go` (`Routes`), `contract.go` (`Contract`),
`deployment_config_influence.go` (`deploymentConfigInfluence`),
`exposure.go` (`Exposure`), and `rest.go` (`Rest`).

- Each exported constant is a raw JSON object-body string keyed by path,
  meant to be concatenated inside the paths object `openapi/spec.go`
  assembles — except `deploymentConfigInfluence`, which `routes.go` folds
  into `Routes` directly. See `README.md`'s Assembly section before
  touching either constant; adding `deploymentConfigInfluence` to
  `openapi/spec.go`'s concatenation as well would duplicate the route.
- `routes.go` and `deployment_config_influence.go` depend on
  `openapi/schema` for `schema.ImpactK8sResourceLimits`; `routes.go` also
  uses `schema.ImpactRuntimeTopologyLimits` and
  `schema.EvidenceBoundaries`. Reuse those imports for new routes needing
  the same shapes rather than inlining a copy.
- MUST NOT import `openapi` (the parent) or any other `paths/<family>`
  leaf — the parent already imports every leaf, so either direction back
  through it is a cycle.

## Verification

A change here must keep `scripts/verify-openapi.sh` green — it
cross-references `mux.HandleFunc` registrations against these fragments —
and must update `docs/public/reference/http-api.md` in the same PR (root
`CLAUDE.md` Documentation Discipline).

## Naming

`docs/internal/naming.md` is law: no `impact_` file prefixes, no
`impact/impact.go`, exported identifiers lose the family stutter.
