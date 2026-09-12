# Infrastructure OpenAPI Paths — Agent Instructions

Scope: `go/internal/query/openapi/paths/infrastructure/` (package
`infrastructure`, top-level files).

## Ownership

This leaf owns the infra-resource, Kubernetes, observability-coverage, and
secrets/IAM OpenAPI fragments (#6060, lane C): `routes.go` (`Routes`),
`resource_aggregate.go` (`ResourceAggregate`), `kubernetes.go`
(`Kubernetes`), `observability_coverage.go` (`ObservabilityCoverage`), and
`secrets_iam.go` (`SecretsIAM`).

- Each exported constant is a raw JSON object-body string keyed by path,
  meant to be concatenated inside the paths object `openapi/spec.go`
  assembles. Do not add a wrapping brace; match the sibling constants'
  shape.
- None of these fragments import `openapi/schema`. If a new route needs a
  shared schema fragment, add the import rather than inlining a copy.
- MUST NOT import `openapi` (the parent) or any other `paths/<family>`
  leaf — the parent already imports every leaf, so either direction back
  through it is a cycle.

## Verification

A change here must keep `scripts/verify-openapi.sh` green — it
cross-references `mux.HandleFunc` registrations against these fragments —
and must update `docs/public/reference/http-api.md` in the same PR (root
`CLAUDE.md` Documentation Discipline).

## Naming

`docs/internal/naming.md` is law: no `infrastructure_` file prefixes, no
`infrastructure/infrastructure.go`, exported identifiers lose the family
stutter.
