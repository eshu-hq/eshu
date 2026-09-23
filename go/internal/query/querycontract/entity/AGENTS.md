# Agent instructions: querycontract/entity

Contract types and pure resolution helpers. No reader, no telemetry.

- Keep the import one way: this package imports `querycontract`; the parent
  must never import it back, or the split turns into a cycle.
- `buildEntityNameSearchQuery` in package query is pinned by a queryplan
  source hash (`queryplan_production_variants_test.go`). Changing a type or
  constant it names changes that hash; confirm the SQL text did not change
  before refreshing it.
- `codemodel/code_relationships_resolution.go` carries a byte-identical copy
  of the resolution chain. Change both or neither.

Verify with `cd go && go vet ./internal/query/... && go test ./internal/query/... -count=1`.
