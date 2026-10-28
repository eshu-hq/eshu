# AGENTS.md — deploy

Scoped agent instructions for the `eshu-deploy` extraction census (issue
#8871).

## Mandatory

- The census record (`docs/internal/evidence/8871-eshu-deploy-census.md`)
  is an inventory, not a migration: never add code, chart, workflow, or
  removal changes in this package. The cutover PR is separate work owned
  by the #8871 drive.
- Keep the `## Counts` line shape (`- <label>: <N>`) exact; the test
  parses it. When a deployment family gains or loses a file, update the
  census number in the same change.
- Schema-bootstrap and repair implementation (`go/cmd/bootstrap-data-plane`)
  stays core-owned per the ecosystem migration plan. This package records
  the boundary; it never moves the implementation.
- Apply TDD: a new census assertion fails against the tree or the record
  before the record changes.
- This package emits no telemetry and owns none; do not add metrics,
  spans, or structured logs here.

## Patterns to follow

- Mirror the `go/cmd/collector-vault-live/extraction_census_test.go`
  precedent: record-relative path constant, family list asserted both as
  named in the record and present in the tree.
- Register any new test-read docs file in
  `specs/ci-docs-lockstep-paths.txt`, `go/internal/cigates/docsset.go`,
  and the workflow `lockstep` filters (`test.yml`,
  `security-scan.yml`, `mcp-schema-drift.yml`).
