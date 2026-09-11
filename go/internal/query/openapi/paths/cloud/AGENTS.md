# Cloud OpenAPI Paths — Agent Instructions

Scope: `go/internal/query/openapi/paths/cloud/` (package `cloud`, top-level files).

## Ownership

This leaf owns the cloud inventory and runtime-drift OpenAPI fragments
(#6060, lane C): `routes.go` (`Routes`), `inventory.go` (`Inventory`),
`runtime_drift.go` (`RuntimeDrift`), and `aws_runtime_drift.go`
(`AWSRuntimeDrift`).

- Each exported constant is a raw JSON object-body string keyed by path,
  meant to be concatenated inside the paths object `openapi/spec.go`
  assembles. Do not add a wrapping brace; match the sibling constants'
  shape (see any file for the pattern).
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

`docs/internal/naming.md` is law: no `cloud_` file prefixes, no
`cloud/cloud.go`, exported identifiers lose the family stutter.
