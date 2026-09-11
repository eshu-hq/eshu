# Evidence OpenAPI Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/evidence/` (package `evidence`).

## Ownership

This leaf owns the OpenAPI 3.0 path documentation for the evidence surface:
`routes.go` (`Routes`), `documentation_finding_aggregate.go`
(`DocumentationFindingAggregate`), `bundle.go` (`Bundle`),
`incident_context.go` (`IncidentContext`), `investigation_workflows.go`
(`InvestigationWorkflows`), `investigations.go` (`Investigations`),
`visualization_packets.go` (`VisualizationPackets`), and `work_item.go`
(`WorkItem`). Each file is a JSON string constant only — no handler logic,
no HTTP wiring.

- This package may import `openapi/schema` for shared inline schemas. It
  MUST NOT import the parent `openapi` package or the `query` root — both
  import this package, so either direction is a cycle.
- `routes.go` sits at 493 of the 500-line cap. A new fragment belongs in its
  own file (following `status`'s `routes.go`/`compare.go` split) rather than
  growing `routes.go` further.
- A constant here is inert until `openapi/spec.go` concatenates it into the
  published spec. Adding, renaming, or removing an exported constant here
  without updating `spec.go` in the same change silently changes the
  published OpenAPI document without changing what the server actually
  serves, or vice versa.

## Naming

`docs/internal/naming.md` is law: no `evidence_` file prefixes, no
`evidence/evidence.go`, and exported constants lose the family stutter
(`Bundle`, not EvidenceBundle).

## Verification

A change here MUST keep `scripts/verify-openapi.sh` green — it cross-references
`mux.HandleFunc` registrations against these fragments and fails on either an
undocumented route or a documented route with no handler. A route path or
method change here MUST update
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR, per the root `CLAUDE.md` documentation rule.
