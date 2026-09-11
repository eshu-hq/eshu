# Status OpenAPI Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/status/` (package `status`).

## Ownership

This leaf owns the OpenAPI 3.0 path documentation for the status, admin,
and operator surfaces — thirteen files, the broadest leaf in the tree:
`routes.go` (`Routes`), `compare.go` (`Compare`), `admin.go` (`Admin`),
`answer_narration.go` (`AnswerNarration`), `collector_readiness.go`
(`CollectorReadiness`), `collector_extraction_readiness.go`
(`CollectorExtractionReadiness`, plus the unexported
`collectorExtractionReadinessSchema` it splices into itself),
`fact_schema_version.go` (`FactSchemaVersion`), `governance.go`
(`Governance`), `hosted_readiness.go` (`HostedReadiness`), `metrics.go`
(`Metrics`), `operations.go` (`Operations`), `operator_control_plane.go`
(`OperatorControlPlane`), and `semantic.go` (`Semantic`). Each file is a
JSON string constant only — no handler logic, no HTTP wiring.

- This package imports nothing from the rest of the repo. It MUST NOT
  import the parent `openapi` package or the `query` root — both import
  this package, so either direction is a cycle.
- `compare.go` exists only because `routes.go` was already near the
  500-line cap; do not fold `Compare` back into `Routes`. Follow the same
  precedent (a new file, not a growing one) for the next addition once any
  file here approaches the cap.
- A constant here is inert until `openapi/spec.go` concatenates it into the
  published spec. Adding, renaming, or removing an exported constant here
  without updating `spec.go` in the same change silently changes the
  published OpenAPI document without changing what the server actually
  serves, or vice versa.

## Naming

`docs/internal/naming.md` is law: no `status_` file prefixes, no
`status/status.go`, and exported constants lose the family stutter
(`Admin`, not StatusAdmin). Do not reintroduce the stale
`openAPIPathsStatusAndCompare` name on `Routes` — it never documented
`/compare` and the rename to `Routes` is the corrected name.

## Verification

A change here MUST keep `scripts/verify-openapi.sh` green — it cross-references
`mux.HandleFunc` registrations against these fragments and fails on either an
undocumented route or a documented route with no handler. A route path or
method change here MUST update
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR, per the root `CLAUDE.md` documentation rule.
