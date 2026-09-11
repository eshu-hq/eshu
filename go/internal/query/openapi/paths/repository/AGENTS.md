# Repository OpenAPI Paths — Agent Instructions

Scope: `go/internal/query/openapi/paths/repository/` (package `repository`,
top-level files).

## Ownership

This leaf owns the repository, package-registry, and dependency OpenAPI
fragments (#6060, lane C): `routes.go` (`Routes`), `branches.go`
(`Branches`), `freshness.go` (`Freshness`), `stats_and_coverage.go`
(`StatsAndCoverage`), `dependencies.go` (`Dependencies`),
`package_registry.go` (`PackageRegistry`), and
`package_registry_aggregate.go` (`PackageRegistryAggregate`).

- Each exported constant is a raw JSON object-body string keyed by path,
  meant to be concatenated inside the paths object `openapi/spec.go`
  assembles. Do not add a wrapping brace; match the sibling constants'
  shape.
- `routes.go` depends on `openapi/schema` for `schema.EvidenceBoundaries`.
  Reuse that import rather than inlining a second copy of the fragment if
  a new route in this package needs the same evidence-boundaries shape.
- MUST NOT import `openapi` (the parent) or any other `paths/<family>`
  leaf — the parent already imports every leaf, so either direction back
  through it is a cycle.

## Verification

A change here must keep `scripts/verify-openapi.sh` green — it
cross-references `mux.HandleFunc` registrations against these fragments —
and must update `docs/public/reference/http-api.md` in the same PR (root
`CLAUDE.md` Documentation Discipline).

## Naming

`docs/internal/naming.md` is law: no `repository_` file prefixes, no
`repository/repository.go`, exported identifiers lose the family stutter.
