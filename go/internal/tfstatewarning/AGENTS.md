# AGENTS.md - internal/tfstatewarning guidance for LLM assistants

## Read first

1. `go/internal/tfstatewarning/README.md` - package contract and classification table purpose
2. `go/internal/tfstatewarning/classification.go` - closed warning severity/actionability map
3. `go/internal/collector/terraformstate/README.md` - Terraform-state warning emission rules
4. `go/internal/status/README.md` - operator-facing status contract

## Invariants

- Keep this package dependency-free except for the Go standard library.
- Treat warning classification as a closed contract. Unknown pairs must return
  `ok=false`; do not silently assign a default severity.
- Do not put raw Terraform state locators, resource names, paths, ARNs, or
  attribute values in this package.
- Keep collector emission and status readback using the same classification
  table so operator meaning does not drift.

## Anti-patterns

- Importing collector, status, query, storage, telemetry, or graph packages.
- Adding per-resource or per-locator state to the classification table.
- Treating warning summary display concerns as classifier behavior.
