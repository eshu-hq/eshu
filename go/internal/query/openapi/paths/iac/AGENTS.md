# IaC OpenAPI Paths — Agent Instructions

Scope: `go/internal/query/openapi/paths/iac/` (package `iac`, top-level files).

## Ownership

This leaf owns the IaC-drift and replatforming OpenAPI fragments (#6060,
lane C): `routes.go` (`Routes`), `resources.go` (`Resources`),
`terraform_config_state_drift.go` (`TerraformConfigStateDrift`),
`replatforming.go` (`Replatforming`), `replatforming_ownership.go`
(`ReplatformingOwnership`), `replatforming_selectors.go`
(`ReplatformingSelectors`), and `replatforming_rollups.go`
(`ReplatformingRollups`).

- Each exported constant is a raw JSON object-body string keyed by path,
  meant to be concatenated inside the paths object `openapi/spec.go`
  assembles. Do not add a wrapping brace; match the sibling constants'
  shape.
- Keep new replatforming routes here rather than splitting them into a
  separate package: the handler side lives on `IaCHandler`
  (`go/internal/query/replatforming_*_handler.go`), and the fragment
  should stay next to the family it documents.
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

`docs/internal/naming.md` is law: no `iac_` file prefixes, no
`iac/iac.go`, exported identifiers lose the family stutter.
